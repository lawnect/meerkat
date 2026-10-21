package rathole

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/lawnect/meerkat/internal/contract"
	"github.com/lawnect/meerkat/internal/validation"
)

const Version = "0.5.0"

func Client(c contract.Configuration) ([]byte, error) {
	return client(c, validation.Address(c.Target))
}

// ClientAt uses a resolved, pinned local IP while retaining the original
// allowlisted target in the control-plane configuration.
func ClientAt(c contract.Configuration, address string) ([]byte, error) {
	host, port, err := net.SplitHostPort(address)
	ip := net.ParseIP(host)
	if err != nil || ip == nil || (!ip.IsLoopback() && !ip.IsPrivate()) || port != strconv.Itoa(c.Target.Port) {
		return nil, errors.New("resolved target must be a local IP with the allowed port")
	}
	return client(c, address)
}

func client(c contract.Configuration, address string) ([]byte, error) {
	if err := validation.Configuration(c); err != nil {
		return nil, err
	}
	return []byte(fmt.Sprintf(`[client]
remote_addr = %s
retry_interval = 1

[client.transport]
type = "noise"

[client.transport.noise]
pattern = "Noise_NK_25519_ChaChaPoly_BLAKE2s"
remote_public_key = %s

[client.services.%s]
type = "tcp"
token = %s
local_addr = %s
`, strconv.Quote(c.Tunnel.RemoteAddress), strconv.Quote(c.Tunnel.ServerPublicKey), c.Tunnel.ServiceName, strconv.Quote(c.Tunnel.ServiceToken), strconv.Quote(address))), nil
}

// ClientsAt places reachable services on one rathole client connection.
// Services without a resolved address remain configured on the Outlet, but
// cannot hold up the tunnels for healthy services.
func ClientsAt(c contract.Configuration, addresses map[string]string) ([]byte, error) {
	if err := validation.Configuration(c); err != nil {
		return nil, err
	}
	services := c.ActiveServices()
	if len(services) == 0 {
		return nil, errors.New("no services configured")
	}
	if len(addresses) == 0 {
		return nil, errors.New("no resolved services")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "[client]\nremote_addr = %s\nretry_interval = 1\n\n[client.transport]\ntype = \"noise\"\n\n[client.transport.noise]\npattern = \"Noise_NK_25519_ChaChaPoly_BLAKE2s\"\nremote_public_key = %s\n", strconv.Quote(services[0].Tunnel.RemoteAddress), strconv.Quote(services[0].Tunnel.ServerPublicKey))
	included := 0
	for _, service := range services {
		address, ok := addresses[service.ServiceID]
		if !ok {
			continue
		}
		host, port, err := net.SplitHostPort(address)
		ip := net.ParseIP(host)
		if err != nil || ip == nil || (!ip.IsLoopback() && !ip.IsPrivate()) || port != strconv.Itoa(service.Target.Port) {
			return nil, errors.New("resolved target must be a local IP with the allowed port")
		}
		fmt.Fprintf(&b, "\n[client.services.%s]\ntype = \"tcp\"\ntoken = %s\nlocal_addr = %s\n", service.Tunnel.ServiceName, strconv.Quote(service.Tunnel.ServiceToken), strconv.Quote(address))
		included++
	}
	if included == 0 {
		return nil, errors.New("no configured services have resolved addresses")
	}
	return []byte(b.String()), nil
}

type ServerService struct{ Name, Token, BindAddress string }

func Server(bind, privateKey string, services []ServerService) ([]byte, error) {
	if err := validation.Endpoint(bind); err != nil {
		return nil, err
	}
	if !validation.PublicKey(privateKey) {
		return nil, errors.New("invalid Noise private key")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "[server]\nbind_addr = %s\n\n[server.transport]\ntype = \"noise\"\n\n[server.transport.noise]\npattern = \"Noise_NK_25519_ChaChaPoly_BLAKE2s\"\nlocal_private_key = %s\n", strconv.Quote(bind), strconv.Quote(privateKey))
	seen := map[string]bool{}
	for _, s := range services {
		if !validation.Label(s.Name) || seen[s.Name] || !validation.Token(s.Token) {
			return nil, errors.New("invalid or duplicate service")
		}
		seen[s.Name] = true
		if err := validation.Endpoint(s.BindAddress); err != nil {
			return nil, err
		}
		fmt.Fprintf(&b, "\n[server.services.%s]\ntype = \"tcp\"\ntoken = %s\nbind_addr = %s\n", s.Name, strconv.Quote(s.Token), strconv.Quote(s.BindAddress))
	}
	return []byte(b.String()), nil
}
