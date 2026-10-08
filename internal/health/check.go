package health

import (
	"context"
	"errors"
	"net"
	"time"

	"github.com/lawnect/meerkat/internal/contract"
	"github.com/lawnect/meerkat/internal/validation"
)

// Resolve pins the data-plane destination to a checked local IP. Rathole must
// not independently resolve a single-label name to an unchecked public address.
func Resolve(ctx context.Context, target contract.Target) (string, error) {
	if err := validation.Target(target); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, target.Host)
	if err != nil || len(ips) == 0 {
		return "", errors.New("target name cannot be resolved")
	}
	for _, ip := range ips {
		if !ip.IP.IsLoopback() && !ip.IP.IsPrivate() {
			return "", errors.New("target name resolves outside the local service network")
		}
	}
	return validation.Address(contract.Target{Host: ips[0].IP.String(), Port: target.Port}), nil
}

func Check(ctx context.Context, target contract.Target) string {
	if validation.Target(target) != nil {
		return "unhealthy"
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, target.Host)
	if err != nil || len(ips) == 0 {
		return "unhealthy"
	}
	// Single-label names must resolve locally. Never probe a public IP target.
	for _, ip := range ips {
		if !ip.IP.IsLoopback() && !ip.IP.IsPrivate() {
			return "unhealthy"
		}
	}
	for _, ip := range ips {
		t := contract.Target{Host: ip.IP.String(), Port: target.Port}
		conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", validation.Address(t))
		if err == nil {
			conn.Close()
			return "healthy"
		}
	}
	return "unhealthy"
}
