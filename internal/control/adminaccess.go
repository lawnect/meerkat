package control

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

var (
	ErrInvalidAdminAccess = errors.New("invalid admin access settings")
	// ErrDamagedAdminAccess comes with ClosedAdminAccess when the stored
	// settings can't be read, such as ones a later version wrote before a
	// rollback: an access control fails closed, and the local admin port and
	// MEERKAT_ADMIN_IP_LIMIT=false still get in.
	ErrDamagedAdminAccess = errors.New("stored admin access settings are damaged")
)

const (
	settingAdminAccess = "admin_access"

	MaxAdminAccessAllowed = 50
)

// AdminAccess limits the management console on public addresses. When
// Restricted, only clients in Allowed reach it there; an empty list closes
// it to the internet, leaving the local admin port over SSH. The Connector
// API and access protection's sign-in stay open to everyone.
type AdminAccess struct {
	Restricted bool     `json:"restricted"`
	Allowed    []string `json:"allowed"`
}

func DefaultAdminAccess() AdminAccess { return AdminAccess{Allowed: []string{}} }

// ClosedAdminAccess closes the console on public addresses.
func ClosedAdminAccess() AdminAccess { return AdminAccess{Restricted: true, Allowed: []string{}} }

// Normalize reports whether the settings are valid and puts the ranges in
// normalizeRange's form, once each, in the order given.
func (a AdminAccess) Normalize() (AdminAccess, bool) {
	if len(a.Allowed) > MaxAdminAccessAllowed {
		return a, false
	}
	allowed, ok := normalizeRanges(a.Allowed)
	if !ok {
		return a, false
	}
	a.Allowed = allowed
	return a, true
}

// AdminAccess returns the stored settings, the defaults when none are
// stored, or ClosedAdminAccess with ErrDamagedAdminAccess when the stored
// ones can't be read.
func (d *Database) AdminAccess(ctx context.Context) (AdminAccess, error) {
	raw, err := getSetting(ctx, d.db, settingAdminAccess)
	if err != nil || raw == "" {
		return DefaultAdminAccess(), err
	}
	var a AdminAccess
	if json.Unmarshal([]byte(raw), &a) != nil {
		return ClosedAdminAccess(), ErrDamagedAdminAccess
	}
	if a, ok := a.Normalize(); ok {
		return a, nil
	}
	return ClosedAdminAccess(), ErrDamagedAdminAccess
}

func (d *Database) SetAdminAccess(ctx context.Context, a AdminAccess, now time.Time) (AdminAccess, error) {
	a, ok := a.Normalize()
	if !ok {
		return a, ErrInvalidAdminAccess
	}
	raw, err := json.Marshal(a)
	if err != nil {
		return a, err
	}
	return a, putSetting(ctx, d.db, settingAdminAccess, string(raw), now)
}

// AdminIPLimitLifted reports whether the environment lifted the limit
// (MEERKAT_ADMIN_IP_LIMIT=false), for an administrator locked out with no
// SSH access.
func (d *Database) AdminIPLimitLifted() bool { return d.environment().AdminIPLimitLifted }
