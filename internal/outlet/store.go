package outlet

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"log/slog"
	"maps"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lawnect/meerkat/internal/contract"
	"github.com/lawnect/meerkat/internal/control"
	"github.com/lawnect/meerkat/internal/proxy"
	"github.com/lawnect/meerkat/internal/rathole"
	"github.com/lawnect/meerkat/internal/securefs"
	"github.com/lawnect/meerkat/internal/validation"
)

type Seed struct {
	Name                string                 `json:"name"`
	Configuration       contract.Configuration `json:"configuration"`
	ProbeAddress        string                 `json:"probe_address"`
	EnrollmentHash      string                 `json:"enrollment_hash"`
	EnrollmentExpiresAt time.Time              `json:"enrollment_expires_at"`
}

type Config struct {
	ListenAddress          string `json:"listen_address"`
	TLSCertificate         string `json:"tls_certificate"`
	TLSKey                 string `json:"tls_key"`
	AdminTokenHash         string `json:"admin_token_hash"`
	EnrollmentTTLSeconds   int    `json:"enrollment_ttl_seconds"`
	CredentialTTLSeconds   int    `json:"credential_ttl_seconds"`
	Domain                 string `json:"domain,omitempty"`
	Region                 string `json:"region,omitempty"`
	TunnelListenAddress    string `json:"tunnel_listen_address,omitempty"`
	TunnelRemoteAddress    string `json:"tunnel_remote_address,omitempty"`
	TunnelServerPublicKey  string `json:"tunnel_server_public_key,omitempty"`
	TunnelServerPrivateKey string `json:"tunnel_server_private_key,omitempty"`
	Connectors             []Seed `json:"connectors"`
	// CaddyUpstream is the host:port Caddy uses to reach the public
	// listener, written into the generated Caddyfile and expected as the
	// Host of Caddy's certificate check. It comes from the environment;
	// empty means proxy.DefaultUpstream.
	CaddyUpstream string `json:"-"`
	// CaddyMetrics is the host:port of Caddy's metrics site, read for
	// traffic usage. It comes from the environment; empty means
	// proxy.DefaultMetrics.
	CaddyMetrics string `json:"-"`
}

type Record struct {
	Seed
	ProbeAddresses      map[string]string   `json:"probe_addresses,omitempty"`
	ServiceStatuses     map[string]string   `json:"service_statuses,omitempty"`
	Enrolled            bool                `json:"enrolled"`
	PublicKey           string              `json:"public_key,omitempty"`
	RunTokenHash        string              `json:"run_token_hash,omitempty"`
	CredentialHash      string              `json:"credential_hash,omitempty"`
	CredentialExpiresAt time.Time           `json:"credential_expires_at,omitempty"`
	LastHeartbeat       *contract.Heartbeat `json:"last_heartbeat,omitempty"`
	LastSeen            time.Time           `json:"last_seen,omitempty"`
	TunnelStatus        string              `json:"tunnel_status"`
}

func (r Record) probeFor(serviceID string) string {
	if address := r.ProbeAddresses[serviceID]; address != "" {
		return address
	}
	if r.Configuration.ServiceID == serviceID {
		return r.ProbeAddress
	}
	return ""
}

