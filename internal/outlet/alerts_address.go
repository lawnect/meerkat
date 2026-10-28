package outlet

import (
	"context"
	"time"
)

// addressAlertState is the address check's, kept between rounds.
type addressAlertState struct{}

func (s *Store) routeAlerts(ctx context.Context, env alertEnv, now time.Time) error {
	if !env.rules.RouteError.Enabled {
		return s.syncAlerts(ctx, env, "route_error", nil, nil, nil, now)
	}
	plans, err := s.control.HTTPRoutePlans(ctx)
	if err != nil {
		return err
	}
	var active []condition
	healthy := map[string]bool{}
	for _, plan := range plans {
		key := "route_error:" + plan.ID
		if plan.LastError == "" {
			healthy[key] = true
			continue
		}
		active = append(active, condition{key, plan.Hostname, routeErrorNotice(plan.Hostname, plan.ConnectorID, plan.LastError)})
	}
	return s.syncAlerts(ctx, env, "route_error", active, healthy, nil, now)
}
