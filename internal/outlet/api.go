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
	"strconv"
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
	mux.HandleFunc("GET /api/v1/outlet", s.getOutlet)
	mux.HandleFunc("POST /api/v1/outlet/domains/{domain}/check-dns", s.checkOutletDomainDNS)
	mux.HandleFunc("PUT /api/v1/outlet", s.updateOutlet)
	mux.HandleFunc("POST /api/v1/outlet/domains", s.addOutletDomain)
	mux.HandleFunc("DELETE /api/v1/outlet/domains/{domain}", s.deleteOutletDomain)
	mux.HandleFunc("GET /api/v1/diagnostics/trace", s.trace)
	mux.HandleFunc("GET /api/v1/connectors", s.list)
	mux.HandleFunc("GET /api/v1/connectors/{id}/usage", s.connectorUsage)
	mux.HandleFunc("POST /api/v1/connectors", s.createConnector)
	mux.HandleFunc("DELETE /api/v1/connectors/{id}", s.deleteConnector)
	mux.HandleFunc("GET /api/v1/connectors/{id}/services", s.listServices)
	mux.HandleFunc("POST /api/v1/connectors/{id}/services", s.createService)
	mux.HandleFunc("PATCH /api/v1/connectors/{id}/services/{service_id}", s.updateServiceTarget)
	mux.HandleFunc("DELETE /api/v1/connectors/{id}/services/{service_id}", s.deleteService)
	mux.HandleFunc("GET /api/v1/services/{id}/routes", s.listRoutes)
	mux.HandleFunc("POST /api/v1/services/{id}/routes", s.createHTTPRoute)
	mux.HandleFunc("DELETE /api/v1/services/{id}/routes/{route_id}", s.deleteHTTPRoute)
	mux.HandleFunc("POST /api/v1/connectors/{id}/enrollment-tokens", s.issueToken)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Request-ID", Token())
		mux.ServeHTTP(w, r)
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
	for id, record := range s.managed {
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
		record.EnrollmentHash = ""
		if err := s.saveManaged(id, record); err != nil {
			fail(w, r, 503, "persistence_failed")
			return
		}
		reply(w, 201, contract.EnrollmentResponse{Configuration: record.Configuration, Credential: credential, CredentialExpiresAt: record.CredentialExpiresAt})
		return
	}
	fail(w, r, 401, "invalid_enrollment_token")
}

