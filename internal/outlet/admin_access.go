package outlet

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/netip"

	"github.com/lawnect/meerkat/internal/control"
)

// adminAccessRule is the compiled control.AdminAccess: its ranges and the
// address book entries carrying its tags.
type adminAccessRule struct {
	control.AdminAccess
	allowed []netip.Prefix
}

func compileAdminAccess(a control.AdminAccess, book control.IPAddresses) *adminAccessRule {
	return &adminAccessRule{AdminAccess: a, allowed: scopePrefixes(book, a.AllowedTags, a.Allowed)}
}

func (rule *adminAccessRule) allows(ip string) bool { return prefixesContain(rule.allowed, ip) }

// adminOpenPaths answer on the management host whatever the IP limit: the
// Connector API, which tunnels reach from anywhere, access protection's
// sign-in, where visitors of protected addresses are sent, and /healthz,
// which the guide has external monitors check from addresses nobody can
// list. Exact paths, so no unclean variant of one gets past the limit to a
// ServeMux redirect.
var adminOpenPaths = map[string]bool{
	"/connector/v1/enrollment": true,
	"/connector/v1/config":     true,
	"/connector/v1/heartbeat":  true,
	accessStartPath:            true,
	accessCallbackPath:         true,
	"/healthz":                 true,
}

func (s *Store) reloadAdminAccess(ctx context.Context) error {
	if s.control == nil {
		return nil
	}
	a, err := s.control.AdminAccess(ctx)
	if errors.Is(err, control.ErrDamagedAdminAccess) {
		slog.Warn("the saved admin console IP limit can't be read, so the console is closed on public addresses until it is saved again over SSH or MEERKAT_ADMIN_IP_LIMIT=false lifts it")
	} else if err != nil {
		return err
	}
	book, _, err := s.ipAddressBook(ctx)
	if err != nil {
		return err
	}
	s.adminAccess.Store(compileAdminAccess(a, book))
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

// isAdminHost reports whether host is the management host.
func (s *Store) isAdminHost(ctx context.Context, host string) bool {
	routing, err := s.control.Routing(ctx)
	return err == nil && host != "" && host == routing.Outlet.AdminHostname
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

// sharedClient reports whether the address the Outlet sees is likely one
// many clients share: a private, loopback or link-local address (a proxy or
// Docker's port forwarding in front of Caddy) or a request through a CDN.
// Allowing it would let everyone behind it in.
func sharedClient(r *http.Request, ip string) bool {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return false
	}
	addr = addr.Unmap()
	cgnat := netip.MustParsePrefix("100.64.0.0/10")
	return addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast() || cgnat.Contains(addr) ||
		r.Header.Get("Cf-Ray") != "" || r.Header.Get("Cdn-Loop") != ""
}

type adminAccessView struct {
	control.AdminAccess
	// Client is the address the request came from on a public address, and
	// ClientRange what adding it would add; both are null on the local port.
	Client      *string `json:"client"`
	ClientRange *string `json:"client_range"`
	// ClientShared says the client address looks shared (see sharedClient).
	ClientShared bool `json:"client_shared"`
	// Damaged says the stored settings couldn't be read and the console is
	// closed on public addresses until they are saved again.
	Damaged bool `json:"damaged"`
	// Lifted says MEERKAT_ADMIN_IP_LIMIT=false is ignoring the limit.
	Lifted bool `json:"lifted"`
}

func (s *Store) adminAccessView(r *http.Request, a control.AdminAccess) adminAccessView {
	view := adminAccessView{AdminAccess: a, Lifted: s.control.AdminIPLimitLifted()}
	if ip, public := requestOrigin(r); public && ip != "" {
		addRange := clientRange(ip)
		view.Client, view.ClientRange, view.ClientShared = &ip, &addRange, sharedClient(r, ip)
	}
	return view
}

func (s *Store) getAdminAccess(w http.ResponseWriter, r *http.Request) {
	if !s.alertsAdmin(w, r) {
		return
	}
	a, err := s.control.AdminAccess(r.Context())
	damaged := errors.Is(err, control.ErrDamagedAdminAccess)
	if err != nil && !damaged {
		fail(w, r, 503, "persistence_failed")
		return
	}
	view := s.adminAccessView(r, a)
	view.Damaged = damaged
	reply(w, 200, view)
}

func (s *Store) putAdminAccess(w http.ResponseWriter, r *http.Request) {
	if !s.alertsAdmin(w, r) {
		return
	}
	var req control.AdminAccess
	// allowed_tags too: a console from before tags would drop them.
	if !decode(w, r, &req, "restricted", "allowed", "allowed_tags") {
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
	s.saving.Lock()
	// Saving from the internet must not shut out the one saving: their
	// address has to be in the ranges or carry one of the tags. The local
	// admin port can close the console to the internet altogether.
	if ip, public := requestOrigin(r); public && next.Restricted {
		book, damaged, err := s.ipAddressBook(r.Context())
		if err != nil {
			s.saving.Unlock()
			fail(w, r, 503, "persistence_failed")
			return
		}
		// Unknown tags are refused as such before the lock-out check, which
		// would otherwise report them as leaving this address out.
		for _, tag := range next.AllowedTags {
			if _, ok := book.Spelling(tag); !ok {
				s.saving.Unlock()
				if damaged {
					fail(w, r, 409, "ip_addresses_damaged")
				} else {
					fail(w, r, 400, "invalid_admin_access")
				}
				return
			}
		}
		if !compileAdminAccess(next, book).allows(ip) {
			s.saving.Unlock()
			fail(w, r, 409, "admin_access_locks_out")
			return
		}
	}
	saved, err := s.control.SetAdminAccess(r.Context(), next, s.now())
	if err == nil {
		err = s.reloadAdminAccess(context.WithoutCancel(r.Context()))
	}
	s.saving.Unlock()
	if errors.Is(err, control.ErrInvalidAdminAccess) {
		fail(w, r, 400, "invalid_admin_access")
		return
	}
	if errors.Is(err, control.ErrStoredIPAddresses) {
		fail(w, r, 409, "ip_addresses_damaged")
		return
	}
	if err != nil {
		fail(w, r, 503, "persistence_failed")
		return
	}
	s.audited(r, "security.admin_access", "security", "admin_access", map[string]any{"restricted": saved.Restricted, "allowed": saved.Allowed, "allowed_tags": saved.AllowedTags})
	reply(w, 200, s.adminAccessView(r, saved))
}
