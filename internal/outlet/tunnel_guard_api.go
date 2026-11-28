package outlet

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"time"

	"github.com/lawnect/meerkat/internal/control"
	"github.com/lawnect/meerkat/internal/validation"
)

// defaultTunnelInternalAddress is where rathole listens when outlet.json has
// no tunnel_internal_address: the loopback address on the port after the
// tunnel port, or the one before it if that is the last port. The usual 2333
// gives DefaultTunnelInternalAddress, and a tunnel port of 2334 doesn't
// collide with it. If the tunnel port doesn't parse, which validation
// refuses, it is DefaultTunnelInternalAddress.
func defaultTunnelInternalAddress(listen string) string {
	_, port, err := net.SplitHostPort(listen)
	if err != nil {
		return DefaultTunnelInternalAddress
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return DefaultTunnelInternalAddress
	}
	next := n + 1
	if next > 65535 {
		next = n - 1
	}
	return net.JoinHostPort("127.0.0.1", strconv.Itoa(next))
}

// validateTunnelAddresses checks where the Outlet listens for tunnels and
// where rathole does. rathole must be reachable only from inside the Outlet's
// network namespace, or the guard could be walked around, and can't share
// the public port.
func validateTunnelAddresses(listen, internal string) error {
	if err := validation.Endpoint(listen); err != nil {
		return errors.New("invalid tunnel_listen_address")
	}
	if err := validation.Endpoint(internal); err != nil || !isLoopbackAddress(internal) {
		return errors.New("tunnel_internal_address must be a loopback IP address with a port, such as " + DefaultTunnelInternalAddress)
	}
	_, listenPort, _ := net.SplitHostPort(listen)
	if _, internalPort, _ := net.SplitHostPort(internal); internalPort == listenPort {
		return errors.New("tunnel_internal_address must use another port than tunnel_listen_address")
	}
	return nil
}

func (s *Store) reloadTunnelGuard(ctx context.Context) error {
	if s.guard == nil {
		return nil
	}
	settings := control.DefaultTunnelGuard()
	if s.control != nil {
		var err error
		if settings, err = s.control.TunnelGuard(ctx); err != nil {
			return err
		}
	}
	s.guard.config.Store(compileTunnelGuard(settings))
	return nil
}

// RunTunnelGuard listens on the public tunnel port, admits connections and
// proxies them to rathole until ctx ends or the Store is closed. Without a
// tunnel_listen_address it does nothing. The Outlet runs on whether or not
// the port can be bound.
func (s *Store) RunTunnelGuard(ctx context.Context) {
	if s.guard == nil || s.config.TunnelListenAddress == "" {
		return
	}
	s.guard.run(ctx)
}

// learnConnector makes the address of an authenticated Connector API request
// known to the tunnel guard, so the Connector's tunnel connections pass
// without the limits of unknown clients. Call it only once the request has
// been authenticated: an address anyone could make known would protect
// nothing. The address is held for the Connector id, which has a few of them.
//
// An address many clients share is not learned: behind Docker's port
// forwarding or rootless networking every client looks like one internal
// address, and making it known would pass every internet client on the tunnel
// port as a Connector. Such addresses are listed as warnings instead.
func (s *Store) learnConnector(r *http.Request, id string) {
	if s.guard == nil {
		return
	}
	ip, public := requestOrigin(r)
	if ip == "" {
		return
	}
	if sharedConnectorAddress(r, ip, public) {
		s.guard.refuseShared(ip)
		return
	}
	s.guard.learnFor(id, ip)
}

// sharedConnectorAddress says that clients other than the Connector may reach
// the tunnel port from ip. It is sharedClient's notion, with one exception:
// a peer of the Outlet's own admin port, which Connectors on the same Docker
// network or LAN use directly, is that Connector whatever private address it
// has. Through Caddy a private address is the gateway every client appears
// behind, and a Connector in the same place has to be allowed by address
// range instead. Loopback, link-local and carrier-grade NAT addresses are
// never a single Connector.
func sharedConnectorAddress(r *http.Request, ip string, public bool) bool {
	if !sharedClient(r, ip) {
		return false
	}
	addr, err := netip.ParseAddr(ip)
	return public || err != nil || !addr.Unmap().IsPrivate()
}

type tunnelGuardConnector struct {
	Client   string `json:"client"`
	LastSeen string `json:"last_seen"`
}

// tunnelGuardWarning says why the guard can't tell Connectors from other
// clients at an address. "shared_address": a Connector's request came from an
// address other clients may share, so it was not made known; add it under
// Allowed if the Connectors really are the only ones behind it.
type tunnelGuardWarning struct {
	Code     string `json:"code"`
	Client   string `json:"client"`
	LastSeen string `json:"last_seen"`
}

type tunnelGuardView struct {
	Settings   control.TunnelGuardSettings `json:"settings"`
	Stats      tunnelGuardStats            `json:"stats"`
	Connectors []tunnelGuardConnector      `json:"connectors"`
	Warnings   []tunnelGuardWarning        `json:"warnings"`
}

func (s *Store) tunnelGuardView(ctx context.Context) (tunnelGuardView, error) {
	settings, err := s.control.TunnelGuard(ctx)
	if err != nil {
		return tunnelGuardView{}, err
	}
	view := tunnelGuardView{Settings: settings, Stats: s.guard.stats(), Connectors: []tunnelGuardConnector{}, Warnings: []tunnelGuardWarning{}}
	for _, known := range s.guard.known.list(s.now()) {
		view.Connectors = append(view.Connectors, tunnelGuardConnector{Client: clientName(known.key), LastSeen: known.seen.UTC().Format(time.RFC3339)})
	}
	for _, shared := range s.guard.shared.list(s.now()) {
		view.Warnings = append(view.Warnings, tunnelGuardWarning{Code: "shared_address", Client: clientName(shared.key), LastSeen: shared.seen.UTC().Format(time.RFC3339)})
	}
	return view, nil
}

func (s *Store) getTunnelGuard(w http.ResponseWriter, r *http.Request) {
	if !s.alertsAdmin(w, r) {
		return
	}
	view, err := s.tunnelGuardView(r.Context())
	if err != nil {
		fail(w, r, 503, "persistence_failed")
		return
	}
	reply(w, 200, view)
}

func (s *Store) putTunnelGuard(w http.ResponseWriter, r *http.Request) {
	if !s.alertsAdmin(w, r) {
		return
	}
	var req control.TunnelGuardSettings
	if !decode(w, r, &req, "mode", "allowed") {
		return
	}
	s.saving.Lock()
	saved, err := s.control.SetTunnelGuard(r.Context(), req, s.now())
	if err == nil {
		err = s.reloadTunnelGuard(context.WithoutCancel(r.Context()))
	}
	s.saving.Unlock()
	if errors.Is(err, control.ErrInvalidTunnelGuard) {
		fail(w, r, 400, "invalid_tunnel_guard")
		return
	}
	if err != nil {
		fail(w, r, 503, "persistence_failed")
		return
	}
	raw, _ := json.Marshal(saved)
	var detail map[string]any
	_ = json.Unmarshal(raw, &detail)
	s.audited(r, "security.tunnel_guard", "security", "tunnel_guard", detail)
	view, err := s.tunnelGuardView(r.Context())
	if err != nil {
		fail(w, r, 503, "persistence_failed")
		return
	}
	reply(w, 200, view)
}
