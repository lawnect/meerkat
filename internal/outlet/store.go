package outlet

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/lawnect/meerkat/internal/contract"
	"github.com/lawnect/meerkat/internal/control"
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
	ListenAddress         string `json:"listen_address"`
	TLSCertificate        string `json:"tls_certificate"`
	TLSKey                string `json:"tls_key"`
	AdminTokenHash        string `json:"admin_token_hash"`
	EnrollmentTTLSeconds  int    `json:"enrollment_ttl_seconds"`
	CredentialTTLSeconds  int    `json:"credential_ttl_seconds"`
	TunnelRemoteAddress   string `json:"tunnel_remote_address,omitempty"`
	TunnelServerPublicKey string `json:"tunnel_server_public_key,omitempty"`
	Connectors            []Seed `json:"connectors"`
}

type Record struct {
	Seed
	Enrolled            bool                `json:"enrolled"`
	PublicKey           string              `json:"public_key,omitempty"`
	CredentialHash      string              `json:"credential_hash,omitempty"`
	CredentialExpiresAt time.Time           `json:"credential_expires_at,omitempty"`
	LastHeartbeat       *contract.Heartbeat `json:"last_heartbeat,omitempty"`
	LastSeen            time.Time           `json:"last_seen,omitempty"`
	TunnelStatus        string              `json:"tunnel_status"`
}

type Store struct {
	mu          sync.Mutex
	path        string
	managedPath string
	records     map[string]Record
	managed     map[string]Record
	config      Config
	control     *control.Database
	now         func() time.Time
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
	s := &Store{path: filepath.Join(dir, "state.json"), managedPath: filepath.Join(dir, "managed-state.json"), records: records, managed: map[string]Record{}, config: config, control: controlDB, now: time.Now}
	err := securefs.ReadJSON(s.path, &records)
	if err == nil {
		if len(records) != len(s.records) {
			return nil, errors.New("state and bootstrap configuration differ")
		}
		for id, r := range records {
			seed, ok := s.records[id]
			if !ok || seed.Configuration != r.Configuration || seed.Name != r.Name || seed.ProbeAddress != r.ProbeAddress {
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
	for id, record := range s.managed {
		if !validation.Label(id) || validation.Configuration(record.Configuration) != nil || !isLoopbackAddress(record.ProbeAddress) {
			return nil, errors.New("invalid managed connector state")
		}
		// Observed liveness never survives an Outlet restart.
		record.LastSeen, record.TunnelStatus = time.Time{}, "unknown"
		s.managed[id] = record
	}
	return s, nil
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
	s.records = next
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
	s.managed = next
	return nil
}
