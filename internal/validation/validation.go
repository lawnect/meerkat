package validation

import (
	"encoding/base64"
	"errors"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/lawnect/meerkat/internal/contract"
)

var label = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,62}$`)
var token = regexp.MustCompile(`^[a-zA-Z0-9_-]{32,128}$`)
var hostnameLabel = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

func Label(s string) bool { return label.MatchString(s) }
func Token(s string) bool { return token.MatchString(s) }

// Hostname permits a public DNS name, not an IP literal, wildcard, or a
// trailing-dot resolver form. Outlet routes use a single concrete hostname.
func Hostname(s string) bool {
	if len(s) > 253 || strings.ToLower(s) != s || !strings.Contains(s, ".") || net.ParseIP(s) != nil {
		return false
	}
	for _, part := range strings.Split(s, ".") {
		if !hostnameLabel.MatchString(part) {
			return false
		}
	}
	return true
}

func Target(t contract.Target) error {
	if t.Port < 1 || t.Port > 65535 {
		return errors.New("target port must be 1..65535")
	}
	if ip := net.ParseIP(t.Host); ip != nil {
		if !ip.IsLoopback() {
			return errors.New("IP targets must be loopback")
		}
		return nil
	}
	if !Label(t.Host) || strings.EqualFold(t.Host, "localhost") && t.Host != "localhost" {
		return errors.New("target host must be an explicit Compose service name or loopback")
	}
	return nil
}

func Address(t contract.Target) string { return net.JoinHostPort(t.Host, strconv.Itoa(t.Port)) }

func Endpoint(s string) error {
	host, port, err := net.SplitHostPort(s)
	// Only the canonical form: brackets only around an IPv6 literal, so the
	// value also works inside a URL.
	if err != nil || host == "" || strings.ContainsAny(host, "\n\r\t\"\\ /@") || net.JoinHostPort(host, port) != s {
		return errors.New("invalid tunnel endpoint")
	}
	p, err := strconv.Atoi(port)
	if err != nil || p < 1 || p > 65535 || strconv.Itoa(p) != port {
		return errors.New("invalid tunnel port")
	}
	return nil
}

// TunnelAddress permits the host:port a Connector dials for the data tunnel:
// a lowercase DNS name, a single-label Compose service name or an IP literal.
// Loopback is rejected: it names the Connector's own host, never the Outlet.
func TunnelAddress(s string) bool {
	if Endpoint(s) != nil {
		return false
	}
	host, _, _ := net.SplitHostPort(s)
	if ip := net.ParseIP(host); ip != nil {
		return !ip.IsUnspecified() && !ip.IsLoopback()
	}
	return host != "localhost" && (Hostname(host) || hostnameLabel.MatchString(host))
}

func PublicKey(s string) bool {
	b, err := base64.StdEncoding.DecodeString(s)
	return err == nil && len(b) == 32
}

// Development HTTP is an explicit opt-in. The CLI additionally restricts plain
// HTTP listeners to loopback; Compose uses it only on an isolated local network.
func OutletURL(s string, development bool) error {
	u, err := url.Parse(s)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return errors.New("outlet must be an origin URL without credentials, path or query")
	}
	if u.Scheme != "https" && !(development && u.Scheme == "http") {
		return errors.New("outlet requires HTTPS (HTTP is development-only)")
	}
	return nil
}

func Configuration(c contract.Configuration) error {
	if !Label(c.ConnectorID) || c.Revision < 0 {
		return errors.New("invalid connector identity or revision")
	}
	services := c.ActiveServices()
	if len(services) == 0 {
		if c.ServiceID != "" || c.Target != (contract.Target{}) || c.Tunnel != (contract.Tunnel{}) {
			return errors.New("invalid empty configuration")
		}
		return nil
	}
	if c.Revision < 1 {
		return errors.New("invalid service revision")
	}
	seen := map[string]bool{}
	for _, service := range services {
		if !Label(service.ServiceID) || !Label(service.Tunnel.ServiceName) || service.Tunnel.ServiceName != service.ServiceID || seen[service.ServiceID] {
			return errors.New("invalid or duplicate service identity")
		}
		seen[service.ServiceID] = true
		if err := Target(service.Target); err != nil {
			return err
		}
		if err := Endpoint(service.Tunnel.RemoteAddress); err != nil {
			return err
		}
		if !PublicKey(service.Tunnel.ServerPublicKey) || !Token(service.Tunnel.ServiceToken) {
			return errors.New("invalid Noise key or service token")
		}
	}
	if len(c.Services) != 0 {
		if c.ServiceID != "" || c.Target != (contract.Target{}) || c.Tunnel != (contract.Tunnel{}) {
			return errors.New("mixed service configuration")
		}
		remote, key := services[0].Tunnel.RemoteAddress, services[0].Tunnel.ServerPublicKey
		for _, service := range services[1:] {
			if service.Tunnel.RemoteAddress != remote || service.Tunnel.ServerPublicKey != key {
				return errors.New("services must share a tunnel endpoint")
			}
		}
	}
	return nil
}