// Called only while the store lock is held.
func (s *Store) authenticated(r *http.Request) (string, Record, bool, bool) {
	for id, record := range s.records {
		if record.Enrolled && s.now().Before(record.CredentialExpiresAt) && matches(bearer(r), record.CredentialHash) {
			return id, record, false, true
		}
	}
	for id, record := range s.managed {
		if record.Enrolled && s.now().Before(record.CredentialExpiresAt) && matches(bearer(r), record.CredentialHash) {
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
		if err := s.saveManaged(id, record); err != nil {
			fail(w, r, 503, "persistence_failed")
			return
		}
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
	s.mu.Unlock()
	// An open server listening port alone is not proof of an active tunnel.
	// An application response through that port confirms the HTTP forwarding path.
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
	var saveErr error
	if managed {
		saveErr = s.saveManaged(id, record)
	} else {
		saveErr = s.save(id, record)
	}
	if saveErr != nil {
		s.mu.Unlock()
		fail(w, r, 503, "persistence_failed")
		return
	}
	s.mu.Unlock()
	if managed {
		if err := s.reconcileProxy(r.Context()); err != nil {
			fail(w, r, 503, "proxy_reconcile_failed")
			return
		}
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
	reply(w, 200, outlet)
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
	// Query a fresh subdomain to test whether the wildcard record is active.
	probeLabel := strings.ToLower(strings.NewReplacer("_", "a").Replace(Token()[:12]))
	hostnames := []string{domain, "check-" + probeLabel + "." + domain}
	if domain == outlet.Domain {
		hostnames = append(hostnames, contract.AdminHostname(domain))
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

type outletRequest struct {
	Domain string `json:"domain"`
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
	outlet, err := s.control.SetDomain(r.Context(), req.Domain)
	if errors.Is(err, control.ErrInvalidDomain) {
		fail(w, r, 400, "invalid_domain")
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
	if err := s.reconcileProxy(r.Context()); err != nil {
		fail(w, r, 503, "proxy_reconcile_failed")
		return
	}
	reply(w, 200, outlet)
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
	outlet, err := s.control.AddDomain(r.Context(), req.Domain)
	if errors.Is(err, control.ErrInvalidDomain) {
		fail(w, r, 400, "invalid_domain")
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
	if err := s.reconcileProxy(r.Context()); err != nil {
		fail(w, r, 503, "proxy_reconcile_failed")
		return
	}
	reply(w, 201, outlet)
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
	if err := s.reconcileProxy(r.Context()); err != nil {
		fail(w, r, 503, "proxy_reconcile_failed")
		return
	}
	reply(w, 200, outlet)
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
	// A fresh token can recover an expired identity. Active credentials cannot be
	// silently replaced. Tunnel-token revocation belongs to server config rollout.
	if record.Enrolled && s.now().Before(record.CredentialExpiresAt) && r.URL.Query().Get("reset") != "true" {
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

func (s *Store) issueManagedToken(w http.ResponseWriter, r *http.Request) {
	remoteAddress := s.config.TunnelRemoteAddress
	if remoteAddress == "" {
		outlet, err := s.control.Outlet(r.Context())
		if err != nil {
			fail(w, r, 503, "persistence_failed")
			return
		}
		if outlet.Domain != "" {
			_, port, err := net.SplitHostPort(s.config.TunnelListenAddress)
			if err == nil {
				remoteAddress = net.JoinHostPort(outlet.Domain, port)
			}
		}
	}
	if remoteAddress == "" || !validation.PublicKey(s.config.TunnelServerPublicKey) || validation.Endpoint(remoteAddress) != nil {
		fail(w, r, 503, "tunnel_runtime_unconfigured")
		return
	}
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
	record, exists := s.managed[id]
	if exists && record.Enrolled && s.now().Before(record.CredentialExpiresAt) && r.URL.Query().Get("reset") != "true" {
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
	record.LastHeartbeat, record.LastSeen, record.TunnelStatus, record.ServiceStatuses = nil, time.Time{}, "unknown", nil
	record.EnrollmentHash, record.EnrollmentExpiresAt = Hash(token), s.now().Add(time.Duration(s.config.EnrollmentTTLSeconds)*time.Second)
	if err := s.saveManaged(id, record); err != nil {
		s.mu.Unlock()
		fail(w, r, 503, "persistence_failed")
		return
	}
	s.mu.Unlock()
	if err := s.reconcileTunnel(); err != nil {
		fail(w, r, 503, "tunnel_reconcile_failed")
		return
	}
	reply(w, 201, map[string]any{"token": token, "expires_at": record.EnrollmentExpiresAt, "services": services})
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
	s.mu.Lock()
	if record, ok := s.managed[r.PathValue("id")]; ok {
		remoteAddress := s.config.TunnelRemoteAddress
		if remoteAddress == "" {
			outlet, outletErr := s.control.Outlet(r.Context())
			_, port, splitErr := net.SplitHostPort(s.config.TunnelListenAddress)
			if outletErr == nil && splitErr == nil {
				remoteAddress = net.JoinHostPort(outlet.Domain, port)
			}
		}
		prior := record.Configuration.ActiveServices()
		record.Configuration = contract.Configuration{ConnectorID: r.PathValue("id"), Revision: record.Configuration.Revision + 1, Services: append([]contract.ServiceConfiguration(nil), prior...)}
		record.Configuration.Services = append(record.Configuration.Services, contract.ServiceConfiguration{ServiceID: service.ID, Target: service.Target, Tunnel: contract.Tunnel{RemoteAddress: remoteAddress, ServerPublicKey: s.config.TunnelServerPublicKey, ServiceName: service.ID, ServiceToken: Token()}})
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
		record.LastHeartbeat, record.ServiceStatuses, record.TunnelStatus = nil, nil, "unknown"
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
	if changed {
		services[index].Target = req.Target
		record.Configuration.ServiceID, record.Configuration.Target, record.Configuration.Tunnel = "", contract.Target{}, contract.Tunnel{}
		record.Configuration.Services = services
		record.Configuration.Revision++
		record.LastHeartbeat = nil
		record.ServiceStatuses = nil
		record.TunnelStatus = "unknown"
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
			record.LastHeartbeat = nil
			record.ServiceStatuses = nil
			record.TunnelStatus = "unknown"
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
	route, err := s.control.CreateHTTPRoute(r.Context(), r.PathValue("id"), req.Hostname)
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
	if err := s.reconcileProxy(r.Context()); err != nil {
		fail(w, r, 503, "proxy_reconcile_failed")
		return
	}
	reply(w, 201, route)
}

func (s *Store) deleteHTTPRoute(w http.ResponseWriter, r *http.Request) {
	if !s.admin(w, r) {
		return
	}
	if s.control == nil {
		fail(w, r, 503, "control_database_unavailable")
		return
	}
	err := s.control.DeleteHTTPRoute(r.Context(), r.PathValue("id"), r.PathValue("route_id"))
	if errors.Is(err, control.ErrRouteNotFound) {
		fail(w, r, 404, "route_not_found")
		return
	}
	if err != nil {
		fail(w, r, 503, "persistence_failed")
		return
	}
	if err := s.reconcileProxy(r.Context()); err != nil {
		fail(w, r, 503, "proxy_reconcile_failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
