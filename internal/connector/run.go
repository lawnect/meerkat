package connector

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"log/slog"
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
	if local.Target != (contract.Target{}) && local.Target != remote.Target {
		return errors.New("outlet target is not in the connector's local allowlist")
	}
	return nil
}

func enroll(ctx context.Context, cfg Config, api *transport.Client, token string) (contract.EnrollmentResponse, error) {
	path := filepath.Join(cfg.DataDir, "session.json")
	var saved session
	err := securefs.ReadJSON(path, &saved)
	if err == nil {
		if saved.OutletURL != cfg.OutletURL || saved.Name != cfg.Name {
			return contract.EnrollmentResponse{}, errors.New("saved identity belongs to another outlet or connector")
		}
		if !time.Now().Before(saved.Enrollment.CredentialExpiresAt) {
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
		if remote.ServiceID != "" {
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
	if !os.IsNotExist(err) {
		return contract.EnrollmentResponse{}, err
	}
	if !validation.Token(token) {
		return contract.EnrollmentResponse{}, errors.New("a valid one-time enrollment token is required")
	}
	public, err := publicIdentity(cfg.DataDir)
	if err != nil {
		return contract.EnrollmentResponse{}, err
	}
	response, err := api.Enroll(ctx, token, contract.EnrollmentRequest{Name: cfg.Name, Version: contract.Version, PublicKey: public})
	if err != nil {
		return response, err
	}
	if response.ServiceID != "" {
		if err := allowed(cfg, response.Configuration); err != nil {
			return response, err
		}
	}
	if !validation.Token(response.Credential) || !time.Now().Before(response.CredentialExpiresAt) {
		return response, errors.New("invalid enrollment credential")
	}
	if err := securefs.WriteJSON(path, session{cfg.OutletURL, cfg.Name, response}); err != nil {
		return response, err
	}
	return response, nil
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
	var address string
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
	if response.ServiceID == "" {
		log.Info("connector connected; waiting for a service")
	}
	for response.ServiceID == "" {
		timer := time.NewTimer(5 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
		if !time.Now().Before(response.CredentialExpiresAt) {
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
		if remote.ServiceID != "" {
			if err := allowed(cfg, remote); err != nil {
				return err
			}
			response.Configuration = remote
			if err := securefs.WriteJSON(filepath.Join(cfg.DataDir, "session.json"), session{cfg.OutletURL, cfg.Name, response}); err != nil {
				return err
			}
		}
	}
	for attempt := 0; attempt < 6; attempt++ {
		address, err = health.Resolve(ctx, response.Target)
		if err == nil {
			break
		}
		if attempt == 5 {
			return err
		}
		log.Warn("connector waiting for local target", "attempt", attempt+1)
		timer := time.NewTimer(time.Duration(1<<attempt) * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
	log.Info("connector authenticated", "connector_id", response.ConnectorID, "service_id", response.ServiceID)
	config, err := rathole.ClientAt(response.Configuration, address)
	if err != nil {
		return err
	}
	path := filepath.Join(cfg.DataDir, "rathole-client.toml")
	if err := securefs.Write(path, config); err != nil {
		return err
	}
	supervisor := rathole.NewSupervisor(cfg.RatholeBinary, path)
	childCtx, stopChild := context.WithCancel(ctx)
	defer stopChild()
	done := make(chan error, 1)
	go func() { done <- supervisor.Run(childCtx) }()
	interval, _ := time.ParseDuration(cfg.HeartbeatInterval)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	lastHealth, lastProcess := "", ""
	report := func(reportCtx context.Context) error {
		process := supervisor.Snapshot()
		serviceHealth := health.Check(reportCtx, response.Target)
		if serviceHealth != lastHealth || process.State != lastProcess {
			log.Info("connector status", "process_state", process.State, "service_health", serviceHealth, "restarts", process.Restarts)
			lastHealth, lastProcess = serviceHealth, process.State
		}
		hb := contract.Heartbeat{ConnectorID: response.ConnectorID, ServiceID: response.ServiceID, Revision: response.Revision, Version: contract.Version, ProcessState: process.State, ProcessRestarts: process.Restarts, ServiceHealth: serviceHealth, ObservedAt: time.Now().UTC()}
		return api.Heartbeat(reportCtx, response.Credential, hb)
	}
	if err := report(ctx); err != nil {
		log.Warn("heartbeat failed")
	}
	for {
		select {
		case <-ctx.Done():
			stopChild()
			<-done
			return nil
		case err := <-done:
			finalCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			_ = report(finalCtx)
			cancel()
			return err
		case <-ticker.C:
			if !time.Now().Before(response.CredentialExpiresAt) {
				stopChild()
				<-done
				return errors.New("connector credential expired; tunnel stopped")
			}
			if err := report(ctx); err != nil {
				var httpErr *transport.HTTPError
				if errors.As(err, &httpErr) && (httpErr.Status == 401 || httpErr.Status == 403 || httpErr.Status == 409) {
					stopChild()
					<-done
					return errors.New("outlet rejected connector authorization or revision; tunnel stopped")
				}
				log.Warn("heartbeat failed")
			}
		}
	}
}
