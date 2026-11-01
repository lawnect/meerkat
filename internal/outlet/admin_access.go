package outlet

import (
	"context"
	"errors"
	"net/http"
	"net/netip"
	"slices"

	"github.com/lawnect/meerkat/internal/control"
)

// adminAccessRule is the compiled control.AdminAccess.
type adminAccessRule struct {
	control.AdminAccess
	allowed []netip.Prefix
}

func compileAdminAccess(a control.AdminAccess) *adminAccessRule {
	rule := &adminAccessRule{AdminAccess: a}
	for _, entry := range a.Allowed {
		if prefix, err := netip.ParsePrefix(entry); err == nil {
			rule.allowed = append(rule.allowed, prefix)
		} else if addr, err := netip.ParseAddr(entry); err == nil {
			rule.allowed = append(rule.allowed, netip.PrefixFrom(addr, addr.BitLen()))
		}
	}
	return rule
}

func (rule *adminAccessRule) allows(ip string) bool {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return false
	}
	addr = addr.Unmap()
	return slices.ContainsFunc(rule.allowed, func(prefix netip.Prefix) bool { return prefix.Contains(addr) })
}

// adminOpenPaths answer on the management host whatever the IP limit: the
// Connector API, which tunnels reach from anywhere, and access protection's
// sign-in, where visitors of protected addresses are sent. Exact paths, so
// no unclean variant of one gets past the limit to a ServeMux redirect.
var adminOpenPaths = map[string]bool{
	"/connector/v1/enrollment": true,
	"/connector/v1/config":     true,
	"/connector/v1/heartbeat":  true,
	accessStartPath:            true,
	accessCallbackPath:         true,
}

func (s *Store) reloadAdminAccess(ctx context.Context) error {
	if s.control == nil {
		return nil
	}
	a, err := s.control.AdminAccess(ctx)
	if err != nil {
		return err
	}
	s.adminAccess.Store(compileAdminAccess(a))
	return nil
}

// adminReachable reports whether a public request to the management host
// may reach the console. The local admin port never comes through here.
func (s *Store) adminReachable(r *http.Request) bool {
	rule := s.adminAccess.Load()
	if rule == nil || !rule.Restricted || s.control == nil || s.control.AdminIPLimitLifted() || adminOpenPaths[r.URL.Path] {
		return true
	}
	ip, _ := requestOrigin(r)
	return rule.allows(ip)
}

// clientRange is what "add current IP" adds: the address itself for IPv4,
// and its /64 for IPv6, where a client's address changes within its network.
func clientRange(ip string) string {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return ""
	}
	addr = addr.Unmap()
	if addr.Is4() {
		return addr.String()
	}
	return netip.PrefixFrom(addr, 64).Masked().String()
}

type adminAccessView struct {
	control.AdminAccess
	// Client is the address the request came from on a public address, and
	// ClientRange what adding it would add; both are null on the local port.
	Client      *string `json:"client"`
	ClientRange *string `json:"client_range"`
	// Lifted says MEERKAT_ADMIN_IP_LIMIT=false is ignoring the limit.
	Lifted bool `json:"lifted"`
}

func (s *Store) adminAccessView(r *http.Request, a control.AdminAccess) adminAccessView {
	view := adminAccessView{AdminAccess: a, Lifted: s.control.AdminIPLimitLifted()}
	if ip, public := requestOrigin(r); public && ip != "" {
		addRange := clientRange(ip)
		view.Client, view.ClientRange = &ip, &addRange
	}
	return view
}

func (s *Store) getAdminAccess(w http.ResponseWriter, r *http.Request) {
	if !s.alertsAdmin(w, r) {
		return
	}
	a, err := s.control.AdminAccess(r.Context())
	if err != nil {
		fail(w, r, 503, "persistence_failed")
		return
	}
	reply(w, 200, s.adminAccessView(r, a))
}

func (s *Store) putAdminAccess(w http.ResponseWriter, r *http.Request) {
	if !s.alertsAdmin(w, r) {
		return
	}
	var req control.AdminAccess
	if !decode(w, r, &req, "restricted", "allowed") {
		return
	}
	if req.Allowed == nil {
		req.Allowed = []string{}
	}
	next, ok := req.Normalize()
	if !ok {
		fail(w, r, 400, "invalid_admin_access")
		return
	}
	// Saving from the internet must not shut out the one saving: their
	// address has to be in the list. The local admin port can close the
	// console to the internet altogether.
	if ip, public := requestOrigin(r); public && next.Restricted && !compileAdminAccess(next).allows(ip) {
		fail(w, r, 409, "admin_access_locks_out")
		return
	}
	s.saving.Lock()
	saved, err := s.control.SetAdminAccess(r.Context(), next, s.now())
	if err == nil {
		err = s.reloadAdminAccess(context.WithoutCancel(r.Context()))
	}
	s.saving.Unlock()
	if errors.Is(err, control.ErrInvalidAdminAccess) {
		fail(w, r, 400, "invalid_admin_access")
		return
	}
	if err != nil {
		fail(w, r, 503, "persistence_failed")
		return
	}
	s.audited(r, "security.admin_access", "security", "admin_access", map[string]any{"restricted": saved.Restricted, "allowed": saved.Allowed})
	reply(w, 200, s.adminAccessView(r, saved))
}
