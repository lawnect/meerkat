package control

import (
	"context"
	"encoding/json"
	"time"
)

const settingHSTS = "hsts"

// HSTS says whether public addresses send Strict-Transport-Security. Every
// public address is served over HTTPS (port 80 only redirects), so it is on
// unless an administrator turns it off. The management host always sends it.
type HSTS struct {
	Enabled bool `json:"enabled"`
}

func DefaultHSTS() HSTS { return HSTS{Enabled: true} }

// HSTS returns the stored setting, or the default.
func (d *Database) HSTS(ctx context.Context) (HSTS, error) {
	raw, err := getSetting(ctx, d.db, settingHSTS)
	if err != nil || raw == "" {
		return DefaultHSTS(), err
	}
	var h HSTS
	if err := json.Unmarshal([]byte(raw), &h); err != nil {
		return DefaultHSTS(), nil
	}
	return h, nil
}

func (d *Database) SetHSTS(ctx context.Context, h HSTS, now time.Time) (HSTS, error) {
	raw, err := json.Marshal(h)
	if err != nil {
		return h, err
	}
	return h, putSetting(ctx, d.db, settingHSTS, string(raw), now)
}
