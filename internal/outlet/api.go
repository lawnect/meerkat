package outlet

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/lawnect/meerkat/internal/contract"
	"github.com/lawnect/meerkat/internal/control"
	"github.com/lawnect/meerkat/internal/dashboard"
	"github.com/lawnect/meerkat/internal/securefs"
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
	return decodeLimited(w, r, v, 16384, required...)
}

// decodeLimited is decode for a body of up to limit bytes.
func decodeLimited(w http.ResponseWriter, r *http.Request, v any, limit int64, required ...string) bool {
	if r.Header.Get("Content-Type") != "application/json" {
		fail(w, r, 415, "json_required")
		return false
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
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
	mux.HandleFunc("POST /api/v1/setup/recover", s.recoverAdmin)
	mux.HandleFunc("POST /api/v1/session", s.login)
	mux.HandleFunc("POST /api/v1/session/second-factor", s.secondFactor)
	mux.HandleFunc("DELETE /api/v1/session", s.logout)
	mux.HandleFunc("GET /api/v1/audit", s.getAudit)
	mux.HandleFunc("GET /api/v1/security/rate-limit", s.getRateLimit)
	mux.HandleFunc("PUT /api/v1/security/rate-limit", s.putRateLimit)
	mux.HandleFunc("GET /api/v1/security/connection-limits", s.getConnectionLimits)
	mux.HandleFunc("PUT /api/v1/security/connection-limits", s.putConnectionLimits)
	mux.HandleFunc("GET /api/v1/security/block-rules", s.getBlockRules)
	mux.HandleFunc("PUT /api/v1/security/block-rules", s.putBlockRules)
	mux.HandleFunc("GET /api/v1/security/sso", s.getSSO)
	mux.HandleFunc("PUT /api/v1/security/sso", s.putSSO)
	mux.HandleFunc("DELETE /api/v1/security/sso", s.resetSSO)
	mux.HandleFunc("POST /api/v1/security/sso/test", s.testSSO)
	mux.HandleFunc("GET "+ssoPath+"/start", s.ssoStart)
	mux.HandleFunc("GET "+ssoCallbackPath, s.ssoCallback)
	mux.HandleFunc("GET /api/v1/alerts", s.getAlerts)
	mux.HandleFunc("PUT /api/v1/alerts/telegram", s.putTelegram)
	mux.HandleFunc("POST /api/v1/alerts/telegram/test", s.testTelegram)
	mux.HandleFunc("DELETE /api/v1/alerts/telegram", s.deleteTelegram)
	mux.HandleFunc("PUT /api/v1/alerts/rules", s.putAlertRules)
	mux.HandleFunc("PUT /api/v1/security/second-factor", s.putSecondFactor)
	mux.HandleFunc("GET /api/v1/outlet", s.getOutlet)
	mux.HandleFunc("POST /api/v1/outlet/domains/{domain}/check-dns", s.checkOutletDomainDNS)
	mux.HandleFunc("PUT /api/v1/outlet", s.updateOutlet)
	mux.HandleFunc("POST /api/v1/outlet/domains", s.addOutletDomain)
	mux.HandleFunc("DELETE /api/v1/outlet/domains/{domain}", s.deleteOutletDomain)
	mux.HandleFunc("PUT /api/v1/outlet/domains/{domain}/mode", s.updateOutletDomainMode)
	mux.HandleFunc("PUT /api/v1/outlet/tunnel", s.updateTunnel)
	mux.HandleFunc("POST /api/v1/outlet/tunnel/check", s.checkTunnel)
	mux.HandleFunc("GET /api/v1/diagnostics/trace", s.trace)
	mux.HandleFunc("GET /api/v1/connectors", s.list)
	mux.HandleFunc("GET /api/v1/usage", s.outletUsage)
	mux.HandleFunc("GET /api/v1/connectors/{id}/usage", s.connectorUsage)
	mux.HandleFunc("POST /api/v1/connectors", s.createConnector)
	mux.HandleFunc("PATCH /api/v1/connectors/{id}", s.updateConnector)
	mux.HandleFunc("DELETE /api/v1/connectors/{id}", s.deleteConnector)
	mux.HandleFunc("GET /api/v1/connectors/{id}/services", s.listServices)
	mux.HandleFunc("POST /api/v1/connectors/{id}/services", s.createService)
	mux.HandleFunc("PATCH /api/v1/connectors/{id}/services/{service_id}", s.updateServiceTarget)
	mux.HandleFunc("DELETE /api/v1/connectors/{id}/services/{service_id}", s.deleteService)
	mux.HandleFunc("GET /api/v1/services/{id}/routes", s.listRoutes)
	mux.HandleFunc("POST /api/v1/services/{id}/routes", s.createHTTPRoute)
	mux.HandleFunc("DELETE /api/v1/services/{id}/routes/{route_id}", s.deleteHTTPRoute)
	mux.HandleFunc("PUT /api/v1/addresses/{hostname}", s.updateAddress)
	mux.HandleFunc("GET "+accessStartPath, s.accessStart)
	mux.HandleFunc("GET "+accessCallbackPath, s.accessCallback)
	mux.HandleFunc("GET /api/v1/security/access-provider", s.getAccessProvider)
	mux.HandleFunc("PUT /api/v1/security/access-provider", s.putAccessProvider)
	mux.HandleFunc("POST /api/v1/security/access-provider/test", s.testAccessProvider)
	mux.HandleFunc("POST /api/v1/security/access/sessions/revoke", s.revokeAccessSessions)
	mux.HandleFunc("GET /api/v1/security/access-policies", s.getAccessPolicies)
	mux.HandleFunc("PUT /api/v1/security/access-policies", s.putAccessPolicies)
	mux.HandleFunc("GET /api/v1/observe/access-metrics", s.getAccessMetrics)
	mux.HandleFunc("GET /api/v1/observe/access-log", s.getAccessLog)
	mux.HandleFunc("PUT /api/v1/observe/access-log", s.putAccessLog)
	mux.HandleFunc("GET /api/v1/observe/failure-log", s.getFailureLog)
	mux.HandleFunc("PUT /api/v1/observe/failure-log", s.putFailureLog)
	mux.HandleFunc("GET /api/v1/security/access-tokens", s.getAccessTokens)
	mux.HandleFunc("POST /api/v1/security/access-tokens", s.createAccessToken)
	mux.HandleFunc("PUT /api/v1/security/access-tokens/{id}", s.updateAccessToken)
	mux.HandleFunc("DELETE /api/v1/security/access-tokens/{id}", s.deleteAccessToken)
	mux.HandleFunc("POST /api/v1/connectors/{id}/enrollment-tokens", s.issueToken)
	// Every public route is same-site with the management host, so
	// SameSite=Strict still lets a tunneled app post a form to the admin API
	// with the session cookie. Browsers label such writes cross-origin.
	csrf := http.NewCrossOriginProtection()
	csrf.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fail(w, r, http.StatusForbidden, "cross_origin_request")
	}))
	protected := csrf.Handler(mux)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Request-ID", Token())
		if _, public := requestOrigin(r); public {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000")
		}
		protected.ServeHTTP(w, r)
	})
}

type traceSnapshot struct {
	ObservedAt time.Time    `json:"observed_at"`
	Outlet     traceOutlet  `json:"outlet"`
	Request    traceRequest `json:"request"`
}

type traceOutlet struct {
	Hostname string `json:"hostname"`
	Region   string `json:"region,omitempty"`
}

type traceRequest struct {
	RemoteAddress     string `json:"remote_address"`
	Host              string `json:"host"`
	Protocol          string `json:"protocol"`
	Method            string `json:"method"`
	ForwardedFor      string `json:"forwarded_for,omitempty"`
	ForwardedHost     string `json:"forwarded_host,omitempty"`
	ForwardedProto    string `json:"forwarded_proto,omitempty"`
	CloudflareRay     string `json:"cloudflare_ray,omitempty"`
	CloudflareColo    string `json:"cloudflare_colo,omitempty"`
	CloudflareCountry string `json:"cloudflare_country,omitempty"`
}

