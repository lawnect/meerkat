package outlet

import (
	"context"
	"time"
)

// hostAlertState is the Outlet restart, disk and memory rules', kept
// between rounds.
type hostAlertState struct{}

func (s *Store) restartAlerts(ctx context.Context, env alertEnv, now time.Time) error {
	return nil
}

func (s *Store) diskAlerts(ctx context.Context, env alertEnv, now time.Time) error {
	return nil
}

func (s *Store) memoryAlerts(ctx context.Context, env alertEnv, now time.Time) error {
	return nil
}
