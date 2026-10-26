package outlet

import (
	"cmp"
	"context"
	"hash/fnv"
	"net/http"
	"slices"
	"sync/atomic"
	"time"

	"github.com/lawnect/meerkat/internal/control"
)

// backend is a service a public address can send a request to now: its
// tunnel is connected and the app answers at the tunnel port.
type backend struct {
	serviceID string
	address   string
}

// suspectFor is how long a service that just failed a request is passed
// over while another service of the address is live: its tunnel can keep
// accepting connections for a while after its Connector is gone.
const suspectFor = 10 * time.Second

// liveBackends keeps the services of plans that can take a request, in the
// plans' failover order. A service of a shared address must also be healthy
// by its Connector's own check, as the console shows it, and is passed over
// for a while after a failed request when another one is live.
func (s *Store) liveBackends(plans []control.HTTPRoutePlan) []backend {
	now := s.now()
	s.mu.RLock()
	live := make([]backend, 0, len(plans))
	for _, plan := range plans {
		record, ok := s.managed[plan.ConnectorID]
		address := record.probeFor(plan.ServiceID)
		if !ok || !record.Enrolled || managedConnectorStatus(record, now) != "connected" || record.ServiceStatuses[plan.ServiceID] != "connected" || !isLoopbackAddress(address) {
			continue
		}
		if len(plans) > 1 && managedServiceStatus(record, plan.ServiceID, now) != "connected" {
			continue
		}
		live = append(live, backend{serviceID: plan.ServiceID, address: address})
	}
	s.mu.RUnlock()
	if len(live) < 2 {
		return live
	}
	trusted := live[:0:0]
	for _, b := range live {
		if since, ok := s.suspect.Load(b.serviceID); !ok || now.Sub(since.(time.Time)) >= suspectFor {
			trusted = append(trusted, b)
		}
	}
	if len(trusted) == 0 {
		return live
	}
	return trusted
}

// failed and answered keep the passive health of a service up to date.
func (s *Store) failed(serviceID string)   { s.suspect.Store(serviceID, s.now()) }
func (s *Store) answered(serviceID string) { s.suspect.Delete(serviceID) }

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
// consumed. A request never written to a connection reached nobody, so any
// method goes on; once written, only methods safe to repeat do, a timeout
// included: the service may have had the request, but repeating it is safe.
func retryable(r *http.Request, err error, wrote bool) bool {
	if r.ContentLength != 0 || len(r.TransferEncoding) != 0 || r.Context().Err() != nil {
		return false
	}
	if !wrote {
		return true
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	return false
}

// nextBackend picks the service a retry goes to, from the routing and health
// of now, skipping the ones tried.
func (s *Store) nextBackend(host, client string, tried map[string]bool) (backend, bool) {
	routing, err := s.control.Routing(context.Background())
	if err != nil {
		return backend{}, false
	}
	plans, policy := routing.Routes(host)
	for _, candidate := range s.orderBackends(host, client, s.liveBackends(plans), policy) {
		if !tried[candidate.serviceID] {
			return candidate, true
		}
	}
	return backend{}, false
}