func (s *Store) trace(w http.ResponseWriter, r *http.Request) {
	if !s.admin(w, r) {
		return
	}
	hostname, _ := os.Hostname()
	cloudflareRay := r.Header.Get("CF-Ray")
	cloudflareColo := ""
	if dash := strings.LastIndexByte(cloudflareRay, '-'); dash >= 0 && dash+1 < len(cloudflareRay) {
		cloudflareColo = cloudflareRay[dash+1:]
	}
	protocol := "HTTP/" + strings.TrimPrefix(r.Proto, "HTTP/")
	snapshot := traceSnapshot{
		ObservedAt: time.Now().UTC(),
		Outlet:     traceOutlet{Hostname: hostname, Region: s.config.Region},
		Request: traceRequest{
			RemoteAddress:     r.RemoteAddr,
			Host:              r.Host,
			Protocol:          protocol,
			Method:            r.Method,
			ForwardedFor:      r.Header.Get("X-Forwarded-For"),
			ForwardedHost:     r.Header.Get("X-Forwarded-Host"),
			ForwardedProto:    r.Header.Get("X-Forwarded-Proto"),
			CloudflareRay:     cloudflareRay,
			CloudflareColo:    cloudflareColo,
			CloudflareCountry: r.Header.Get("CF-IPCountry"),
		},
	}
	reply(w, http.StatusOK, snapshot)
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
	// Only requests holding a valid token are recorded, after the lock is
	// released; a token-less guess is anyone's and stays out of the log.
	var event *control.AuditEntry
	defer func() {
		if event != nil {
			s.audit(r, event.Action, *event)
		}
	}()
	enrolled := func(id string, first bool) {
		if first {
			event = &control.AuditEntry{Action: "connector.enroll", ResourceType: "connector", ResourceID: id, Detail: map[string]any{"name": req.Name, "version": req.Version}}
		}
	}
	rejected := func(id, reason string) {
		event = &control.AuditEntry{Action: "connector.enroll_failed", ResourceType: "connector", ResourceID: id, Detail: map[string]any{"name": req.Name, "reason": reason}}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, record := range s.records {
		if matches(bearer(r), record.RunTokenHash) {
			if record.Name != req.Name {
				rejected(id, "connector_mismatch")
				fail(w, r, 403, "connector_mismatch")
				return
			}
			first := !record.Enrolled
			if first {
				record.Enrolled, record.PublicKey = true, req.PublicKey
				if err := s.save(id, record); err != nil {
					fail(w, r, 503, "persistence_failed")
					return
				}
			}
			enrolled(id, first)
			reply(w, 201, contract.EnrollmentResponse{Configuration: record.Configuration, Credential: bearer(r)})
			return
		}
		if !matches(bearer(r), record.EnrollmentHash) {
			continue
		}
		if !s.now().Before(record.EnrollmentExpiresAt) {
			rejected(id, "expired")
			fail(w, r, 401, "invalid_enrollment_token")
			return
		}
		if record.Name != req.Name {
			rejected(id, "connector_mismatch")
			fail(w, r, 403, "connector_mismatch")
			return
		}
		if record.Enrolled {
			rejected(id, "already_enrolled")
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
		enrolled(id, true)
		reply(w, 201, contract.EnrollmentResponse{Configuration: record.Configuration, Credential: credential, CredentialExpiresAt: record.CredentialExpiresAt})
		return
	}
	for id, record := range s.managed {
		if matches(bearer(r), record.RunTokenHash) {
			if record.Name != req.Name {
				rejected(id, "connector_mismatch")
				fail(w, r, 403, "connector_mismatch")
				return
			}
			first := !record.Enrolled
			if first {
				record.Enrolled, record.PublicKey = true, req.PublicKey
				if err := s.saveManaged(id, record); err != nil {
					fail(w, r, 503, "persistence_failed")
					return
				}
			}
			enrolled(id, first)
			reply(w, 201, contract.EnrollmentResponse{Configuration: record.Configuration, Credential: bearer(r)})
			return
		}
		if !matches(bearer(r), record.EnrollmentHash) {
			continue
		}
		if !s.now().Before(record.EnrollmentExpiresAt) {
			rejected(id, "expired")
			fail(w, r, 401, "invalid_enrollment_token")
			return
		}
		if record.Name != req.Name {
			rejected(id, "connector_mismatch")
			fail(w, r, 403, "connector_mismatch")
			return
		}
		if record.Enrolled {
			rejected(id, "already_enrolled")
			fail(w, r, 409, "already_enrolled")
			return
		}
		credential := Token()
		record.Enrolled, record.PublicKey = true, req.PublicKey
		record.CredentialHash = Hash(credential)
		record.CredentialExpiresAt = s.now().Add(time.Duration(s.config.CredentialTTLSeconds) * time.Second)
		record.EnrollmentHash = ""
		if err := s.saveManaged(id, record); err != nil {
			fail(w, r, 503, "persistence_failed")
			return
		}
		enrolled(id, true)
		reply(w, 201, contract.EnrollmentResponse{Configuration: record.Configuration, Credential: credential, CredentialExpiresAt: record.CredentialExpiresAt})
		return
	}
	fail(w, r, 401, "invalid_enrollment_token")
}

// Called only while the store lock is held.
func (s *Store) authenticated(r *http.Request) (string, Record, bool, bool) {
	for id, record := range s.records {
		if record.Enrolled && (matches(bearer(r), record.RunTokenHash) || s.now().Before(record.CredentialExpiresAt) && matches(bearer(r), record.CredentialHash)) {
			return id, record, false, true
		}
	}
	for id, record := range s.managed {
		if record.Enrolled && (matches(bearer(r), record.RunTokenHash) || s.now().Before(record.CredentialExpiresAt) && matches(bearer(r), record.CredentialHash)) {
			return id, record, true, true
		}
	}
	return "", Record{}, false, false
}

func (s *Store) configuration(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, record, managed, ok := s.authenticated(r)
	if !ok {
		fail(w, r, 401, "invalid_credential")
		return
	}
	if managed && len(record.Configuration.ActiveServices()) == 0 {
		record.LastSeen = s.now().UTC()
		s.observe(id, record, managed)
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
	id, record, managed, ok := s.authenticated(r)
	if !ok {
		s.mu.Unlock()
		fail(w, r, 401, "invalid_credential")
		return
	}
	if hb.ConnectorID != id || (len(record.Configuration.Services) == 0 && hb.ServiceID != record.Configuration.ServiceID) || (len(record.Configuration.Services) != 0 && hb.ServiceID != "") {
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
	for _, service := range record.Configuration.ActiveServices() {
		if len(record.Configuration.Services) != 0 && hb.ServiceHealths[service.ServiceID] != "healthy" && hb.ServiceHealths[service.ServiceID] != "unhealthy" && hb.ServiceHealths[service.ServiceID] != "unknown" {
			s.mu.Unlock()
			fail(w, r, 400, "invalid_heartbeat")
			return
		}
	}
	since := record.LastSeen
	s.mu.Unlock()
	// An open server listening port alone is not proof of an active tunnel.
	// An application response through that port confirms the HTTP forwarding
	// path, and so do proxied answers since the previous heartbeat: under
	// heavy load the probe alone can time out and take every route down.
	statuses := map[string]string{}
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true}
	client := &http.Client{Transport: transport, Timeout: 2 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	for _, service := range record.Configuration.ActiveServices() {
		probe := "disconnected"
		if address := record.probeFor(service.ServiceID); address != "" {
			probeReq, _ := http.NewRequestWithContext(r.Context(), "GET", "http://"+address+"/", nil)
			if res, err := client.Do(probeReq); err == nil {
				if res.StatusCode < 500 {
					probe = "connected"
				}
				res.Body.Close()
			}
		}
		if probe != "connected" && s.servedSince(service.ServiceID, since) {
			probe = "connected"
		}
		statuses[service.ServiceID] = probe
	}
	transport.CloseIdleConnections()
	s.mu.Lock()
	// Re-authenticate after the probe in case a credential was rotated meanwhile.
	_, record, managed, ok = s.authenticated(r)
	if !ok {
		s.mu.Unlock()
		fail(w, r, 401, "invalid_credential")
		return
	}
	if hb.Revision != record.Configuration.Revision {
		s.mu.Unlock()
		fail(w, r, 409, "revision_mismatch")
		return
	}
	if record.LastHeartbeat != nil && hb.ObservedAt.Before(record.LastHeartbeat.ObservedAt) {
		s.mu.Unlock()
		fail(w, r, 409, "out_of_order_heartbeat")
		return
	}
	record.LastHeartbeat, record.LastSeen, record.ServiceStatuses = &hb, s.now().UTC(), statuses
	record.TunnelStatus = "connected"
	if len(statuses) != 0 {
		record.TunnelStatus = "disconnected"
		for _, status := range statuses {
			if status == "connected" {
				record.TunnelStatus = "connected"
				break
			}
		}
	}
	s.observe(id, record, managed)
	s.mu.Unlock()
	reply(w, 200, map[string]string{"status": "accepted"})
}

func (s *Store) admin(w http.ResponseWriter, r *http.Request) bool {
	if s.control != nil {
		complete, err := s.setupComplete(r)
		if err != nil {
			fail(w, r, 503, "persistence_failed")
			return false
		}
		if complete {
			if _, ok := s.sessionAdmin(r); ok {
				return true
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
	complete, err := s.setupComplete(r)
	if err != nil {
		fail(w, r, 503, "persistence_failed")
		return
	}
	// The sign-in page shows the single sign-on button and hides the password
	// form where it is off. The local port over SSH always offers passwords,
	// and recovery with the bootstrap token. Anyone can read this, so it names
	// neither the version, which would match release notes, nor the provider.
	_, public := requestOrigin(r)
	signIn := map[string]any{"password": true, "sso": false, "recovery": !public}
	if public && complete {
		if sso, err := s.control.SSO(r.Context()); err == nil && sso.Enabled {
			signIn = map[string]any{"password": sso.PasswordLogin, "sso": true, "recovery": false}
		}
	}
	reply(w, 200, map[string]any{"setup_complete": complete, "sign_in": signIn})
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
	_, public := requestOrigin(r)
	admin, err := s.control.Setup(r.Context(), s.config.AdminTokenHash, bearer(r), req.Email, req.Password, public)
	if errors.Is(err, control.ErrAlreadySetup) {
		fail(w, r, 409, "setup_complete")
		return
	}
	if err != nil {
		fail(w, r, 401, "invalid_setup_credential")
		return
	}
	s.setupDone.Store(true)
	s.audit(r, "setup.admin_create", control.AuditEntry{ActorID: admin.ID, ActorEmail: admin.Email, ResourceType: "admin", ResourceID: admin.ID})
	reply(w, 201, map[string]string{"status": "created"})
}

// recoverAdmin sets an administrator's password with the bootstrap token. It
// exists only on the local admin port, reached over SSH, so an Outlet whose
// administrators all sign in with a broken provider can still be entered.
func (s *Store) recoverAdmin(w http.ResponseWriter, r *http.Request) {
	if _, public := requestOrigin(r); public {
		http.NotFound(w, r)
		return
	}
	if s.control == nil {
		fail(w, r, 503, "control_database_unavailable")
		return
	}
	if !s.allowLogin(w, r) {
		return
	}
	var req adminCredentials
	if !decode(w, r, &req, "email", "password") {
		return
	}
	admin, err := s.control.RecoverAdmin(r.Context(), s.config.AdminTokenHash, bearer(r), req.Email, req.Password)
	if errors.Is(err, control.ErrInvalidCredentials) {
		fail(w, r, 401, "invalid_setup_credential")
		return
	}
	if err != nil {
		fail(w, r, 503, "persistence_failed")
		return
	}
	s.setupDone.Store(true)
	s.audit(r, "setup.admin_recover", control.AuditEntry{ActorID: admin.ID, ActorEmail: admin.Email, ResourceType: "admin", ResourceID: admin.ID})
	reply(w, 200, map[string]string{"status": "recovered"})
}

// login checks the password. A request from the internet then needs the
// Telegram code when the second factor is on; the local admin port, reached
// over SSH, never does, so an operator can always get in.
func (s *Store) login(w http.ResponseWriter, r *http.Request) {
	if s.control == nil {
		fail(w, r, 503, "control_database_unavailable")
		return
	}
	if !s.allowLogin(w, r) {
		return
	}
	var req adminCredentials
	if !decode(w, r, &req, "email", "password") {
		return
	}
	_, public := requestOrigin(r)
	if public {
		// With single sign-on required, a password is never checked on the
		// internet; the local port over SSH keeps accepting it.
		if sso, err := s.control.SSO(r.Context()); err != nil {
			fail(w, r, 503, "persistence_failed")
			return
		} else if !sso.PasswordLogin {
			fail(w, r, http.StatusForbidden, "password_login_disabled")
			return
		}
		wait, ok := s.loginLimit.beginPasswordCheck(s.now(), s.trustedDevice(r))
		if !ok {
			w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
			fail(w, r, http.StatusTooManyRequests, "rate_limited")
			return
		}
	}
	admin, err := s.control.VerifyCredentials(r.Context(), req.Email, req.Password)
	if public {
		s.loginLimit.endPasswordCheck(err != nil, s.now())
	}
	if err != nil {
		s.audit(r, "session.login_failed", control.AuditEntry{ActorEmail: attemptedEmail(req.Email), ResourceType: "admin", ResourceID: "unknown", Detail: map[string]any{"reason": "invalid_credentials"}})
		fail(w, r, 401, "invalid_credentials")
		return
	}
	if public {
		required, err := s.control.LoginSecondFactor(r.Context())
		if err != nil {
			fail(w, r, 503, "persistence_failed")
			return
		}
		if required {
			s.startSecondFactor(w, r, admin)
			return
		}
	}
	s.finishLogin(w, r, admin, false)
}

func (s *Store) logout(w http.ResponseWriter, r *http.Request) {
	var admin control.Admin
	var active bool
	if s.control != nil {
		if cookie, err := r.Cookie(sessionCookieName); err == nil {
			admin, active = s.sessionAdmin(r)
			err := s.control.RevokeSession(r.Context(), cookie.Value)
			// Even when revoking failed, the next request asks the database.
			s.sessions.revoke(cookie.Value)
			if err != nil {
				fail(w, r, 503, "persistence_failed")
				return
			}
		}
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode})
	// A single sign-on session also ends at the provider, or the next
	// sign-in would go straight through without asking.
	logoutURL := s.ssoLogoutURL(r)
	http.SetCookie(w, &http.Cookie{Name: ssoIDTokenCookie, Value: "", Path: "/api/v1/session", MaxAge: -1, HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode})
	if active {
		s.audit(r, "session.logout", control.AuditEntry{ActorID: admin.ID, ActorEmail: admin.Email, ResourceType: "admin", ResourceID: admin.ID})
	}
	if logoutURL != "" {
		reply(w, 200, map[string]string{"logout_url": logoutURL})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Store) getOutlet(w http.ResponseWriter, r *http.Request) {
	if !s.admin(w, r) {
		return
	}
	if s.control == nil {
		fail(w, r, 503, "control_database_unavailable")
		return
	}
	outlet, err := s.control.Outlet(r.Context())
	if err != nil {
		fail(w, r, 503, "persistence_failed")
		return
	}
	s.replyOutlet(w, r, 200, outlet)
}

type outletTunnel struct {
	Address        string `json:"address"`
	DefaultAddress string `json:"default_address"`
	Custom         bool   `json:"custom"`
}

type outletResponse struct {
	control.Outlet
	Tunnel outletTunnel `json:"tunnel"`
	// Version is the Outlet's software version, also shown on the login page.
	Version string `json:"version"`
}

// tunnel resolves the endpoint Connectors dial: the administrator's setting,
// then outlet.json, then the management host on the rathole listen port. The
// management host already points at the Outlet for the Connector API, so the
// default needs no extra DNS record and leaves the domain root free.
func (s *Store) tunnel(ctx context.Context, adminHostname string) (outletTunnel, error) {
	tunnel := outletTunnel{DefaultAddress: s.config.TunnelRemoteAddress}
	if tunnel.DefaultAddress == "" && adminHostname != "" {
		if _, port, err := net.SplitHostPort(s.config.TunnelListenAddress); err == nil {
			tunnel.DefaultAddress = net.JoinHostPort(adminHostname, port)
		}
	}
	custom, err := s.control.TunnelAddress(ctx)
	if err != nil {
		return outletTunnel{}, err
	}
	tunnel.Address, tunnel.Custom = tunnel.DefaultAddress, custom != ""
	if tunnel.Custom {
		tunnel.Address = custom
	}
	return tunnel, nil
}

func (s *Store) replyOutlet(w http.ResponseWriter, r *http.Request, status int, outlet control.Outlet) {
	tunnel, err := s.tunnel(r.Context(), outlet.AdminHostname)
	if err != nil {
		fail(w, r, 503, "persistence_failed")
		return
	}
	reply(w, status, outletResponse{Outlet: outlet, Tunnel: tunnel, Version: contract.Version})
}

type dnsRecordCheck struct {
	Hostname  string   `json:"hostname"`
	Addresses []string `json:"addresses"`
	Error     string   `json:"error,omitempty"`
}

type domainDNSCheck struct {
	CheckedAt time.Time        `json:"checked_at"`
	Records   []dnsRecordCheck `json:"records"`
}

func (s *Store) checkOutletDomainDNS(w http.ResponseWriter, r *http.Request) {
	if !s.admin(w, r) {
		return
	}
	if s.control == nil {
		fail(w, r, 503, "control_database_unavailable")
		return
	}
	outlet, err := s.control.Outlet(r.Context())
	if err != nil {
		fail(w, r, 503, "persistence_failed")
		return
	}
	domain := r.PathValue("domain")
	registered := false
	for _, candidate := range outlet.Domains {
		if candidate == domain {
			registered = true
			break
		}
	}
	if !registered {
		fail(w, r, 404, "domain_not_found")
		return
	}
	hostnames, err := s.dnsHostnames(r.Context(), outlet, domain)
	if err != nil {
		fail(w, r, 503, "persistence_failed")
		return
	}
	result := domainDNSCheck{CheckedAt: time.Now().UTC(), Records: make([]dnsRecordCheck, 0, len(hostnames))}
	resolver := net.DefaultResolver
	ctx, cancel := context.WithTimeout(r.Context(), 6*time.Second)
	defer cancel()
	for _, hostname := range hostnames {
		check := dnsRecordCheck{Hostname: hostname, Addresses: []string{}}
		addresses, lookupErr := resolver.LookupIPAddr(ctx, hostname)
		if lookupErr != nil {
			check.Error = "dns_lookup_failed"
		} else {
			for _, address := range addresses {
				check.Addresses = append(check.Addresses, address.IP.String())
			}
		}
		result.Records = append(result.Records, check)
	}
	reply(w, http.StatusOK, result)
}

// dnsHostnames lists the names that must resolve to the Outlet for a domain:
// the management host on the primary domain, a fresh probe name in wildcard
// mode or every public address in hosts mode, and the tunnel host when it
// belongs to this domain. The domain root is checked only when it is the
// tunnel host, because it may serve another site.
func (s *Store) dnsHostnames(ctx context.Context, outlet control.Outlet, domain string) ([]string, error) {
	hostnames := []string{}
	add := func(hostname string) {
		for _, existing := range hostnames {
			if existing == hostname {
				return
			}
		}
		hostnames = append(hostnames, hostname)
	}
	if domain == outlet.Domain && outlet.AdminHostname != "" {
		add(outlet.AdminHostname)
	}
	if outlet.Modes[domain] == control.DomainHosts {
		routes, err := s.control.DomainRouteHostnames(ctx, domain)
		if err != nil {
			return nil, err
		}
		for _, hostname := range routes {
			add(hostname)
		}
	} else {
		// Query a fresh subdomain to test whether the wildcard record is active.
		add("check-" + strings.ToLower(strings.NewReplacer("_", "a", "-", "a").Replace(Token()[:12])) + "." + domain)
	}
	tunnel, err := s.tunnel(ctx, outlet.AdminHostname)
	if err != nil {
		return nil, err
	}
	if host, _, splitErr := net.SplitHostPort(tunnel.Address); splitErr == nil && (host == domain || strings.HasSuffix(host, "."+domain)) {
		add(host)
	}
	return hostnames, nil
}

type outletRequest struct {
	Domain string `json:"domain"`
	Mode   string `json:"mode,omitempty"`
}

type domainModeRequest struct {
	Mode string `json:"mode"`
}

func (s *Store) updateOutlet(w http.ResponseWriter, r *http.Request) {
	if !s.admin(w, r) {
		return
	}
	if s.control == nil {
		fail(w, r, 503, "control_database_unavailable")
		return
	}
	var req outletRequest
	if !decode(w, r, &req, "domain") {
		return
	}
	previous, err := s.control.Outlet(r.Context())
	if err != nil {
		fail(w, r, 503, "persistence_failed")
		return
	}
	outlet, err := s.control.SetDomain(r.Context(), req.Domain, req.Mode)
	if errors.Is(err, control.ErrDomainFromEnvironment) {
		fail(w, r, 409, "domain_from_environment")
		return
	}
	if errors.Is(err, control.ErrAdminHostnameTaken) {
		fail(w, r, 409, "admin_hostname_taken")
		return
	}
	if errors.Is(err, control.ErrInvalidDomain) {
		fail(w, r, 400, "invalid_domain")
		return
	}
	if errors.Is(err, control.ErrInvalidDomainMode) {
		fail(w, r, 400, "invalid_domain_mode")
		return
	}
	if errors.Is(err, control.ErrDomainHasRoutes) {
		fail(w, r, 409, "domain_has_routes")
		return
	}
	if errors.Is(err, control.ErrDomainExists) {
		fail(w, r, 409, "domain_exists")
		return
	}
	if err != nil {
		fail(w, r, 503, "persistence_failed")
		return
	}
	// The management host follows the primary domain; so do its saved limits,
	// saved and put in place as a rate limit save is (see Store.saving).
	s.saving.Lock()
	err = s.control.RenameRateLimitHost(r.Context(), previous.AdminHostname, outlet.AdminHostname, s.now())
	if err == nil {
		err = s.reloadRateLimit(r.Context())
	}
	s.saving.Unlock()
	if err != nil {
		fail(w, r, 503, "persistence_failed")
		return
	}
	s.audited(r, "domain.set", "domain", req.Domain, map[string]any{"mode": outlet.Modes[req.Domain]})
	if err := s.reconcileProxy(r.Context()); err != nil {
		fail(w, r, 503, "proxy_reconcile_failed")
		return
	}
	// A default tunnel address follows the primary domain.
	s.mu.Lock()
	tunnel, err := s.tunnel(r.Context(), outlet.AdminHostname)
	if err == nil {
		err = s.retargetTunnels(tunnel.Address)
	}
	s.mu.Unlock()
	if err != nil {
		fail(w, r, 503, "persistence_failed")
		return
	}
	reply(w, 200, outletResponse{Outlet: outlet, Tunnel: tunnel, Version: contract.Version})
}

func (s *Store) addOutletDomain(w http.ResponseWriter, r *http.Request) {
	if !s.admin(w, r) {
		return
	}
	if s.control == nil {
		fail(w, r, 503, "control_database_unavailable")
		return
	}
	var req outletRequest
	if !decode(w, r, &req, "domain") {
		return
	}
	outlet, err := s.control.AddDomain(r.Context(), req.Domain, req.Mode)
	if errors.Is(err, control.ErrDomainFromEnvironment) {
		fail(w, r, 409, "domain_from_environment")
		return
	}
	if errors.Is(err, control.ErrInvalidDomain) {
		fail(w, r, 400, "invalid_domain")
		return
	}
	if errors.Is(err, control.ErrInvalidDomainMode) {
		fail(w, r, 400, "invalid_domain_mode")
		return
	}
	if errors.Is(err, control.ErrDomainExists) {
		fail(w, r, 409, "domain_exists")
		return
	}
	if err != nil {
		fail(w, r, 503, "persistence_failed")
		return
	}
	s.audited(r, "domain.add", "domain", req.Domain, map[string]any{"mode": outlet.Modes[req.Domain]})
	if err := s.reconcileProxy(r.Context()); err != nil {
		fail(w, r, 503, "proxy_reconcile_failed")
		return
	}
	s.replyOutlet(w, r, 201, outlet)
}

// updateOutletDomainMode switches a domain between one wildcard record and
// per-host records. It changes DNS guidance and checks only; routing is the same.
func (s *Store) updateOutletDomainMode(w http.ResponseWriter, r *http.Request) {
	if !s.admin(w, r) {
		return
	}
	if s.control == nil {
		fail(w, r, 503, "control_database_unavailable")
		return
	}
	var req domainModeRequest
	if !decode(w, r, &req, "mode") {
		return
	}
	outlet, err := s.control.SetDomainMode(r.Context(), r.PathValue("domain"), req.Mode)
	if errors.Is(err, control.ErrInvalidDomainMode) {
		fail(w, r, 400, "invalid_domain_mode")
		return
	}
	if errors.Is(err, control.ErrDomainNotFound) {
		fail(w, r, 404, "domain_not_found")
		return
	}
	if err != nil {
		fail(w, r, 503, "persistence_failed")
		return
	}
	s.audited(r, "domain.mode", "domain", r.PathValue("domain"), map[string]any{"mode": req.Mode})
	s.replyOutlet(w, r, 200, outlet)
}

func (s *Store) deleteOutletDomain(w http.ResponseWriter, r *http.Request) {
	if !s.admin(w, r) {
		return
	}
	if s.control == nil {
		fail(w, r, 503, "control_database_unavailable")
		return
	}
	outlet, err := s.control.RemoveDomain(r.Context(), r.PathValue("domain"))
	if errors.Is(err, control.ErrPrimaryDomain) {
		fail(w, r, 409, "primary_domain")
		return
	}
	if errors.Is(err, control.ErrDomainHasRoutes) {
		fail(w, r, 409, "domain_has_routes")
		return
	}
	if errors.Is(err, control.ErrDomainNotFound) {
		fail(w, r, 404, "domain_not_found")
		return
	}
	if err != nil {
		fail(w, r, 503, "persistence_failed")
		return
	}
	s.audited(r, "domain.remove", "domain", r.PathValue("domain"), nil)
	if err := s.reconcileProxy(r.Context()); err != nil {
		fail(w, r, 503, "proxy_reconcile_failed")
		return
	}
	s.replyOutlet(w, r, 200, outlet)
}

type tunnelRequest struct {
	Address string `json:"address"`
}

// normalizeTunnelAddress accepts host or host:port. A bare host uses the
// rathole listen port, which Compose publishes on the same port by default.
func (s *Store) normalizeTunnelAddress(raw string) (string, bool) {
	address := strings.ToLower(strings.TrimSpace(raw))
	if address == "" {
		return "", true
	}
	if _, _, err := net.SplitHostPort(address); err != nil {
		_, port, listenErr := net.SplitHostPort(s.config.TunnelListenAddress)
		if listenErr != nil {
			return "", false
		}
		address = net.JoinHostPort(strings.Trim(address, "[]"), port)
	}
	return address, validation.TunnelAddress(address)
}

// retargeted points every service of config at address; services on one
// Connector must share a tunnel endpoint. It keeps the single-service shape,
// whose heartbeats still name the service.
func retargeted(config contract.Configuration, address string) (contract.Configuration, bool) {
	changed := false
	if len(config.Services) == 0 {
		if config.ServiceID != "" && config.Tunnel.RemoteAddress != address {
			config.Tunnel.RemoteAddress, changed = address, true
		}
		return config, changed
	}
	config.Services = slices.Clone(config.Services)
	for i := range config.Services {
		if config.Services[i].Tunnel.RemoteAddress != address {
			config.Services[i].Tunnel.RemoteAddress, changed = address, true
		}
	}
	return config, changed
}

// retargetTunnels points every managed Connector at address in one write.
// The new revision makes each Connector fetch its configuration on the next
// heartbeat. The caller holds s.mu.
func (s *Store) retargetTunnels(address string) error {
	if address == "" {
		return nil
	}
	next, changed := make(map[string]Record, len(s.managed)), false
	for id, record := range s.managed {
		if config, ok := retargeted(record.Configuration, address); ok {
			config.Revision++
			record.Configuration, changed = config, true
		}
		next[id] = record
	}
	if !changed {
		return nil
	}
	if err := securefs.WriteJSON(s.managedPath, next); err != nil {
		return err
	}
	s.managed, s.managedDirty = next, false
	return nil
}

func (s *Store) updateTunnel(w http.ResponseWriter, r *http.Request) {
	if !s.admin(w, r) {
		return
	}
	if s.control == nil {
		fail(w, r, 503, "control_database_unavailable")
		return
	}
	var req tunnelRequest
	if !decode(w, r, &req, "address") {
		return
	}
	address, ok := s.normalizeTunnelAddress(req.Address)
	if !ok {
		fail(w, r, 400, "invalid_tunnel_address")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	outlet, err := s.control.Outlet(r.Context())
	var current outletTunnel
	if err == nil {
		current, err = s.tunnel(r.Context(), outlet.AdminHostname)
	}
	if err != nil {
		fail(w, r, 503, "persistence_failed")
		return
	}
	// Without a default, clearing would leave Connectors on the old address.
	if address == "" && current.DefaultAddress == "" {
		fail(w, r, 409, "tunnel_default_unavailable")
		return
	}
	if err := s.control.SetTunnelAddress(r.Context(), address); err != nil {
		if errors.Is(err, control.ErrInvalidTunnelAddress) {
			fail(w, r, 400, "invalid_tunnel_address")
		} else {
			fail(w, r, 503, "persistence_failed")
		}
		return
	}
	tunnel, err := s.tunnel(r.Context(), outlet.AdminHostname)
	if err == nil {
		err = s.retargetTunnels(tunnel.Address)
	}
	if err != nil {
		// Keep the stored setting in line with what Connectors dial.
		previous := ""
		if current.Custom {
			previous = current.Address
		}
		_ = s.control.SetTunnelAddress(context.WithoutCancel(r.Context()), previous)
		fail(w, r, 503, "persistence_failed")
		return
	}
	s.audited(r, "tunnel.update", "outlet", "tunnel", map[string]any{"address": address})
	reply(w, 200, outletResponse{Outlet: outlet, Tunnel: tunnel, Version: contract.Version})
}

type tunnelDial struct {
	Address   string `json:"address"`
	Reachable bool   `json:"reachable"`
	LatencyMS int64  `json:"latency_ms"`
	Error     string `json:"error,omitempty"`
}

type tunnelCheck struct {
	CheckedAt time.Time    `json:"checked_at"`
	Address   string       `json:"address"`
	Reachable bool         `json:"reachable"`
	Error     string       `json:"error,omitempty"`
	Results   []tunnelDial `json:"results"`
}

func (s *Store) checkTunnel(w http.ResponseWriter, r *http.Request) {
	if !s.admin(w, r) {
		return
	}
	if s.control == nil {
		fail(w, r, 503, "control_database_unavailable")
		return
	}
	var req tunnelRequest
	if !decode(w, r, &req) {
		return
	}
	address, ok := s.normalizeTunnelAddress(req.Address)
	if !ok {
		fail(w, r, 400, "invalid_tunnel_address")
		return
	}
	if address == "" {
		outlet, err := s.control.Outlet(r.Context())
		var tunnel outletTunnel
		if err == nil {
			tunnel, err = s.tunnel(r.Context(), outlet.AdminHostname)
		}
		if err != nil {
			fail(w, r, 503, "persistence_failed")
			return
		}
		address = tunnel.Address
	}
	if address == "" {
		fail(w, r, 409, "tunnel_address_unset")
		return
	}
	reply(w, 200, s.probeTunnel(r.Context(), address))
}

// probeTunnel resolves the tunnel host and opens a TCP connection to each
// address. It runs on the Outlet, so it shows DNS and the published port, but
// not the Connector network's egress rules or that the listener is this Outlet.
func (s *Store) probeTunnel(ctx context.Context, address string) tunnelCheck {
	lookupIP, dialTCP := s.lookupIP, s.dialTCP
	if lookupIP == nil {
		lookupIP = net.DefaultResolver.LookupIPAddr
	}
	if dialTCP == nil {
		var dialer net.Dialer
		dialTCP = func(ctx context.Context, address string) (net.Conn, error) {
			return dialer.DialContext(ctx, "tcp", address)
		}
	}
	check := tunnelCheck{CheckedAt: time.Now().UTC(), Address: address, Results: []tunnelDial{}}
	host, port, _ := net.SplitHostPort(address)
	ips := []string{host}
	if net.ParseIP(host) == nil {
		lookup, cancel := context.WithTimeout(ctx, 3*time.Second)
		resolved, err := lookupIP(lookup, host)
		cancel()
		if err != nil || len(resolved) == 0 {
			check.Error = "dns_lookup_failed"
			return check
		}
		ips = ips[:0]
		for _, ip := range resolved[:min(len(resolved), 8)] {
			ips = append(ips, ip.IP.String())
		}
	}
	check.Results = make([]tunnelDial, len(ips))
	var wg sync.WaitGroup
	for i, ip := range ips {
		// Connectors reach the tunnel from outside, so these never answer for
		// it; dialing them would only probe the Outlet's own surroundings.
		if parsed := net.ParseIP(ip); parsed == nil || parsed.IsLoopback() || parsed.IsUnspecified() || parsed.IsLinkLocalUnicast() || parsed.IsLinkLocalMulticast() || parsed.IsMulticast() {
			check.Results[i] = tunnelDial{Address: net.JoinHostPort(ip, port), Error: "address_not_allowed"}
			continue
		}
		wg.Go(func() {
			result := tunnelDial{Address: net.JoinHostPort(ip, port)}
			dial, cancel := context.WithTimeout(ctx, 4*time.Second)
			defer cancel()
			started := time.Now()
			conn, err := dialTCP(dial, result.Address)
			if err != nil {
				result.Error = dialError(err)
			} else {
				conn.Close()
				result.Reachable, result.LatencyMS = true, time.Since(started).Milliseconds()
			}
			check.Results[i] = result
		})
	}
	wg.Wait()
	for _, result := range check.Results {
		check.Reachable = check.Reachable || result.Reachable
	}
	return check
}

func dialError(err error) string {
	var netErr net.Error
	switch {
	case errors.Is(err, syscall.ECONNREFUSED):
		return "connection_refused"
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &netErr) && netErr.Timeout():
		return "timeout"
	case errors.Is(err, syscall.EHOSTUNREACH), errors.Is(err, syscall.ENETUNREACH):
		return "unreachable"
	default:
		return "connection_failed"
	}
}

type Status struct {
	ID                  string              `json:"id"`
	Name                string              `json:"name"`
	ServiceID           string              `json:"service_id"`
	Status              string              `json:"status"`
	TunnelStatus        string              `json:"tunnel_status"`
	LastSeen            time.Time           `json:"last_seen"`
	Heartbeat           *contract.Heartbeat `json:"heartbeat"`
	CredentialExpiresAt time.Time           `json:"credential_expires_at,omitzero"`
}

func (s *Store) list(w http.ResponseWriter, r *http.Request) {
	if !s.admin(w, r) {
		return
	}
	if s.control != nil {
		connectors, err := s.control.ListConnectors(r.Context())
		if err != nil {
			fail(w, r, 503, "persistence_failed")
			return
		}
		s.mu.Lock()
		for i := range connectors {
			if record, ok := s.managed[connectors[i].ID]; ok {
				connectors[i].Status = managedConnectorStatus(record, s.now())
				if !record.LastSeen.IsZero() {
					connectors[i].LastSeen = record.LastSeen.UTC().Format(time.RFC3339)
				} else if record.LastHeartbeat != nil {
					connectors[i].LastSeen = record.LastHeartbeat.ObservedAt.UTC().Format(time.RFC3339)
				}
			}
		}
		s.mu.Unlock()
		reply(w, 200, map[string]any{"connectors": connectors})
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

func managedConnectorStatus(record Record, now time.Time) string {
	if !record.Enrolled {
		return "draft"
	}
	if record.LastSeen.IsZero() || now.Sub(record.LastSeen) >= 60*time.Second {
		return "disconnected"
	}
	if now.Sub(record.LastSeen) >= 15*time.Second {
		return "stale"
	}
	// Tunnel connectivity is the Connector's recent authenticated response.
	// Service health and tunnel forwarding are reported per service below.
	return "connected"
}

func managedServiceStatus(record Record, serviceID string, now time.Time) string {
	if !record.Enrolled || record.probeFor(serviceID) == "" || record.LastHeartbeat == nil {
		return "unknown"
	}
	if _, observed := record.ServiceStatuses[serviceID]; !observed {
		return "unknown"
	}
	status := managedConnectorStatus(record, now)
	if status == "disconnected" || status == "stale" {
		return status
	}
	health := record.LastHeartbeat.ServiceHealth
	if len(record.Configuration.Services) != 0 {
		health = record.LastHeartbeat.ServiceHealths[serviceID]
	}
	if health == "unhealthy" {
		return "error"
	}
	if health == "healthy" && record.ServiceStatuses[serviceID] == "connected" && status == "connected" {
		return "connected"
	}
	return "connecting"
}

func (s *Store) issueToken(w http.ResponseWriter, r *http.Request) {
	if !s.admin(w, r) {
		return
	}
	if s.control != nil {
		s.issueManagedToken(w, r)
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
	// Active identities require an explicit reset before their run token changes.
	if record.Enrolled && (record.RunTokenHash != "" || s.now().Before(record.CredentialExpiresAt)) && r.URL.Query().Get("reset") != "true" {
		fail(w, r, 409, "already_enrolled")
		return
	}
	token := Token()
	record.Enrolled = false
	record.PublicKey, record.CredentialHash = "", ""
	record.RunTokenHash = Hash(token)
	record.CredentialExpiresAt = time.Time{}
	record.LastHeartbeat, record.LastSeen, record.TunnelStatus = nil, time.Time{}, "unknown"
	record.EnrollmentHash, record.EnrollmentExpiresAt = "", time.Time{}
	if err := s.save(id, record); err != nil {
		fail(w, r, 503, "persistence_failed")
		return
	}
	reply(w, 201, map[string]any{"token": token})
}

func (s *Store) issueManagedToken(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	services, err := s.control.ListServices(r.Context(), id)
	if errors.Is(err, control.ErrConnectorNotFound) {
		fail(w, r, 404, "connector_not_found")
		return
	}
	if err != nil {
		fail(w, r, 503, "persistence_failed")
		return
	}
	connectors, err := s.control.ListConnectors(r.Context())
	if err != nil {
		fail(w, r, 503, "persistence_failed")
		return
	}
	name := ""
	for _, connector := range connectors {
		if connector.ID == id {
			name = connector.Name
			break
		}
	}
	if name == "" {
		fail(w, r, 404, "connector_not_found")
		return
	}
	s.mu.Lock()
	// Resolve under the lock so a concurrent tunnel address change is not lost.
	outlet, err := s.control.Outlet(r.Context())
	var tunnel outletTunnel
	if err == nil {
		tunnel, err = s.tunnel(r.Context(), outlet.AdminHostname)
	}
	if err != nil {
		s.mu.Unlock()
		fail(w, r, 503, "persistence_failed")
		return
	}
	remoteAddress := tunnel.Address
	if remoteAddress == "" || !validation.PublicKey(s.config.TunnelServerPublicKey) || validation.Endpoint(remoteAddress) != nil {
		s.mu.Unlock()
		fail(w, r, 503, "tunnel_runtime_unconfigured")
		return
	}
	record, exists := s.managed[id]
	if exists && record.Enrolled && (record.RunTokenHash != "" || s.now().Before(record.CredentialExpiresAt)) && r.URL.Query().Get("reset") != "true" {
		s.mu.Unlock()
		fail(w, r, 409, "already_enrolled")
		return
	}
	if !exists {
		port := s.nextProbePort()
		record = Record{Seed: Seed{Name: name, Configuration: contract.Configuration{ConnectorID: id}, ProbeAddress: net.JoinHostPort("127.0.0.1", strconv.Itoa(port))}, TunnelStatus: "unknown"}
	}
	previous := record.Configuration.ActiveServices()
	record.Configuration = contract.Configuration{ConnectorID: id, Revision: record.Configuration.Revision + 1}
	record.ProbeAddresses = maps.Clone(record.ProbeAddresses)
	if record.ProbeAddresses == nil {
		record.ProbeAddresses = map[string]string{}
	}
	nextPort := s.nextProbePort()
	for _, service := range services {
		if record.ProbeAddresses[service.ID] == "" {
			if len(previous) == 1 && previous[0].ServiceID == service.ID && record.ProbeAddress != "" {
				record.ProbeAddresses[service.ID] = record.ProbeAddress
			} else {
				for s.probePortUsed(nextPort, record.ProbeAddresses) {
					nextPort++
				}
				record.ProbeAddresses[service.ID] = net.JoinHostPort("127.0.0.1", strconv.Itoa(nextPort))
				nextPort++
			}
		}
		record.Configuration.Services = append(record.Configuration.Services, contract.ServiceConfiguration{ServiceID: service.ID, Target: service.Target, Tunnel: contract.Tunnel{RemoteAddress: remoteAddress, ServerPublicKey: s.config.TunnelServerPublicKey, ServiceName: service.ID, ServiceToken: Token()}})
	}
	token := Token()
	record.Enrolled = false
	record.PublicKey, record.CredentialHash = "", ""
	record.RunTokenHash = Hash(token)
	record.CredentialExpiresAt = time.Time{}
	record.LastHeartbeat, record.LastSeen, record.TunnelStatus, record.ServiceStatuses = nil, time.Time{}, "unknown", nil
	record.EnrollmentHash, record.EnrollmentExpiresAt = "", time.Time{}
	if err := s.saveManaged(id, record); err != nil {
		s.mu.Unlock()
		fail(w, r, 503, "persistence_failed")
		return
	}
	s.mu.Unlock()
	action := "connector.token_issue"
	if r.URL.Query().Get("reset") == "true" {
		action = "connector.token_reset"
	}
	s.audited(r, action, "connector", id, map[string]any{"name": name})
	if err := s.reconcileTunnel(); err != nil {
		fail(w, r, 503, "tunnel_reconcile_failed")
		return
	}
	reply(w, 201, map[string]any{"token": token, "services": services})
}

func (s *Store) nextProbePort() int {
	used := map[int]bool{}
	for _, set := range []map[string]Record{s.records, s.managed} {
		for _, record := range set {
			_, port, err := net.SplitHostPort(record.ProbeAddress)
			if value, err := strconv.Atoi(port); err == nil {
				used[value] = true
			}
			_ = err
			for _, address := range record.ProbeAddresses {
				_, value, splitErr := net.SplitHostPort(address)
				if splitErr == nil {
					if port, parseErr := strconv.Atoi(value); parseErr == nil {
						used[port] = true
					}
				}
			}
		}
	}
	for port := 24000; port < 30000; port++ {
		if !used[port] {
			return port
		}
	}
	return 0
}

func (s *Store) probePortUsed(port int, extra map[string]string) bool {
	for _, address := range extra {
		_, value, err := net.SplitHostPort(address)
		if err == nil && value == strconv.Itoa(port) {
			return true
		}
	}
	for _, set := range []map[string]Record{s.records, s.managed} {
		for _, record := range set {
			if _, value, err := net.SplitHostPort(record.ProbeAddress); err == nil && value == strconv.Itoa(port) {
				return true
			}
			for _, address := range record.ProbeAddresses {
				_, value, err := net.SplitHostPort(address)
				if err == nil && value == strconv.Itoa(port) {
					return true
				}
			}
		}
	}
	return false
}

type connectorRequest struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// connectorUpdateRequest changes dashboard-only fields; omitted fields stay.
type connectorUpdateRequest struct {
	Description *string `json:"description,omitempty"`
	DisplayName *string `json:"display_name,omitempty"`
}

// updateConnector changes the administrator's note on a Connector. The note
// is never sent to the Connector itself.
func (s *Store) updateConnector(w http.ResponseWriter, r *http.Request) {
	if !s.admin(w, r) {
		return
	}
	if s.control == nil {
		fail(w, r, 503, "control_database_unavailable")
		return
	}
	var req connectorUpdateRequest
	if !decode(w, r, &req) {
		return
	}
	if req.Description == nil && req.DisplayName == nil {
		fail(w, r, 400, "invalid_request")
		return
	}
	id := r.PathValue("id")
	result := map[string]string{"id": id}
	if req.DisplayName != nil {
		name, err := s.control.SetConnectorDisplayName(r.Context(), id, *req.DisplayName)
		if !s.connectorUpdateOK(w, r, err) {
			return
		}
		s.audited(r, "connector.rename", "connector", id, map[string]any{"display_name": name})
		result["display_name"] = name
	}
	if req.Description != nil {
		description, err := s.control.SetConnectorDescription(r.Context(), id, *req.Description)
		if !s.connectorUpdateOK(w, r, err) {
			return
		}
		s.audited(r, "connector.describe", "connector", id, nil)
		result["description"] = description
	}
	reply(w, 200, result)
}

func (s *Store) connectorUpdateOK(w http.ResponseWriter, r *http.Request, err error) bool {
	switch {
	case errors.Is(err, control.ErrInvalidDescription):
		fail(w, r, 400, "invalid_description")
	case errors.Is(err, control.ErrInvalidDisplayName):
		fail(w, r, 400, "invalid_display_name")
	case errors.Is(err, control.ErrConnectorNotFound):
		fail(w, r, 404, "connector_not_found")
	case err != nil:
		fail(w, r, 503, "persistence_failed")
	default:
		return true
	}
	return false
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
	connector, err := s.control.CreateDescribedConnector(r.Context(), req.Name, req.Description)
	if errors.Is(err, control.ErrInvalidDescription) {
		fail(w, r, 400, "invalid_description")
		return
	}
	if errors.Is(err, control.ErrConnectorExists) {
		fail(w, r, 409, "connector_name_taken")
		return
	}
	if err != nil {
		fail(w, r, 400, "invalid_connector")
		return
	}
	s.audited(r, "connector.create", "connector", connector.ID, map[string]any{"name": connector.Name})
	reply(w, 201, connector)
}

func (s *Store) deleteConnector(w http.ResponseWriter, r *http.Request) {
	if !s.admin(w, r) {
		return
	}
	if s.control == nil {
		fail(w, r, 503, "control_database_unavailable")
		return
	}
	id := r.PathValue("id")
	exists, err := s.control.ConnectorExists(r.Context(), id)
	if err != nil {
		fail(w, r, 503, "persistence_failed")
		return
	}
	if !exists {
		fail(w, r, 404, "connector_not_found")
		return
	}
	name := s.connectorName(r.Context(), id)
	if err := s.removeManaged(id); err != nil {
		fail(w, r, 503, "persistence_failed")
		return
	}
	if err := s.reconcileTunnel(); err != nil {
		fail(w, r, 503, "tunnel_reconcile_failed")
		return
	}
	if err := s.reconcileProxy(r.Context()); err != nil {
		fail(w, r, 503, "proxy_reconcile_failed")
		return
	}
	if err := s.control.DeleteConnector(r.Context(), id); err != nil {
		if errors.Is(err, control.ErrConnectorNotFound) {
			fail(w, r, 404, "connector_not_found")
		} else {
			fail(w, r, 503, "persistence_failed")
		}
		return
	}
	s.audited(r, "connector.delete", "connector", id, map[string]any{"name": name})
	if err := s.reconcileProxy(r.Context()); err != nil {
		fail(w, r, 503, "proxy_reconcile_failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type serviceRequest struct {
	Name     string          `json:"name"`
	Protocol string          `json:"protocol"`
	Target   contract.Target `json:"target"`
}

func (s *Store) listServices(w http.ResponseWriter, r *http.Request) {
	if !s.admin(w, r) {
		return
	}
	if s.control == nil {
		fail(w, r, 503, "control_database_unavailable")
		return
	}
	services, err := s.control.ListServices(r.Context(), r.PathValue("id"))
	if errors.Is(err, control.ErrConnectorNotFound) {
		fail(w, r, 404, "connector_not_found")
		return
	}
	if err != nil {
		fail(w, r, 503, "persistence_failed")
		return
	}
	s.mu.Lock()
	if record, ok := s.managed[r.PathValue("id")]; ok {
		for i := range services {
			services[i].ObservedStatus = managedServiceStatus(record, services[i].ID, s.now())
		}
	}
	s.mu.Unlock()
	reply(w, 200, map[string]any{"services": services})
}

func (s *Store) createService(w http.ResponseWriter, r *http.Request) {
	if !s.admin(w, r) {
		return
	}
	if s.control == nil {
		fail(w, r, 503, "control_database_unavailable")
		return
	}
	var req serviceRequest
	if !decode(w, r, &req, "name", "protocol", "target") {
		return
	}
	service, err := s.control.CreateService(r.Context(), r.PathValue("id"), req.Name, req.Protocol, req.Target)
	if errors.Is(err, control.ErrConnectorNotFound) {
		fail(w, r, 404, "connector_not_found")
		return
	}
	if errors.Is(err, control.ErrServiceExists) {
		fail(w, r, 409, "service_name_taken")
		return
	}
	if errors.Is(err, control.ErrServiceTargetExists) {
		fail(w, r, 409, "service_target_taken")
		return
	}
	if errors.Is(err, control.ErrInvalidService) {
		fail(w, r, 400, "invalid_service")
		return
	}
	if err != nil {
		fail(w, r, 503, "persistence_failed")
		return
	}
	s.audited(r, "service.create", "service", service.ID, map[string]any{"connector_id": r.PathValue("id"), "name": req.Name, "protocol": req.Protocol, "target": req.Target})
	s.mu.Lock()
	if record, ok := s.managed[r.PathValue("id")]; ok {
		prior := record.Configuration.ActiveServices()
		remoteAddress := ""
		if outlet, outletErr := s.control.Outlet(r.Context()); outletErr == nil {
			if tunnel, tunnelErr := s.tunnel(r.Context(), outlet.AdminHostname); tunnelErr == nil {
				remoteAddress = tunnel.Address
			}
		}
		if remoteAddress == "" && len(prior) != 0 {
			remoteAddress = prior[0].Tunnel.RemoteAddress
		}
		config := contract.Configuration{ConnectorID: r.PathValue("id"), Revision: record.Configuration.Revision + 1, Services: append([]contract.ServiceConfiguration(nil), prior...)}
		config.Services = append(config.Services, contract.ServiceConfiguration{ServiceID: service.ID, Target: service.Target, Tunnel: contract.Tunnel{RemoteAddress: remoteAddress, ServerPublicKey: s.config.TunnelServerPublicKey, ServiceName: service.ID, ServiceToken: Token()}})
		config, _ = retargeted(config, remoteAddress)
		// An invalid configuration would stop the Connector and the next Outlet start.
		if validation.Configuration(config) != nil {
			s.mu.Unlock()
			fail(w, r, 503, "tunnel_runtime_unconfigured")
			return
		}
		record.Configuration = config
		record.ProbeAddresses = maps.Clone(record.ProbeAddresses)
		if record.ProbeAddresses == nil {
			record.ProbeAddresses = map[string]string{}
		}
		for _, old := range prior {
			if record.ProbeAddresses[old.ServiceID] == "" {
				record.ProbeAddresses[old.ServiceID] = record.ProbeAddress
			}
		}
		record.ProbeAddresses[service.ID] = net.JoinHostPort("127.0.0.1", strconv.Itoa(s.nextProbePort()))
		// The new service has no observation yet. Keep the existing services'
		// heartbeat and probe status so their public routes stay available.
		if err := s.saveManaged(r.PathValue("id"), record); err != nil {
			s.mu.Unlock()
			fail(w, r, 503, "persistence_failed")
			return
		}
	}
	s.mu.Unlock()
	if err := s.reconcileTunnel(); err != nil {
		fail(w, r, 503, "tunnel_reconcile_failed")
		return
	}
	if err := s.reconcileProxy(r.Context()); err != nil {
		fail(w, r, 503, "proxy_reconcile_failed")
		return
	}
	reply(w, 201, service)
}

type serviceTargetRequest struct {
	Target contract.Target `json:"target"`
}

func (s *Store) updateServiceTarget(w http.ResponseWriter, r *http.Request) {
	if !s.admin(w, r) {
		return
	}
	if s.control == nil {
		fail(w, r, 503, "control_database_unavailable")
		return
	}
	var req serviceTargetRequest
	if !decode(w, r, &req, "target") {
		return
	}
	id, serviceID := r.PathValue("id"), r.PathValue("service_id")
	s.mu.Lock()
	record, ok := s.managed[id]
	index := -1
	if ok {
		for i, item := range record.Configuration.ActiveServices() {
			if item.ServiceID == serviceID {
				index = i
				break
			}
		}
	}
	if index < 0 {
		s.mu.Unlock()
		fail(w, r, 404, "service_not_found")
		return
	}
	services := append([]contract.ServiceConfiguration(nil), record.Configuration.ActiveServices()...)
	changed := services[index].Target != req.Target
	service, err := s.control.UpdateServiceTarget(r.Context(), id, serviceID, req.Target)
	if err != nil {
		s.mu.Unlock()
		switch {
		case errors.Is(err, control.ErrInvalidService):
			fail(w, r, 400, "invalid_service")
		case errors.Is(err, control.ErrServiceNotFound):
			fail(w, r, 404, "service_not_found")
		case errors.Is(err, control.ErrServiceTargetExists):
			fail(w, r, 409, "service_target_taken")
		default:
			fail(w, r, 503, "persistence_failed")
		}
		return
	}
	s.audit(r, "service.update", control.AuditEntry{ResourceType: "service", ResourceID: serviceID, Detail: map[string]any{"connector_id": id, "target": req.Target}})
	if changed {
		services[index].Target = req.Target
		record.Configuration.ServiceID, record.Configuration.Target, record.Configuration.Tunnel = "", contract.Target{}, contract.Tunnel{}
		record.Configuration.Services = services
		record.Configuration.Revision++
		record.ServiceStatuses = maps.Clone(record.ServiceStatuses)
		if record.ServiceStatuses == nil {
			record.ServiceStatuses = map[string]string{}
		}
		record.ServiceStatuses[serviceID] = "disconnected"
		if err := s.saveManaged(id, record); err != nil {
			s.mu.Unlock()
			fail(w, r, 503, "persistence_failed")
			return
		}
	}
	s.mu.Unlock()
	if changed {
		if err := s.reconcileProxy(r.Context()); err != nil {
			fail(w, r, 503, "proxy_reconcile_failed")
			return
		}
	}
	reply(w, 200, service)
}

func (s *Store) deleteService(w http.ResponseWriter, r *http.Request) {
	if !s.admin(w, r) {
		return
	}
	if s.control == nil {
		fail(w, r, 503, "control_database_unavailable")
		return
	}
	err := s.control.DeleteService(r.Context(), r.PathValue("id"), r.PathValue("service_id"))
	switch {
	case errors.Is(err, control.ErrServiceNotFound):
		fail(w, r, 404, "service_not_found")
	case errors.Is(err, control.ErrServiceHasRoutes):
		fail(w, r, 409, "service_has_routes")
	case err != nil:
		fail(w, r, 503, "persistence_failed")
	default:
		s.audited(r, "service.delete", "service", r.PathValue("service_id"), map[string]any{"connector_id": r.PathValue("id")})
		s.mu.Lock()
		if record, ok := s.managed[r.PathValue("id")]; ok {
			remaining := []contract.ServiceConfiguration{}
			for _, item := range record.Configuration.ActiveServices() {
				if item.ServiceID != r.PathValue("service_id") {
					remaining = append(remaining, item)
				}
			}
			record.Configuration = contract.Configuration{ConnectorID: r.PathValue("id"), Revision: record.Configuration.Revision + 1, Services: remaining}
			record.ProbeAddresses = maps.Clone(record.ProbeAddresses)
			delete(record.ProbeAddresses, r.PathValue("service_id"))
			record.ServiceStatuses = maps.Clone(record.ServiceStatuses)
			delete(record.ServiceStatuses, r.PathValue("service_id"))
			if err := s.saveManaged(r.PathValue("id"), record); err != nil {
				s.mu.Unlock()
				fail(w, r, 503, "persistence_failed")
				return
			}
		}
		s.mu.Unlock()
		if err := s.reconcileTunnel(); err != nil {
			fail(w, r, 503, "tunnel_reconcile_failed")
			return
		}
		if err := s.reconcileProxy(r.Context()); err != nil {
			fail(w, r, 503, "proxy_reconcile_failed")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

type routeRequest struct {
	Hostname string `json:"hostname"`
	// Shared joins an address another service already uses.
	Shared bool `json:"shared"`
}

func (s *Store) listRoutes(w http.ResponseWriter, r *http.Request) {
	if !s.admin(w, r) {
		return
	}
	if s.control == nil {
		fail(w, r, 503, "control_database_unavailable")
		return
	}
	routes, err := s.control.ListRoutes(r.Context(), r.PathValue("id"))
	if errors.Is(err, control.ErrServiceNotFound) {
		fail(w, r, 404, "service_not_found")
		return
	}
	if err != nil {
		fail(w, r, 503, "persistence_failed")
		return
	}
	reply(w, 200, map[string]any{"routes": routes})
}

func (s *Store) createHTTPRoute(w http.ResponseWriter, r *http.Request) {
	if !s.admin(w, r) {
		return
	}
	if s.control == nil {
		fail(w, r, 503, "control_database_unavailable")
		return
	}
	var req routeRequest
	if !decode(w, r, &req, "hostname") {
		return
	}
	route, err := s.control.CreateHTTPRoute(r.Context(), r.PathValue("id"), req.Hostname, req.Shared)
	if errors.Is(err, control.ErrServiceNotFound) {
		fail(w, r, 404, "service_not_found")
		return
	}
	if errors.Is(err, control.ErrRouteExists) {
		fail(w, r, 409, "hostname_taken")
		return
	}
	if errors.Is(err, control.ErrInvalidRoute) {
		fail(w, r, 400, "invalid_route")
		return
	}
	if err != nil {
		fail(w, r, 503, "persistence_failed")
		return
	}
	detail := map[string]any{"service_id": r.PathValue("id"), "hostname": route.Hostname}
	if route.AddressBackends > 1 {
		detail["shared"] = true
	}
	s.audited(r, "route.create", "route", route.ID, detail)
	if err := s.reconcileProxy(r.Context()); err != nil {
		fail(w, r, 503, "proxy_reconcile_failed")
		return
	}
	reply(w, 201, route)
}

type addressRequest struct {
	Mode   string   `json:"mode"`
	Sticky bool     `json:"sticky"`
	Order  []string `json:"order"`
	// Current is the policy and order the caller read; when given, a save
	// over a change made since is refused.
	Current *control.AddressState `json:"current"`
}

// updateAddress sets how requests for a public address are spread over the
// services sharing it, and their failover order.
func (s *Store) updateAddress(w http.ResponseWriter, r *http.Request) {
	if !s.admin(w, r) {
		return
	}
	if s.control == nil {
		fail(w, r, 503, "control_database_unavailable")
		return
	}
	var req addressRequest
	if !decode(w, r, &req, "mode", "order") {
		return
	}
	hostname := r.PathValue("hostname")
	err := s.control.SetAddressPolicy(r.Context(), hostname, control.AddressPolicy{Mode: req.Mode, Sticky: req.Sticky}, req.Order, req.Current)
	if errors.Is(err, control.ErrAddressChanged) {
		fail(w, r, 409, "address_changed")
		return
	}
	if errors.Is(err, control.ErrAddressNotFound) {
		fail(w, r, 404, "address_not_found")
		return
	}
	if errors.Is(err, control.ErrInvalidAddress) {
		fail(w, r, 400, "invalid_address_policy")
		return
	}
	if err != nil {
		fail(w, r, 503, "persistence_failed")
		return
	}
	s.audited(r, "address.update", "address", hostname, map[string]any{"mode": req.Mode, "sticky": req.Mode == control.AddressBalance && req.Sticky, "order": req.Order})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Store) deleteHTTPRoute(w http.ResponseWriter, r *http.Request) {
	if !s.admin(w, r) {
		return
	}
	if s.control == nil {
		fail(w, r, 503, "control_database_unavailable")
		return
	}
	hostname := s.routeHostname(r.Context(), r.PathValue("id"), r.PathValue("route_id"))
	err := s.control.DeleteHTTPRoute(r.Context(), r.PathValue("id"), r.PathValue("route_id"))
	if errors.Is(err, control.ErrRouteNotFound) {
		fail(w, r, 404, "route_not_found")
		return
	}
	if err != nil {
		fail(w, r, 503, "persistence_failed")
		return
	}
	s.audited(r, "route.delete", "route", r.PathValue("route_id"), map[string]any{"service_id": r.PathValue("id"), "hostname": hostname})
	if err := s.reconcileProxy(r.Context()); err != nil {
		fail(w, r, 503, "proxy_reconcile_failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
