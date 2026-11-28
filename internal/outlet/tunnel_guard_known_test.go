package outlet

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/lawnect/meerkat/internal/control"
)

// configurationFrom sends an authenticated Connector API request from remote.
// With a forwarded address it arrives the way Caddy's requests do, which is
// how the Outlet learns where an internet client came from.
func configurationFrom(h http.Handler, remote, forwarded, credential string, headers map[string]string) int {
	r := httptest.NewRequest("GET", "/connector/v1/config", nil)
	r.RemoteAddr = remote
	r.Header.Set("Authorization", "Bearer "+credential)
	for name, value := range headers {
		r.Header.Set(name, value)
	}
	if forwarded != "" {
		r = r.WithContext(context.WithValue(r.Context(), originKey{}, publicOrigin{ip: forwarded}))
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w.Code
}

func knownCount(s *Store) int { return len(s.guard.known.list(s.now())) }

// The table of known addresses is shared by every Connector. One Connector's
// credential, used from thousands of addresses, must only push out that
// Connector's own addresses: the others fall back to the limits of an unknown
// client, or are refused in strict mode.
func TestOneConnectorCannotEvictAnothersKnownAddress(t *testing.T) {
	f := setup(t)
	h := f.store.Handler()
	a := registered(t, f) // connector-a, from 192.0.2.1
	if !isKnown(f.store, "192.0.2.1") {
		t.Fatal("the first Connector's address isn't known")
	}
	w := requestFrom(h, "198.51.100.1:50000", "POST", "/connector/v1/enrollment", f.tokenB, enrollment("connector-b"))
	if w.Code != 201 {
		t.Fatalf("enrollment: %d", w.Code)
	}
	var b struct {
		Credential string `json:"credential"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &b); err != nil {
		t.Fatal(err)
	}
	for i := range 2*guardKnownClients + 10 {
		remote := fmt.Sprintf("[2001:db8:%x:%x::7]:50000", i>>16, i&0xffff)
		if code := configurationFrom(h, remote, "", b.Credential, nil); code != 200 {
			t.Fatalf("config from %s: %d", remote, code)
		}
	}
	if !isKnown(f.store, "192.0.2.1") {
		t.Fatalf("a flood from the second Connector's credential evicted the first Connector's address; known: %d", knownCount(f.store))
	}
	if n := knownCount(f.store); n > 1+guardKnownPerConnector+1 {
		t.Fatalf("%d known addresses after one Connector's flood, want at most %d", n, 1+guardKnownPerConnector+1)
	}
	// The first Connector still learns more of its own.
	if code := configurationFrom(h, "192.0.2.77:50000", "", a.Credential, nil); code != 200 || !isKnown(f.store, "192.0.2.77") {
		t.Fatalf("the first Connector can no longer add addresses: %d", code)
	}
}

type warningView struct {
	Warnings []struct {
		Code     string `json:"code"`
		Client   string `json:"client"`
		LastSeen string `json:"last_seen"`
	} `json:"warnings"`
}

// Behind Docker's port forwarding, or rootless networking, every client looks
// like one internal address. Making that address known would pass every
// internet client on the tunnel port as a Connector, in strict mode too.
func TestSharedAddressesAreNotLearnedAsConnectors(t *testing.T) {
	const caddy = "172.18.0.5:41000"
	for _, tc := range []struct {
		name, remote, forwarded string
		headers                 map[string]string
		known                   bool
	}{
		{"an internet client through Caddy", caddy, "203.0.113.9", nil, true},
		{"an internet IPv6 client through Caddy", caddy, "2001:db8:5:6:7:8:9:a", nil, true},
		{"the Docker gateway through Caddy", caddy, "172.18.0.1", nil, false},
		{"the rootless gateway through Caddy", caddy, "10.0.2.100", nil, false},
		{"a unique local address through Caddy", caddy, "fd00::1", nil, false},
		{"loopback through Caddy", caddy, "127.0.0.1", nil, false},
		{"a link-local address through Caddy", caddy, "169.254.7.7", nil, false},
		{"a carrier-grade NAT address through Caddy", caddy, "100.64.0.9", nil, false},
		{"a CDN edge through Caddy", caddy, "203.0.113.50", map[string]string{"Cf-Ray": "8a1b2c3d4e5f-ICN"}, false},
		// On the local admin port the peer is the Connector itself: a container on
		// the same Docker network or a host on the same LAN.
		{"a container on the Docker network", "172.20.0.7:50000", "", nil, true},
		{"a host on the LAN", "192.168.1.20:50000", "", nil, true},
		{"an IPv6 peer in a unique local range", "[fd00::7]:50000", "", nil, true},
		{"a public peer", "203.0.113.77:50000", "", nil, true},
		{"loopback on the local port", "127.0.0.1:50000", "", nil, false},
		{"IPv6 loopback on the local port", "[::1]:50000", "", nil, false},
		{"a link-local peer on the local port", "169.254.3.4:50000", "", nil, false},
		{"a carrier-grade NAT peer on the local port", "100.64.3.4:50000", "", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newAlertFixture(t)
			h := a.store.Handler()
			credential := a.enroll(t, a.tokenA, "connector-a").Credential
			// The enrollment came from 192.0.2.1; the case is judged on its own address.
			client := tc.forwarded
			if client == "" {
				client = trimBrackets(tc.remote[:len(tc.remote)-len(":50000")])
			}
			if code := configurationFrom(h, tc.remote, tc.forwarded, credential, tc.headers); code != 200 {
				t.Fatalf("config: %d", code)
			}
			if got := isKnown(a.store, client); got != tc.known {
				t.Fatalf("%s known = %v, want %v", client, got, tc.known)
			}
			var view warningView
			w := request(h, "GET", "/api/v1/security/tunnel-guard", a.admin, nil)
			if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &view) != nil {
				t.Fatalf("tunnel guard: %d %s", w.Code, w.Body.String())
			}
			warned := false
			for _, warning := range view.Warnings {
				if warning.Code == "shared_address" && warning.Client == clientName(clientPrefix(netip.MustParseAddr(client))) && warning.LastSeen != "" {
					warned = true
				}
			}
			if warned == tc.known {
				t.Fatalf("warning for %s = %v with known = %v: %+v", client, warned, tc.known, view.Warnings)
			}
		})
	}
}

func trimBrackets(s string) string {
	if len(s) > 1 && s[0] == '[' {
		return s[1 : len(s)-1]
	}
	return s
}

// The scenario that matters: every client reaches the tunnel port from the
// Docker gateway and a Connector's request through Caddy shows the same
// address. Strict mode must still turn the internet away.
func TestStrictModeIsNotOpenedByAGatewayAddress(t *testing.T) {
	f := newAlertFixture(t)
	credential := f.enroll(t, f.tokenA, "connector-a").Credential
	f.store.guard.config.Store(compileTunnelGuard(control.TunnelGuardSettings{Mode: control.TunnelGuardStrict, Allowed: []string{}}))
	if code := configurationFrom(f.store.Handler(), "172.18.0.5:41000", "172.18.0.1", credential, nil); code != 200 {
		t.Fatalf("config: %d", code)
	}
	if _, _, why := f.store.guard.admit(netip.MustParseAddr("172.18.0.1")); why != refusedStrict {
		t.Fatalf("a client behind the gateway address: %v, want refused in strict mode", why)
	}
	// A Connector whose address is its own still passes.
	if code := configurationFrom(f.store.Handler(), "172.18.0.5:41000", "203.0.113.9", credential, nil); code != 200 {
		t.Fatalf("config: %d", code)
	}
	release, _, why := f.store.guard.admit(netip.MustParseAddr("203.0.113.9"))
	if why != admitted {
		t.Fatalf("a known Connector in strict mode: %v", why)
	}
	release()
}
