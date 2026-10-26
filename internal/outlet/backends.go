package outlet

import (
	"cmp"
	"errors"
	"hash/fnv"
	"net"
	"net/http"
	"slices"
	"sync/atomic"

	"github.com/lawnect/meerkat/internal/control"
)

// backend is a service a public address can send a request to now: its
// tunnel is connected and the app answers at the tunnel port.
type backend struct {
	serviceID string
	address   string
}

// liveBackends keeps the services of plans that can take a request, in the
// plans' failover order.
func (s *Store) liveBackends(plans []control.HTTPRoutePlan) []backend {
	now := s.now()
	s.mu.RLock()
	defer s.mu.RUnlock()
	live := make([]backend, 0, len(plans))
	for _, plan := range plans {
		record, ok := s.managed[plan.ConnectorID]
		address := record.probeFor(plan.ServiceID)
		if ok && record.Enrolled && managedConnectorStatus(record, now) == "connected" && record.ServiceStatuses[plan.ServiceID] == "connected" && isLoopbackAddress(address) {
			live = append(live, backend{serviceID: plan.ServiceID, address: address})
		}
	}
	return live
}

// orderBackends puts the service to try first at the front; the rest are
// where a request that can't reach it goes next. Failover keeps priority
// order. Balance takes turns, or with sticky ranks the services for each
// client address by rendezvous hashing, so a client stays on its service
// while that one is up and only its clients move when it isn't.
func (s *Store) orderBackends(host, client string, live []backend, policy control.AddressPolicy) []backend {
	if len(live) < 2 || policy.Mode != control.AddressBalance {
		return live
	}
	if policy.Sticky && client != "" {
		type ranked struct {
			backend
			score uint64
		}
		ranks := make([]ranked, len(live))
		for i, b := range live {
			hash := fnv.New64a()
			_, _ = hash.Write([]byte(client + "\x00" + b.serviceID))
			ranks[i] = ranked{b, hash.Sum64()}
		}
		slices.SortStableFunc(ranks, func(a, b ranked) int { return cmp.Compare(b.score, a.score) })
		ordered := make([]backend, len(ranks))
		for i, rank := range ranks {
			ordered[i] = rank.backend
		}
		return ordered
	}
	counter, _ := s.balance.LoadOrStore(host, new(atomic.Uint64))
	start := int((counter.(*atomic.Uint64).Add(1) - 1) % uint64(len(live)))
	ordered := make([]backend, 0, len(live))
	return append(append(ordered, live[start:]...), live[:start]...)
}

// retryable reports whether a request that failed to reach one service can
// go to the next. It must have no body, which the failed attempt may have
// consumed. A connection that never opened carried nothing, so any method
// goes on; after that, only methods that are safe to repeat do, and never
// after a timeout, which means the service had the request.
func retryable(r *http.Request, err error) bool {
	if r.ContentLength != 0 || len(r.TransferEncoding) != 0 || r.Context().Err() != nil {
		return false
	}
	var dial *net.OpError
	if errors.As(err, &dial) && dial.Op == "dial" {
		return true
	}
	var timeout net.Error
	if errors.As(err, &timeout) && timeout.Timeout() {
		return false
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	return false
}
