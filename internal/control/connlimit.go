package control

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/lawnect/meerkat/internal/validation"
)

var ErrInvalidConnectionLimits = errors.New("invalid connection limits")

const (
	settingConnectionLimits = "connection_limits"

	MaxUploadHosts      = 50
	maxUploadMB         = 100 << 10 // 100 GiB
	maxConcurrent       = 1_000_000
	minIdleTimeout      = 5
	maxIdleTimeout      = 3600
	maxMinUploadPerSecK = 100_000
)

// HostUpload sets the upload size for one public hostname, overriding
// ConnectionLimits.MaxUploadMB. Zero lifts the limit for that host.
type HostUpload struct {
	Host        string `json:"host"`
	MaxUploadMB int    `json:"max_upload_mb"`
}

// ConnectionLimits bound what one request or one client can hold on the
// public listener: request body size, requests in flight, and how long an
// upload or download may stall. Zero turns a limit off.
type ConnectionLimits struct {
	// MaxUploadMB caps a proxied request body, in MiB.
	MaxUploadMB int `json:"max_upload_mb"`
	// PerIPConcurrent caps requests in flight from one address (IPv6 by /56).
	PerIPConcurrent int `json:"per_ip_concurrent"`
	// TotalConcurrent caps proxied requests in flight across all clients.
	TotalConcurrent int `json:"total_concurrent"`
	// IdleTimeoutSeconds cuts a client whose upload or download makes no
	// progress for this long. Waiting for the application does not count.
	IdleTimeoutSeconds int `json:"idle_timeout_seconds"`
	// MinUploadKBPerSecond cuts an upload whose average speed since the
	// request began falls below this, after the first idle timeout.
	MinUploadKBPerSecond int          `json:"min_upload_kb_per_second"`
	Hosts                []HostUpload `json:"hosts"`
}

// DefaultConnectionLimits allows large uploads and busy offices behind one
// address, and stops slow clients from holding connections indefinitely. A
// request in flight costs the Outlet about 170 KB (measured with 10,000 held
// uploads), so the total keeps a flood of them under about 700 MB.
func DefaultConnectionLimits() ConnectionLimits {
	return ConnectionLimits{
		MaxUploadMB:          100,
		PerIPConcurrent:      512,
		TotalConcurrent:      4096,
		IdleTimeoutSeconds:   30,
		MinUploadKBPerSecond: 1,
		Hosts:                []HostUpload{},
	}
}

// Normalize lowercases hosts and reports whether the limits are within range.
func (c ConnectionLimits) Normalize() (ConnectionLimits, bool) {
	inRange := func(v, high int) bool { return v >= 0 && v <= high }
	if !inRange(c.MaxUploadMB, maxUploadMB) || !inRange(c.PerIPConcurrent, maxConcurrent) || !inRange(c.TotalConcurrent, maxConcurrent) ||
		!inRange(c.MinUploadKBPerSecond, maxMinUploadPerSecK) || len(c.Hosts) > MaxUploadHosts ||
		(c.IdleTimeoutSeconds != 0 && (c.IdleTimeoutSeconds < minIdleTimeout || c.IdleTimeoutSeconds > maxIdleTimeout)) {
		return c, false
	}
	hosts := make([]HostUpload, 0, len(c.Hosts))
	seen := map[string]bool{}
	for _, h := range c.Hosts {
		h.Host = strings.ToLower(strings.TrimSpace(h.Host))
		if !validation.Hostname(h.Host) || !inRange(h.MaxUploadMB, maxUploadMB) || seen[h.Host] {
			return c, false
		}
		seen[h.Host] = true
		hosts = append(hosts, h)
	}
	c.Hosts = hosts
	return c, true
}

// ConnectionLimits returns the stored limits, or the defaults.
func (d *Database) ConnectionLimits(ctx context.Context) (ConnectionLimits, error) {
	raw, err := getSetting(ctx, d.db, settingConnectionLimits)
	if err != nil || raw == "" {
		return DefaultConnectionLimits(), err
	}
	var limits ConnectionLimits
	if err := json.Unmarshal([]byte(raw), &limits); err != nil {
		return DefaultConnectionLimits(), nil
	}
	if limits, ok := limits.Normalize(); ok {
		return limits, nil
	}
	return DefaultConnectionLimits(), nil
}

func (d *Database) SetConnectionLimits(ctx context.Context, limits ConnectionLimits, now time.Time) (ConnectionLimits, error) {
	limits, ok := limits.Normalize()
	if !ok {
		return limits, ErrInvalidConnectionLimits
	}
	raw, err := json.Marshal(limits)
	if err != nil {
		return limits, err
	}
	return limits, putSetting(ctx, d.db, settingConnectionLimits, string(raw), now)
}
