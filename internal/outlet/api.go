package outlet

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/lawnect/meerkat/internal/contract"
	"github.com/lawnect/meerkat/internal/control"
	"github.com/lawnect/meerkat/internal/dashboard"
	"github.com/lawnect/meerkat/internal/validation"
)

func isLoopbackAddress(address string) bool {
	host, _, err := net.SplitHostPort(address)
	ip := net.ParseIP(host)
	return err == nil && ip != nil && ip.IsLoopback()
}

func matches(raw, hash string) bool {
	return validation.Token(raw) && subtle.ConstantTimeCompare([]byte(Hash(raw)), []byte(hash)) == 1
}

func bearer(r *http.Request) string {
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, "Bearer ") {
		return ""
	}
	return strings.TrimPrefix(header, "Bearer ")
}

func decode(w http.ResponseWriter, r *http.Request, v any, required ...string) bool {
	if r.Header.Get("Content-Type") != "application/json" {
		fail(w, r, 415, "json_required")
		return false
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384))
	var raw json.RawMessage
	if err := d.Decode(&raw); err != nil {
		fail(w, r, 400, "invalid_request")
		return false
	}
	if err := d.Decode(new(any)); err != io.EOF {
		fail(w, r, 400, "invalid_request")
		return false
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		fail(w, r, 400, "invalid_request")
		return false
	}
	for _, key := range required {
		value, ok := fields[key]
		if !ok || bytes.Equal(value, []byte("null")) {
			fail(w, r, 400, "invalid_request")
			return false
		}
	}
	strict := json.NewDecoder(bytes.NewReader(raw))
	strict.DisallowUnknownFields()
	if strict.Decode(v) != nil {
		fail(w, r, 400, "invalid_request")
		return false
	}
	return true
}

func reply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, _ *http.Request, status int, code string) {
	reply(w, status, contract.Error{Code: code, RequestID: w.Header().Get("X-Request-ID")})
}

func (s *Store) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /", dashboard.Handler())
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { reply(w, 200, map[string]string{"status": "ok"}) })
	mux.HandleFunc("POST /connector/v1/enrollment", s.enroll)
	mux.HandleFunc("GET /connector/v1/config", s.configuration)
	mux.HandleFunc("POST /connector/v1/heartbeat", s.heartbeat)
	mux.HandleFunc("GET /api/v1/setup/status", s.setupStatus)
	mux.HandleFunc("POST /api/v1/setup/admin", s.setupAdmin)
	mux.HandleFunc("POST /api/v1/session", s.login)
	mux.HandleFunc("DELETE /api/v1/session", s.logout)
	mux.HandleFunc("GET /api/v1/connectors", s.list)
	mux.HandleFunc("POST /api/v1/connectors", s.createConnector)
	mux.HandleFunc("POST /api/v1/connectors/{id}/enrollment-tokens", s.issueToken)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Request-ID", Token())
		mux.ServeHTTP(w, r)
	})
}