type Store struct {
	mu          sync.RWMutex
	path        string
	managedPath string
	proxyPath   string
	tunnelPath  string
	records     map[string]Record
	managed     map[string]Record
	config      Config
	control     *control.Database
	now         func() time.Time
	// Tunnel checks resolve and dial through these when set (tests).
	lookupIP func(context.Context, string) ([]net.IPAddr, error)
	dialTCP  func(context.Context, string) (net.Conn, error)
	// Telegram messages go through this when set (tests).
	sendTelegram func(ctx context.Context, token, chatID, text string) error
	challenges   challenges
	loginLimit   attemptLimiter
	alerts       alertState
	limiter      *rateLimiter
	connections  *connectionLimiter
	blocks       blockState
	sso          ssoState
	// ssoHTTP talks to the OpenID provider; nil uses a client with a timeout.
	ssoHTTP *http.Client
	// deviceKey signs trusted-device cookies; empty without a control database.
	deviceKey []byte
	// served maps a service ID to an *atomic.Int64: when, in Unix
	// nanoseconds, it last answered proxied traffic below 500.
	served sync.Map
	// balance counts the requests of each balanced address, to take turns.
	balance sync.Map
	// suspect maps a service ID to when a request to it last failed before
	// any answer, to pass it over for a while (see liveBackends).
	suspect sync.Map
	// background tracks login notifications still being sent.
	background sync.WaitGroup
	journal    *auditJournal
	sessions   sessionCache
	setupDone  atomic.Bool
	// Heartbeats change liveness in memory only; persistLoop writes the state
	// files when these are set. Guarded by mu.
	recordsDirty, managedDirty bool
	stopWorkers                context.CancelFunc
	workers                    sync.WaitGroup
	closeOnce                  sync.Once
}

func Hash(s string) string { b := sha256.Sum256([]byte(s)); return hex.EncodeToString(b[:]) }
func Token() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func Open(config Config, dir string) (*Store, error) {
	return OpenWithControl(config, dir, nil)
}

