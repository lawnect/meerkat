package connector

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"time"

	"github.com/lawnect/meerkat/internal/contract"
	"github.com/lawnect/meerkat/internal/health"
	"github.com/lawnect/meerkat/internal/rathole"
	"github.com/lawnect/meerkat/internal/securefs"
	"github.com/lawnect/meerkat/internal/transport"
	"github.com/lawnect/meerkat/internal/validation"
)

type identity struct {
	PrivateKey string `json:"private_key"`
}
type session struct {
	OutletURL  string                      `json:"outlet_url"`
	Name       string                      `json:"name"`
	Enrollment contract.EnrollmentResponse `json:"enrollment"`
}

func credentialCurrent(expires time.Time) bool {
	return expires.IsZero() || time.Now().Before(expires)
}

func publicIdentity(dir string) (string, error) {
	path := filepath.Join(dir, "identity.json")
	var id identity
	err := securefs.ReadJSON(path, &id)
	if os.IsNotExist(err) {
		_, private, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return "", err
		}
		id.PrivateKey = base64.StdEncoding.EncodeToString(private)
		if err := securefs.WriteJSON(path, id); err != nil {
			return "", err
		}
	} else if err != nil {
		return "", err
	}
	private, err := base64.StdEncoding.DecodeString(id.PrivateKey)
	if err != nil || len(private) != ed25519.PrivateKeySize {
		return "", errors.New("invalid saved connector identity")
	}
	return base64.StdEncoding.EncodeToString(ed25519.PrivateKey(private).Public().(ed25519.PublicKey)), nil
}

func allowed(local Config, remote contract.Configuration) error {
	if err := validation.Configuration(remote); err != nil {
		return err
	}
	if local.Target != (contract.Target{}) {
		for _, service := range remote.ActiveServices() {
			if local.Target != service.Target {
				return errors.New("outlet target is not in the connector's local allowlist")
			}
		}
	}
	return nil
}

func enroll(ctx context.Context, cfg Config, api *transport.Client, token string) (contract.EnrollmentResponse, error) {
	path := filepath.Join(cfg.DataDir, "session.json")
	var saved session
	err := securefs.ReadJSON(path, &saved)
	if err != nil && !os.IsNotExist(err) {
		return contract.EnrollmentResponse{}, err
	}
	if err == nil && (saved.OutletURL != cfg.OutletURL || saved.Name != cfg.Name) {
		return contract.EnrollmentResponse{}, errors.New("saved identity belongs to another outlet or connector")
	}
	if validation.Token(token) {
		public, keyErr := publicIdentity(cfg.DataDir)
		if keyErr != nil {
			return contract.EnrollmentResponse{}, keyErr
		}
		response, enrollErr := api.Enroll(ctx, token, contract.EnrollmentRequest{Name: cfg.Name, Version: contract.Version, PublicKey: public})
		if enrollErr == nil {
			if len(response.ActiveServices()) != 0 {
				if err := allowed(cfg, response.Configuration); err != nil {
					return response, err
				}
			}
			if !validation.Token(response.Credential) || !credentialCurrent(response.CredentialExpiresAt) {
				return response, errors.New("invalid enrollment credential")
			}
			if err := securefs.WriteJSON(path, session{cfg.OutletURL, cfg.Name, response}); err != nil {
				return response, err
			}
			return response, nil
		}
		if os.IsNotExist(err) || saved.Enrollment.CredentialExpiresAt.IsZero() {
			return response, enrollErr
		}
		// Older installations may still have a consumed one-time token in the
		// command. Their saved credential remains the recovery path.
	}
	if err == nil {
		if !credentialCurrent(saved.Enrollment.CredentialExpiresAt) {
			return contract.EnrollmentResponse{}, errors.New("connector credential expired; issue a new enrollment token and use a new data directory")
		}
		if !validation.Token(saved.Enrollment.Credential) {
			return contract.EnrollmentResponse{}, errors.New("invalid saved connector credential")
		}
		remote, err := api.Configuration(ctx, saved.Enrollment.Credential)
		if err != nil {
			return contract.EnrollmentResponse{}, err
		}
		if remote.ConnectorID != saved.Enrollment.ConnectorID {
			return contract.EnrollmentResponse{}, errors.New("outlet returned a different identity")
		}
		if len(remote.ActiveServices()) != 0 {
			if err := allowed(cfg, remote); err != nil {
				return contract.EnrollmentResponse{}, err
			}
		}
		saved.Enrollment.Configuration = remote
		if err := securefs.WriteJSON(path, saved); err != nil {
			return contract.EnrollmentResponse{}, err
		}
		return saved.Enrollment, nil
	}
	return contract.EnrollmentResponse{}, errors.New("a valid tunnel token is required")
}

