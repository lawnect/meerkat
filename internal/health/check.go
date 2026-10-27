package health

import (
	"context"
	"errors"
	"net"
	"sync"
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

// Every check can take up to its full timeout, so a connector with several
// unreachable targets must not run them one after another: the heartbeat would
// outlast the Outlet's staleness window and take every route down.
const maxParallel = 16

// Test seams for the per-target work.
var (
	resolveOne = Resolve
	checkOne   = Check
)

// each runs fn for every index with at most maxParallel running at a time and
// returns when all are done. fn must only write to its own index.
func each(n int, fn func(i int)) {
	var wg sync.WaitGroup
	slots := make(chan struct{}, maxParallel)
	for i := range n {
		slots <- struct{}{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-slots }()
			fn(i)
		}()
	}
	wg.Wait()
}

// ResolveAll resolves every service target at once. Services whose target
// cannot be resolved are left out of the result.
func ResolveAll(ctx context.Context, services []contract.ServiceConfiguration) map[string]string {
	addresses := make([]string, len(services))
	each(len(services), func(i int) {
		if address, err := resolveOne(ctx, services[i].Target); err == nil {
			addresses[i] = address
		}
	})
	resolved := make(map[string]string, len(services))
	for i, service := range services {
		if addresses[i] != "" {
			resolved[service.ServiceID] = addresses[i]
		}
	}
	return resolved
}

// CheckAll checks every service target at once and returns the health by
// service ID.
func CheckAll(ctx context.Context, services []contract.ServiceConfiguration) map[string]string {
	states := make([]string, len(services))
	each(len(services), func(i int) { states[i] = checkOne(ctx, services[i].Target) })
	healths := make(map[string]string, len(services))
	for i, service := range services {
		healths[service.ServiceID] = states[i]
	}
	return healths
}
