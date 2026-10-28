package outlet

import (
	"context"
	"time"
)

// latencyAlertState is the latency rule's, kept between rounds.
type latencyAlertState struct{}

func (s *Store) latencyAlerts(ctx context.Context, env alertEnv, now time.Time) error {
	return nil
}