// Wait only when no service can be reached. A missing new target must not keep
// already reachable services from starting their tunnels. Re-read configuration
// so an operator can correct an unavailable target without recreating the Connector.
func waitForTargets(ctx context.Context, cfg Config, api *transport.Client, response *contract.EnrollmentResponse, log *slog.Logger) (map[string]string, error) {
	for {
		services := response.ActiveServices()
		addresses := make(map[string]string, len(services))
		healths := make(map[string]string, len(services))
		for _, service := range services {
			address, err := health.Resolve(ctx, service.Target)
			if err != nil {
				healths[service.ServiceID] = "unhealthy"
				log.Warn("connector waiting for local target", "service_id", service.ServiceID)
				continue
			}
			addresses[service.ServiceID] = address
			healths[service.ServiceID] = health.Check(ctx, service.Target)
		}
		if len(addresses) != 0 {
			return addresses, nil
		}
		hb := contract.Heartbeat{ConnectorID: response.ConnectorID, Revision: response.Revision, Version: contract.Version, ProcessState: "not_started", ServiceHealth: "unknown", ServiceHealths: healths, ObservedAt: time.Now().UTC()}
		if err := api.Heartbeat(ctx, response.Credential, hb); err != nil {
			var httpErr *transport.HTTPError
			if errors.As(err, &httpErr) && (httpErr.Status == 401 || httpErr.Status == 403) {
				return nil, errors.New("outlet rejected connector credential")
			}
			log.Warn("heartbeat failed while waiting for local targets")
		}
		timer := time.NewTimer(5 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, nil
		case <-timer.C:
		}
		if !credentialCurrent(response.CredentialExpiresAt) {
			return nil, errors.New("connector credential expired while waiting for local targets")
		}
		remote, err := api.Configuration(ctx, response.Credential)
		if err != nil {
			var httpErr *transport.HTTPError
			if errors.As(err, &httpErr) && (httpErr.Status == 401 || httpErr.Status == 403) {
				return nil, errors.New("outlet rejected connector credential")
			}
			log.Warn("cannot fetch connector configuration while waiting for local targets")
			continue
		}
		if remote.ConnectorID != response.ConnectorID {
			return nil, errors.New("outlet returned a different connector")
		}
		if remote.Revision != response.Revision {
			if err := allowed(cfg, remote); err != nil {
				return nil, err
			}
			response.Configuration = remote
			if err := securefs.WriteJSON(filepath.Join(cfg.DataDir, "session.json"), session{cfg.OutletURL, cfg.Name, *response}); err != nil {
				return nil, err
			}
		}
	}
}