func OpenWithControl(config Config, dir string, controlDB *control.Database) (*Store, error) {
	if len(config.AdminTokenHash) != 64 || config.EnrollmentTTLSeconds < 60 || config.CredentialTTLSeconds < 60 {
		return nil, errors.New("invalid authentication configuration")
	}
	if err := validation.Endpoint(config.ListenAddress); err != nil {
		return nil, err
	}
	if config.CaddyUpstream == "" {
		config.CaddyUpstream = proxy.DefaultUpstream
	}
	if config.CaddyMetrics == "" {
		config.CaddyMetrics = proxy.DefaultMetrics
	}
	// A Compose service name, DNS name or IP literal with a port, so nothing
	// else reaches the Caddyfile.
	if !validation.ServiceAddress(config.CaddyUpstream) || !validation.ServiceAddress(config.CaddyMetrics) {
		return nil, errors.New("invalid Caddy address")
	}
	records := make(map[string]Record)
	services, ports := map[string]bool{}, map[string]bool{}
	for _, seed := range config.Connectors {
		if !validation.Label(seed.Name) || validation.Configuration(seed.Configuration) != nil || len(seed.EnrollmentHash) != 64 {
			return nil, errors.New("invalid connector seed")
		}
		id := seed.Configuration.ConnectorID
		if _, ok := records[id]; ok || services[seed.Configuration.Tunnel.ServiceName] || ports[seed.ProbeAddress] {
			return nil, errors.New("duplicate connector, tunnel or probe")
		}
		if err := validation.Endpoint(seed.ProbeAddress); err != nil {
			return nil, err
		}
		// Probes must never be an arbitrary outbound request.
		if !isLoopbackAddress(seed.ProbeAddress) {
			return nil, errors.New("outlet probe must use a loopback address")
		}
		services[seed.Configuration.Tunnel.ServiceName], ports[seed.ProbeAddress] = true, true
		records[id] = Record{Seed: seed, TunnelStatus: "unknown"}
	}
	if err := securefs.Directory(dir); err != nil {
		return nil, err
	}
	s := &Store{path: filepath.Join(dir, "state.json"), managedPath: filepath.Join(dir, "managed-state.json"), proxyPath: filepath.Join(dir, "caddy", "Caddyfile"), tunnelPath: filepath.Join(dir, "rathole", "server.toml"), records: records, managed: map[string]Record{}, config: config, control: controlDB, now: time.Now}
	if controlDB != nil && config.Domain != "" {
		outlet, err := controlDB.Outlet(context.Background())
		if err != nil {
			return nil, err
		}
		if outlet.Domain == "" {
			if _, err := controlDB.SetDomain(context.Background(), config.Domain, ""); err != nil {
				return nil, err
			}
		}
	}
	err := securefs.ReadJSON(s.path, &records)
	if err == nil {
		if len(records) != len(s.records) {
			return nil, errors.New("state and bootstrap configuration differ")
		}
		for id, r := range records {
			seed, ok := s.records[id]
			if !ok || !reflect.DeepEqual(seed.Configuration, r.Configuration) || seed.Name != r.Name || seed.ProbeAddress != r.ProbeAddress {
				return nil, errors.New("state and bootstrap configuration differ")
			}
			// A persisted heartbeat does not prove current liveness after restart.
			r.LastSeen, r.TunnelStatus = time.Time{}, "unknown"
			records[id] = r
		}
		s.records = records
	} else if !os.IsNotExist(err) {
		return nil, err
	} else if err := securefs.WriteJSON(s.path, records); err != nil {
		return nil, err
	}
	if err := securefs.ReadJSON(s.managedPath, &s.managed); err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		if err := securefs.WriteJSON(s.managedPath, s.managed); err != nil {
			return nil, err
		}
	}
	// Point Connectors at the current tunnel address, e.g. after
	// tunnel_remote_address changed in outlet.json.
	if controlDB != nil {
		outlet, err := controlDB.Outlet(context.Background())
		var tunnel outletTunnel
		if err == nil {
			tunnel, err = s.tunnel(context.Background(), outlet.AdminHostname)
		}
		if err == nil {
			err = s.retargetTunnels(tunnel.Address)
		}
		if err != nil {
			return nil, err
		}
	}
	for id, record := range s.managed {
		if !validation.Label(id) || validation.Configuration(record.Configuration) != nil || !isLoopbackAddress(record.ProbeAddress) {
			return nil, errors.New("invalid managed connector state")
		}
		for serviceID, address := range record.ProbeAddresses {
			if !validation.Label(serviceID) || !isLoopbackAddress(address) {
				return nil, errors.New("invalid managed service probe")
			}
		}
		// Observed liveness never survives an Outlet restart.
		record.LastSeen, record.TunnelStatus, record.ServiceStatuses = time.Time{}, "unknown", nil
		s.managed[id] = record
	}
	if err := s.reconcileProxy(context.Background()); err != nil {
		return nil, err
	}
	if err := s.reconcileTunnel(); err != nil {
		return nil, err
	}
	s.limiter = newRateLimiter()
	if err := s.reloadRateLimit(context.Background()); err != nil {
		return nil, err
	}
	s.connections = &connectionLimiter{}
	if err := s.reloadConnectionLimits(context.Background()); err != nil {
		return nil, err
	}
	if err := s.reloadBlockRules(context.Background()); err != nil {
		return nil, err
	}
	if controlDB != nil {
		key, err := controlDB.DeviceKey(context.Background(), time.Now())
		if err != nil {
			return nil, err
		}
		s.deviceKey = key
	}
	workers, stop := context.WithCancel(context.Background())
	s.stopWorkers = stop
	if controlDB != nil {
		s.journal = newAuditJournal(controlDB, filepath.Join(dir, "spool", "audit"))
		s.workers.Go(func() { s.journal.run(workers) })
	}
	s.workers.Go(func() { s.persistLoop(workers) })
	return s, nil
}