func (s *Store) enroll(w http.ResponseWriter, r *http.Request) {
	var req contract.EnrollmentRequest
	if !decode(w, r, &req, "connector_name", "connector_version", "public_key") {
		return
	}
	if !validation.Label(req.Name) || !validation.PublicKey(req.PublicKey) || req.Version == "" || len(req.Version) > 64 {
		fail(w, r, 400, "invalid_identity")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, record := range s.records {
		if !matches(bearer(r), record.EnrollmentHash) {
			continue
		}
		if !s.now().Before(record.EnrollmentExpiresAt) {
			fail(w, r, 401, "invalid_enrollment_token")
			return
		}
		if record.Name != req.Name {
			fail(w, r, 403, "connector_mismatch")
			return
		}
		if record.Enrolled {
			fail(w, r, 409, "already_enrolled")
			return
		}
		credential := Token()
		record.Enrolled, record.PublicKey = true, req.PublicKey
		record.CredentialHash = Hash(credential)
		record.CredentialExpiresAt = s.now().Add(time.Duration(s.config.CredentialTTLSeconds) * time.Second)
		record.EnrollmentHash = "" // consumed durably before responding
		if err := s.save(id, record); err != nil {
			fail(w, r, 503, "persistence_failed")
			return
		}
		reply(w, 201, contract.EnrollmentResponse{Configuration: record.Configuration, Credential: credential, CredentialExpiresAt: record.CredentialExpiresAt})
		return
	}
	fail(w, r, 401, "invalid_enrollment_token")
}

// Called only while the store lock is held.
func (s *Store) authenticated(r *http.Request) (string, Record, bool) {
	for id, record := range s.records {
		if record.Enrolled && s.now().Before(record.CredentialExpiresAt) && matches(bearer(r), record.CredentialHash) {
			return id, record, true
		}
	}
	return "", Record{}, false
}

func (s *Store) configuration(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, record, ok := s.authenticated(r)
	if !ok {
		fail(w, r, 401, "invalid_credential")
		return
	}
	reply(w, 200, record.Configuration)
}

var processStates = map[string]bool{"not_started": true, "starting": true, "running": true, "restarting": true, "stopped": true, "failed": true}

func (s *Store) heartbeat(w http.ResponseWriter, r *http.Request) {
	var hb contract.Heartbeat
	if !decode(w, r, &hb, "connector_id", "service_id", "revision", "connector_version", "process_state", "process_restarts", "service_health", "observed_at") {
		return
	}
	s.mu.Lock()
	id, record, ok := s.authenticated(r)
	if !ok {
		s.mu.Unlock()
		fail(w, r, 401, "invalid_credential")
		return
	}
	if hb.ConnectorID != id || hb.ServiceID != record.Configuration.ServiceID {
		s.mu.Unlock()
		fail(w, r, 403, "service_not_allowed")
		return
	}
	if hb.Revision != record.Configuration.Revision {
		s.mu.Unlock()
		fail(w, r, 409, "revision_mismatch")
		return
	}
	if !processStates[hb.ProcessState] || hb.ProcessRestarts < 0 || (hb.ServiceHealth != "healthy" && hb.ServiceHealth != "unhealthy" && hb.ServiceHealth != "unknown") || hb.Version == "" || len(hb.Version) > 64 || hb.ObservedAt.IsZero() || hb.ObservedAt.After(s.now().Add(5*time.Minute)) {
		s.mu.Unlock()
		fail(w, r, 400, "invalid_heartbeat")
		return
	}
	s.mu.Unlock()
	// An open server listening port alone is not proof of an active tunnel.
	// An application response through that port confirms the HTTP forwarding path.
	probe := "disconnected"
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true}
	client := &http.Client{Transport: transport, Timeout: 2 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	probeReq, _ := http.NewRequestWithContext(r.Context(), "GET", "http://"+record.ProbeAddress+"/", nil)
	if res, err := client.Do(probeReq); err == nil {
		if res.StatusCode < 500 {
			probe = "connected"
		}
		res.Body.Close()
	}
	transport.CloseIdleConnections()
	s.mu.Lock()
	defer s.mu.Unlock()
	// Re-authenticate after the probe in case a credential was rotated meanwhile.
	_, record, ok = s.authenticated(r)
	if !ok {
		fail(w, r, 401, "invalid_credential")
		return
	}
	if record.LastHeartbeat != nil && hb.ObservedAt.Before(record.LastHeartbeat.ObservedAt) {
		fail(w, r, 409, "out_of_order_heartbeat")
		return
	}
	record.LastHeartbeat, record.LastSeen, record.TunnelStatus = &hb, s.now().UTC(), probe
	if err := s.save(id, record); err != nil {
		fail(w, r, 503, "persistence_failed")
		return
	}
	reply(w, 200, map[string]string{"status": "accepted"})
}

func (s *Store) admin(w http.ResponseWriter, r *http.Request) bool {
	if s.control != nil {
		complete, err := s.control.SetupComplete(r.Context())
		if err != nil {
			fail(w, r, 503, "persistence_failed")
			return false
		}
		if complete {
			cookie, err := r.Cookie("meerkat_session")
			if err == nil {
				if _, err = s.control.Authenticate(r.Context(), cookie.Value); err == nil {
					return true
				}
			}
			fail(w, r, 401, "invalid_admin_session")
			return false
		}
	}
	if !matches(bearer(r), s.config.AdminTokenHash) {
		fail(w, r, 401, "invalid_admin_credential")
		return false
	}
	return true
}

type adminCredentials struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (s *Store) setupStatus(w http.ResponseWriter, r *http.Request) {
	if s.control == nil {
		fail(w, r, 503, "control_database_unavailable")
		return
	}
	complete, err := s.control.SetupComplete(r.Context())
	if err != nil {
		fail(w, r, 503, "persistence_failed")
		return
	}
	reply(w, 200, map[string]bool{"setup_complete": complete})
}

func (s *Store) setupAdmin(w http.ResponseWriter, r *http.Request) {
	if s.control == nil {
		fail(w, r, 503, "control_database_unavailable")
		return
	}
	var req adminCredentials
	if !decode(w, r, &req, "email", "password") {
		return
	}
	_, err := s.control.Setup(r.Context(), s.config.AdminTokenHash, bearer(r), req.Email, req.Password)
	if errors.Is(err, control.ErrAlreadySetup) {
		fail(w, r, 409, "setup_complete")
		return
	}
	if err != nil {
		fail(w, r, 401, "invalid_setup_credential")
		return
	}
	reply(w, 201, map[string]string{"status": "created"})
}

func (s *Store) login(w http.ResponseWriter, r *http.Request) {
	if s.control == nil {
		fail(w, r, 503, "control_database_unavailable")
		return
	}
	var req adminCredentials
	if !decode(w, r, &req, "email", "password") {
		return
	}
	session, err := s.control.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		fail(w, r, 401, "invalid_credentials")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "meerkat_session", Value: session.Token, Path: "/", Expires: session.ExpiresAt, MaxAge: int(time.Until(session.ExpiresAt).Seconds()), HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode})
	reply(w, 201, map[string]any{"admin": session.Admin, "expires_at": session.ExpiresAt})
}