func Run(ctx context.Context, cfg Config, token string, log *slog.Logger) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	unlock, err := securefs.Lock(cfg.DataDir)
	if err != nil {
		return err
	}
	defer unlock()
	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	err = rathole.VerifyBinary(checkCtx, cfg.RatholeBinary)
	cancel()
	if err != nil {
		return err
	}
	api, err := transport.New(cfg.OutletURL, cfg.RootCA)
	if err != nil {
		return err
	}
	defer api.Close()
	var response contract.EnrollmentResponse
	var addresses map[string]string
	for attempt := 0; attempt < 6; attempt++ {
		response, err = enroll(ctx, cfg, api, token)
		if err == nil {
			break
		}
		var httpErr *transport.HTTPError
		if errors.As(err, &httpErr) && httpErr.Status < 500 {
			break
		}
		if attempt == 5 {
			break
		}
		log.Warn("connector startup waiting for outlet", "attempt", attempt+1)
		timer := time.NewTimer(time.Duration(1<<attempt) * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
	token = ""
	if err != nil {
		return err
	}
	for {
		if len(response.ActiveServices()) == 0 {
			log.Info("connector connected; waiting for a service")
		}
		for len(response.ActiveServices()) == 0 {
			hb := contract.Heartbeat{ConnectorID: response.ConnectorID, Revision: response.Revision, Version: contract.Version, ProcessState: "running", ServiceHealth: "unknown", ObservedAt: time.Now().UTC()}
			if heartbeatErr := api.Heartbeat(ctx, response.Credential, hb); heartbeatErr != nil {
				log.Warn("heartbeat failed while waiting for services")
			}
			timer := time.NewTimer(5 * time.Second)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil
			case <-timer.C:
			}
			if !credentialCurrent(response.CredentialExpiresAt) {
				return errors.New("connector credential expired while waiting for a service")
			}
			remote, configErr := api.Configuration(ctx, response.Credential)
			if configErr != nil {
				var httpErr *transport.HTTPError
				if errors.As(configErr, &httpErr) && (httpErr.Status == 401 || httpErr.Status == 403) {
					return errors.New("outlet rejected connector credential")
				}
				log.Warn("cannot fetch connector configuration")
				continue
			}
			if remote.ConnectorID != response.ConnectorID {
				return errors.New("outlet returned a different connector")
			}
			if len(remote.ActiveServices()) != 0 {
				if err := allowed(cfg, remote); err != nil {
					return err
				}
				response.Configuration = remote
				if err := securefs.WriteJSON(filepath.Join(cfg.DataDir, "session.json"), session{cfg.OutletURL, cfg.Name, response}); err != nil {
					return err
				}
			}
		}
		addresses, err = waitForTargets(ctx, cfg, api, &response, log)
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			return nil
		}
		if len(response.ActiveServices()) != 0 {
			break
		}
	}
	log.Info("connector authenticated", "connector_id", response.ConnectorID, "configured_service_count", len(response.ActiveServices()), "active_service_count", len(addresses))
	config, err := rathole.ClientsAt(response.Configuration, addresses)
	if err != nil {
		return err
	}
	path := filepath.Join(cfg.DataDir, "rathole-client.toml")
	if err := securefs.WriteReloadable(path, config); err != nil {
		return err
	}
	supervisor := rathole.NewSupervisor(cfg.RatholeBinary, path)
	childCtx, stopChild := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func(s *rathole.Supervisor, runCtx context.Context, result chan<- error) {
		result <- s.Run(runCtx)
	}(supervisor, childCtx, done)
	interval, _ := time.ParseDuration(cfg.HeartbeatInterval)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	lastHealth, lastProcess := "", ""
	report := func(reportCtx context.Context) error {
		process := supervisor.Snapshot()
		serviceHealths := map[string]string{}
		for _, service := range response.ActiveServices() {
			serviceHealths[service.ServiceID] = health.Check(reportCtx, service.Target)
		}
		serviceHealth := "unknown"
		if len(response.ActiveServices()) == 1 {
			serviceHealth = serviceHealths[response.ActiveServices()[0].ServiceID]
		}
		if serviceHealth != lastHealth || process.State != lastProcess {
			log.Info("connector status", "process_state", process.State, "service_health", serviceHealth, "restarts", process.Restarts)
			lastHealth, lastProcess = serviceHealth, process.State
		}
		hb := contract.Heartbeat{ConnectorID: response.ConnectorID, ServiceID: response.ServiceID, Revision: response.Revision, Version: contract.Version, ProcessState: process.State, ProcessRestarts: process.Restarts, ServiceHealth: serviceHealth, ServiceHealths: serviceHealths, ObservedAt: time.Now().UTC()}
		return api.Heartbeat(reportCtx, response.Credential, hb)
	}
	if err := report(ctx); err != nil {
		log.Warn("heartbeat failed")
	}
	refresh := func(remote contract.Configuration) error {
		if remote.ConnectorID != response.ConnectorID {
			return errors.New("outlet returned a different connector")
		}
		if remote.Revision == response.Revision {
			return nil
		}
		if remote.Revision < response.Revision {
			return errors.New("outlet returned an older connector configuration")
		}
		if err := allowed(cfg, remote); err != nil {
			return err
		}
		previous := make(map[string]contract.ServiceConfiguration)
		for _, service := range response.ActiveServices() {
			previous[service.ServiceID] = service
		}
		nextAddresses := make(map[string]string)
		for _, service := range remote.ActiveServices() {
			if old, ok := previous[service.ServiceID]; ok && old.Target == service.Target {
				if address, ok := addresses[service.ServiceID]; ok {
					nextAddresses[service.ServiceID] = address
					continue
				}
			}
			if address, err := health.Resolve(ctx, service.Target); err == nil {
				nextAddresses[service.ServiceID] = address
			} else {
				log.Warn("connector waiting for local target", "service_id", service.ServiceID)
			}
		}
		if len(nextAddresses) == 0 {
			// No old service can safely remain after removal or target change.
			return errors.New("no reachable services in updated configuration")
		}
		nextConfig, err := rathole.ClientsAt(remote, nextAddresses)
		if err != nil {
			return err
		}
		if err := securefs.WriteReloadable(path, nextConfig); err != nil {
			return err
		}
		response.Configuration = remote
		addresses = nextAddresses
		if err := securefs.WriteJSON(filepath.Join(cfg.DataDir, "session.json"), session{cfg.OutletURL, cfg.Name, response}); err != nil {
			return err
		}
		log.Info("connector configuration applied", "revision", response.Revision, "active_service_count", len(addresses))
		return nil
	}
	for {
		select {
		case <-ctx.Done():
			stopChild()
			<-done
			return nil
		case err := <-done:
			stopChild()
			finalCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			_ = report(finalCtx)
			cancel()
			return err
		case <-ticker.C:
			if !credentialCurrent(response.CredentialExpiresAt) {
				stopChild()
				<-done
				return errors.New("connector credential expired; tunnel stopped")
			}
			if err := report(ctx); err != nil {
				var httpErr *transport.HTTPError
				if errors.As(err, &httpErr) && (httpErr.Status == 401 || httpErr.Status == 403) {
					stopChild()
					<-done
					return errors.New("outlet rejected connector authorization; tunnel stopped")
				}
				if errors.As(err, &httpErr) && httpErr.Status == 409 {
					remote, err := api.Configuration(ctx, response.Credential)
					if err != nil {
						var configErr *transport.HTTPError
						if errors.As(err, &configErr) && (configErr.Status == 401 || configErr.Status == 403) {
							stopChild()
							<-done
							return errors.New("outlet rejected connector authorization; tunnel stopped")
						}
						log.Warn("cannot fetch updated connector configuration")
						continue
					}
					if err := refresh(remote); err != nil {
						stopChild()
						<-done
						return err
					}
					continue
				}
				log.Warn("heartbeat failed")
			}
			// Rathole reloads only newly reachable services. Existing control
			// channels keep running while the generated config is updated.
			nextAddresses := maps.Clone(addresses)
			for _, service := range response.ActiveServices() {
				if _, present := nextAddresses[service.ServiceID]; present {
					continue
				}
				if address, err := health.Resolve(ctx, service.Target); err == nil {
					nextAddresses[service.ServiceID] = address
				}
			}
			if len(nextAddresses) > len(addresses) {
				nextConfig, err := rathole.ClientsAt(response.Configuration, nextAddresses)
				if err != nil {
					stopChild()
					<-done
					return err
				}
				if err := securefs.WriteReloadable(path, nextConfig); err != nil {
					stopChild()
					<-done
					return err
				}
				addresses = nextAddresses
				log.Info("connector activated reachable service", "service_count", len(addresses))
			}
		}
	}
}