func (s *Store) reconcileTunnel() error {
	if s.config.TunnelListenAddress == "" || !validation.PublicKey(s.config.TunnelServerPrivateKey) {
		return nil
	}
	s.mu.Lock()
	services := make([]rathole.ServerService, 0, len(s.managed))
	for _, record := range s.managed {
		for _, service := range record.Configuration.ActiveServices() {
			_, port, err := net.SplitHostPort(record.probeFor(service.ServiceID))
			if err != nil {
				s.mu.Unlock()
				return err
			}
			// rathole shares the Outlet's network namespace and the Outlet
			// reaches services over loopback, so only loopback listens:
			// another container on a shared network (Coolify) can't reach an
			// app around the Outlet's routing, block rules and limits.
			bindAddress := net.JoinHostPort("127.0.0.1", port)
			services = append(services, rathole.ServerService{Name: service.Tunnel.ServiceName, Token: service.Tunnel.ServiceToken, BindAddress: bindAddress})
		}
	}
	s.mu.Unlock()
	config, err := rathole.Server(s.config.TunnelListenAddress, s.config.TunnelServerPrivateKey, services)
	if err != nil {
		return err
	}
	return securefs.WriteReloadable(s.tunnelPath, config)
}

// reconcileProxy changes Caddy only when the first domain is registered.
// Domain and route updates are looked up by the public request handler.
func (s *Store) reconcileProxy(ctx context.Context) error {
	if s.control == nil {
		return nil
	}
	routing, err := s.control.Routing(ctx)
	if err != nil {
		return err
	}
	if routing.Outlet.Domain == "" {
		return securefs.WriteIfChanged(s.proxyPath, proxy.EmptyCaddyfile())
	}
	return securefs.WriteIfChanged(s.proxyPath, proxy.DynamicCaddyfile(s.config.CaddyUpstream))
}

const persistInterval = 30 * time.Second

// observe records a Connector's liveness in memory. Liveness never survives
// a restart, so a heartbeat does not wait for the state file; persistLoop
// writes it later. Called only while the store lock is held.
func (s *Store) observe(id string, record Record, managed bool) {
	if managed {
		next := maps.Clone(s.managed)
		next[id] = record
		s.managed, s.managedDirty = next, true
		return
	}
	next := maps.Clone(s.records)
	next[id] = record
	s.records, s.recordsDirty = next, true
}

// persist writes state files that changed only in memory.
func (s *Store) persist() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.recordsDirty {
		if err := securefs.WriteJSON(s.path, s.records); err != nil {
			slog.Error("connector state not saved", "error", err)
		} else {
			s.recordsDirty = false
		}
	}
	if s.managedDirty {
		if err := securefs.WriteJSON(s.managedPath, s.managed); err != nil {
			slog.Error("connector state not saved", "error", err)
		} else {
			s.managedDirty = false
		}
	}
}

func (s *Store) persistLoop(ctx context.Context) {
	ticker := time.NewTicker(persistInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.persist()
		case <-ctx.Done():
			s.persist()
			return
		}
	}
}

// Close stops the background writers after they save what is pending:
// connector state, and audit entries (written, or set aside on disk). Call it
// after the servers stop and before the control database closes.
func (s *Store) Close() {
	s.closeOnce.Do(func() {
		if s.stopWorkers != nil {
			s.stopWorkers()
		}
		s.workers.Wait()
	})
}

// Persist before publishing changes: a failed write must not consume a token.
func (s *Store) save(id string, record Record) error {
	next := make(map[string]Record, len(s.records))
	for key, value := range s.records {
		next[key] = value
	}
	next[id] = record
	if err := securefs.WriteJSON(s.path, next); err != nil {
		return err
	}
	s.records, s.recordsDirty = next, false
	return nil
}

func (s *Store) saveManaged(id string, record Record) error {
	next := make(map[string]Record, len(s.managed)+1)
	for key, value := range s.managed {
		next[key] = value
	}
	next[id] = record
	if err := securefs.WriteJSON(s.managedPath, next); err != nil {
		return err
	}
	s.managed, s.managedDirty = next, false
	return nil
}

func (s *Store) removeManaged(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.managed[id]; !ok {
		return nil
	}
	next := make(map[string]Record, len(s.managed)-1)
	for key, value := range s.managed {
		if key != id {
			next[key] = value
		}
	}
	if err := securefs.WriteJSON(s.managedPath, next); err != nil {
		return err
	}
	s.managed, s.managedDirty = next, false
	return nil
}
