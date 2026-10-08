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

func Label(s string) bool { return label.MatchString(s) }
func Token(s string) bool { return token.MatchString(s) }

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
	if err != nil || host == "" || strings.ContainsAny(host, "\n\r\t\"\\ /@") {
		return errors.New("invalid tunnel endpoint")
	}
	p, err := strconv.Atoi(port)
	if err != nil || p < 1 || p > 65535 {
		return errors.New("invalid tunnel port")
	}
	return nil
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
	if !Label(c.ConnectorID) || !Label(c.ServiceID) || !Label(c.Tunnel.ServiceName) || c.Revision < 1 {
		return errors.New("invalid service identity or revision")
	}
	if err := Target(c.Target); err != nil {
		return err
	}
	if err := Endpoint(c.Tunnel.RemoteAddress); err != nil {
		return err
	}
	if !PublicKey(c.Tunnel.ServerPublicKey) || !Token(c.Tunnel.ServiceToken) {
		return errors.New("invalid Noise key or service token")
	}
	return nil
}