func (s *Store) logout(w http.ResponseWriter, r *http.Request) {
	if s.control != nil {
		if cookie, err := r.Cookie("meerkat_session"); err == nil {
			if err := s.control.RevokeSession(r.Context(), cookie.Value); err != nil {
				fail(w, r, 503, "persistence_failed")
				return
			}
		}
	}
	http.SetCookie(w, &http.Cookie{Name: "meerkat_session", Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode})
	w.WriteHeader(http.StatusNoContent)
}

type Status struct {
	ID                  string              `json:"id"`
	Name                string              `json:"name"`
	ServiceID           string              `json:"service_id"`
	Status              string              `json:"status"`
	TunnelStatus        string              `json:"tunnel_status"`
	LastSeen            time.Time           `json:"last_seen"`
	Heartbeat           *contract.Heartbeat `json:"heartbeat"`
	CredentialExpiresAt time.Time           `json:"credential_expires_at"`
}

func (s *Store) list(w http.ResponseWriter, r *http.Request) {
	if !s.admin(w, r) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	statuses := []Status{}
	for id, record := range s.records {
		state, tunnel := "pending", record.TunnelStatus
		if record.Enrolled {
			state = "disconnected"
		}
		age := s.now().Sub(record.LastSeen)
		if !record.LastSeen.IsZero() && age < 15*time.Second {
			state = "connected"
		} else if !record.LastSeen.IsZero() && age < 60*time.Second {
			state = "stale"
		}
		if state != "connected" {
			tunnel = "unknown"
		}
		statuses = append(statuses, Status{id, record.Name, record.Configuration.ServiceID, state, tunnel, record.LastSeen, record.LastHeartbeat, record.CredentialExpiresAt})
	}
	reply(w, 200, map[string]any{"connectors": statuses})
}

func (s *Store) issueToken(w http.ResponseWriter, r *http.Request) {
	if !s.admin(w, r) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	id := r.PathValue("id")
	record, ok := s.records[id]
	if !ok {
		fail(w, r, 404, "connector_not_found")
		return
	}
	// A fresh token can recover an expired identity. Active credentials cannot be
	// silently replaced. Tunnel-token revocation belongs to server config rollout.
	if record.Enrolled && s.now().Before(record.CredentialExpiresAt) {
		fail(w, r, 409, "already_enrolled")
		return
	}
	token := Token()
	record.Enrolled = false
	record.PublicKey, record.CredentialHash = "", ""
	record.LastHeartbeat, record.LastSeen, record.TunnelStatus = nil, time.Time{}, "unknown"
	record.EnrollmentHash, record.EnrollmentExpiresAt = Hash(token), s.now().Add(time.Duration(s.config.EnrollmentTTLSeconds)*time.Second)
	if err := s.save(id, record); err != nil {
		fail(w, r, 503, "persistence_failed")
		return
	}
	reply(w, 201, map[string]any{"token": token, "expires_at": record.EnrollmentExpiresAt})
}

type connectorRequest struct {
	Name string `json:"name"`
}

func (s *Store) createConnector(w http.ResponseWriter, r *http.Request) {
	if !s.admin(w, r) {
		return
	}
	if s.control == nil {
		fail(w, r, 503, "control_database_unavailable")
		return
	}
	var req connectorRequest
	if !decode(w, r, &req, "name") {
		return
	}
	connector, err := s.control.CreateConnector(r.Context(), req.Name)
	if errors.Is(err, control.ErrConnectorExists) {
		fail(w, r, 409, "connector_name_taken")
		return
	}
	if err != nil {
		fail(w, r, 400, "invalid_connector")
		return
	}
	reply(w, 201, connector)
}
