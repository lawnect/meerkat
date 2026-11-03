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
	MaxTimeoutHosts     = 50
	maxUploadMB         = 100 << 10 // 100 GiB
	maxConcurrent       = 1_000_000
	minIdleTimeout      = 5
	maxIdleTimeout      = 3600
	maxMinUploadPerSecK = 100_000

	// DefaultResponseTimeoutSeconds is how long a service gets to start
	// answering, like nginx's proxy_read_timeout.
	DefaultResponseTimeoutSeconds = 60
	minResponseTimeout            = 5
	// maxResponseTimeout is an hour, like the idle limits of Caddy in front.
	maxResponseTimeout = 3600

	// DefaultRequestTimeoutMinutes is how long a proxied request may take
	// from when it comes to the end of its response: an hour, past any
	// answer a person waits for, LLM streams included.
	DefaultRequestTimeoutMinutes = 60
	minRequestTimeout            = 1
	// maxRequestTimeout is a day; longer transfers lift the limit.
	maxRequestTimeout = 1440
)

// HostUpload sets the upload size for one public hostname, overriding
// ConnectionLimits.MaxUploadMB. Zero lifts the limit for that host.
type HostUpload struct {
	Host        string `json:"host"`
	MaxUploadMB int    `json:"max_upload_mb"`
}

// HostResponseTimeout sets how long the services of one public hostname get
// to start answering, overriding ConnectionLimits.ResponseTimeoutSeconds.
// Zero lifts the limit for that host.
type HostResponseTimeout struct {
	Host    string `json:"host"`
	Seconds int    `json:"response_timeout_seconds"`
}

// HostRequestTimeout sets how long a request to one public hostname may
// take, overriding ConnectionLimits.RequestTimeoutMinutes. Zero lifts the
// limit for that host.
type HostRequestTimeout struct {
	Host    string `json:"host"`
	Minutes int    `json:"request_timeout_minutes"`
}

// ConnectionLimits bound what one request or one client can hold on the
// public listener: request body size, requests in flight, how long an upload
// or download may stall, how long a service may take to answer, and how long
// a request may take in all. Zero turns a limit off.
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
	// ResponseTimeoutSeconds is how long a service gets to send its response
	// headers once the request is sent. The body may stream for longer.
	ResponseTimeoutSeconds int                   `json:"response_timeout_seconds"`
	ResponseTimeouts       []HostResponseTimeout `json:"response_timeouts"`
	// RequestTimeoutMinutes is how long a proxied request may take, from
	// when it comes to the end of its response; a request still going then
	// is ended. A connection upgraded off HTTP (WebSocket) isn't limited.
	RequestTimeoutMinutes int                  `json:"request_timeout_minutes"`
	RequestTimeouts       []HostRequestTimeout `json:"request_timeouts"`
}

// DefaultConnectionLimits allows large uploads and busy offices behind one
// address, and stops slow clients from holding connections indefinitely. A
// request in flight costs the Outlet about 170 KB (measured with 10,000 held
// uploads), so the total keeps a flood of them under about 700 MB.
func DefaultConnectionLimits() ConnectionLimits {
	return ConnectionLimits{
		MaxUploadMB:            100,
		PerIPConcurrent:        512,
		TotalConcurrent:        4096,
		IdleTimeoutSeconds:     30,
		MinUploadKBPerSecond:   1,
		Hosts:                  []HostUpload{},
		ResponseTimeoutSeconds: DefaultResponseTimeoutSeconds,
		ResponseTimeouts:       []HostResponseTimeout{},
		RequestTimeoutMinutes:  DefaultRequestTimeoutMinutes,
		RequestTimeouts:        []HostRequestTimeout{},
	}
}

// Normalize lowercases hosts and reports whether the limits are within range.
func (c ConnectionLimits) Normalize() (ConnectionLimits, bool) {
	inRange := func(v, high int) bool { return v >= 0 && v <= high }
	offOrWithin := func(v, low, high int) bool { return v == 0 || v >= low && v <= high }
	if !inRange(c.MaxUploadMB, maxUploadMB) || !inRange(c.PerIPConcurrent, maxConcurrent) || !inRange(c.TotalConcurrent, maxConcurrent) ||
		!inRange(c.MinUploadKBPerSecond, maxMinUploadPerSecK) || len(c.Hosts) > MaxUploadHosts ||
		!offOrWithin(c.IdleTimeoutSeconds, minIdleTimeout, maxIdleTimeout) ||
		!offOrWithin(c.ResponseTimeoutSeconds, minResponseTimeout, maxResponseTimeout) || len(c.ResponseTimeouts) > MaxTimeoutHosts ||
		!offOrWithin(c.RequestTimeoutMinutes, minRequestTimeout, maxRequestTimeout) || len(c.RequestTimeouts) > MaxTimeoutHosts {
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
	timeouts := make([]HostResponseTimeout, 0, len(c.ResponseTimeouts))
	seen = map[string]bool{}
	for _, h := range c.ResponseTimeouts {
		h.Host = strings.ToLower(strings.TrimSpace(h.Host))
		if !validation.Hostname(h.Host) || !offOrWithin(h.Seconds, minResponseTimeout, maxResponseTimeout) || seen[h.Host] {
			return c, false
		}
		seen[h.Host] = true
		timeouts = append(timeouts, h)
	}
	durations := make([]HostRequestTimeout, 0, len(c.RequestTimeouts))
	seen = map[string]bool{}
	for _, h := range c.RequestTimeouts {
		h.Host = strings.ToLower(strings.TrimSpace(h.Host))
		if !validation.Hostname(h.Host) || !offOrWithin(h.Minutes, minRequestTimeout, maxRequestTimeout) || seen[h.Host] {
			return c, false
		}
		seen[h.Host] = true
		durations = append(durations, h)
	}
	c.Hosts = hosts
	c.ResponseTimeouts = timeouts
	c.RequestTimeouts = durations
	return c, true
}

// ConnectionLimits returns the stored limits, or the defaults. Limits stored
// before a limit existed get its default.
func (d *Database) ConnectionLimits(ctx context.Context) (ConnectionLimits, error) {
	raw, err := getSetting(ctx, d.db, settingConnectionLimits)
	if err != nil || raw == "" {
		return DefaultConnectionLimits(), err
	}
	limits := DefaultConnectionLimits()
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
