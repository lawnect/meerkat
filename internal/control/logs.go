package control

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// The Outlet's logs, each in a file of its own: the requests access
// protection decided, and the public requests that failed or the Outlet
// refused.
const (
	LogAccess   = "access"
	LogFailures = "failures"
)

// ErrInvalidLogSettings reports settings outside the limits below.
var ErrInvalidLogSettings = errors.New("invalid log settings")

// LogSettings say whether a log keeps entries, the most its file may take
// (16 MiB to 4 GiB) and how many days entries stay (1 to 365).
type LogSettings struct {
	Enabled       bool `json:"enabled"`
	MaxMiB        int  `json:"max_mib"`
	RetentionDays int  `json:"retention_days"`
}

// DefaultLogSettings keep a log off, at 256 MiB and 30 days once on.
func DefaultLogSettings() LogSettings {
	return LogSettings{MaxMiB: 256, RetentionDays: 30}
}

func (s LogSettings) valid() bool {
	return s.MaxMiB >= 16 && s.MaxMiB <= 4096 && s.RetentionDays >= 1 && s.RetentionDays <= 365
}

// LogSettings returns a log's settings, the defaults until saved.
func (d *Database) LogSettings(ctx context.Context, name string) (LogSettings, error) {
	settings := DefaultLogSettings()
	raw, err := getSetting(ctx, d.db, "log_"+name)
	if err != nil || raw == "" {
		return settings, err
	}
	// Saved settings that can't be read, or that fail the checks, are the
	// defaults: off.
	if json.Unmarshal([]byte(raw), &settings) != nil || !settings.valid() {
		return DefaultLogSettings(), nil
	}
	return settings, nil
}

// SetLogSettings saves a log's settings.
func (d *Database) SetLogSettings(ctx context.Context, name string, settings LogSettings, now time.Time) (LogSettings, error) {
	if (name != LogAccess && name != LogFailures) || !settings.valid() {
		return settings, ErrInvalidLogSettings
	}
	raw, err := json.Marshal(settings)
	if err != nil {
		return settings, err
	}
	return settings, putSetting(ctx, d.db, "log_"+name, string(raw), now)
}
