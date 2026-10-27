package health

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lawnect/meerkat/internal/contract"
)

func testServices(n int) []contract.ServiceConfiguration {
	services := make([]contract.ServiceConfiguration, n)
	for i := range services {
		services[i] = contract.ServiceConfiguration{ServiceID: fmt.Sprintf("service-%d", i), Target: contract.Target{Host: fmt.Sprintf("app-%d", i), Port: 3000 + i}}
	}
	return services
}

func TestCheckAllRunsSlowChecksAtOnce(t *testing.T) {
	const delay = 300 * time.Millisecond
	old := checkOne
	defer func() { checkOne = old }()
	checkOne = func(ctx context.Context, target contract.Target) string {
		time.Sleep(delay)
		if target.Port%2 == 0 {
			return "healthy"
		}
		return "unhealthy"
	}
	services := testServices(8)
	started := time.Now()
	healths := CheckAll(context.Background(), services)
	// Eight checks one after another would take 2.4 s.
	if elapsed := time.Since(started); elapsed > 4*delay {
		t.Fatalf("checks ran one after another: %v", elapsed)
	}
	if len(healths) != len(services) {
		t.Fatalf("health count = %d", len(healths))
	}
	for _, service := range services {
		want := "unhealthy"
		if service.Target.Port%2 == 0 {
			want = "healthy"
		}
		if healths[service.ServiceID] != want {
			t.Fatalf("%s = %q, want %q", service.ServiceID, healths[service.ServiceID], want)
		}
	}
}

func TestCheckAllBoundsParallelChecks(t *testing.T) {
	old := checkOne
	defer func() { checkOne = old }()
	var running, peak atomic.Int32
	checkOne = func(ctx context.Context, target contract.Target) string {
		n := running.Add(1)
		defer running.Add(-1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		return "healthy"
	}
	if healths := CheckAll(context.Background(), testServices(3*maxParallel)); len(healths) != 3*maxParallel {
		t.Fatalf("health count = %d", len(healths))
	}
	if p := peak.Load(); p > maxParallel || p < 2 {
		t.Fatalf("peak parallel checks = %d, want 2..%d", p, maxParallel)
	}
}

func TestResolveAllKeepsOnlyResolvedTargets(t *testing.T) {
	old := resolveOne
	defer func() { resolveOne = old }()
	resolveOne = func(ctx context.Context, target contract.Target) (string, error) {
		time.Sleep(200 * time.Millisecond)
		if target.Port%2 == 1 {
			return "", errors.New("target name cannot be resolved")
		}
		return fmt.Sprintf("10.0.0.1:%d", target.Port), nil
	}
	services := testServices(6)
	started := time.Now()
	addresses := ResolveAll(context.Background(), services)
	if elapsed := time.Since(started); elapsed > 800*time.Millisecond {
		t.Fatalf("lookups ran one after another: %v", elapsed)
	}
	if len(addresses) != 3 {
		t.Fatalf("addresses = %#v", addresses)
	}
	for _, service := range services {
		address, ok := addresses[service.ServiceID]
		if ok != (service.Target.Port%2 == 0) || ok && address != fmt.Sprintf("10.0.0.1:%d", service.Target.Port) {
			t.Fatalf("%s: address %q, present %v", service.ServiceID, address, ok)
		}
	}
}

func TestCheckAllWithoutServices(t *testing.T) {
	if healths := CheckAll(context.Background(), nil); len(healths) != 0 {
		t.Fatalf("healths = %#v", healths)
	}
}
