package outlet

import (
	"context"
	"time"
)

// tunnelEvents records reconnects and tunnel process restarts as heartbeats
// arrive. It is guarded by Store.mu, which the heartbeat handler holds.
type tunnelEvents struct{}

func (s *Store) unstableAlerts(ctx context.Context, env alertEnv, now time.Time) error {
	return nil
}
