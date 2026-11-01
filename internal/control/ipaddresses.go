package control

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"
	"unicode"
)

var (
	ErrInvalidIPAddress  = errors.New("invalid IP address entry")
	ErrIPAddressNotFound = errors.New("IP address entry not found")
	ErrIPAddressTaken    = errors.New("IP address entry already exists")
	// ErrIPTagInUse refuses a change that would leave a tag an access policy
	// or the admin console limit names with no address.
	ErrIPTagInUse = errors.New("IP tag in use")
	// ErrStoredIPAddresses comes with an empty address book when the stored
	// one can't be read; tags then match no address.
	ErrStoredIPAddresses = errors.New("stored IP addresses are damaged")
)

const (
	settingIPAddresses = "ip_addresses"

	MaxIPAddresses = 500
	// MaxIPTags is how many tags one address carries.
	MaxIPTags   = 10
	maxIPTagLen = 32
	maxIPNote   = 100
	// MaxScopeTags and MaxScopeRanges bound what one access policy or the
	// admin console limit names.
	MaxScopeTags   = 20
	MaxScopeRanges = 50
)

// IPAddress is an entry of the address book: an address or CIDR range, a
// note saying whose it is, and tags that access policies and the admin
// console limit name to mean every address carrying them.
type IPAddress struct {
	ID    string   `json:"id"`
	Range string   `json:"range"`
	Note  string   `json:"note"`
	Tags  []string `json:"tags"`
}

type IPAddresses struct {
	Entries []IPAddress `json:"entries"`
}

// NormalizeTags trims tags and keeps each once, comparing case-insensitively
// and keeping the first spelling. A tag is 1 to 32 characters without
// commas or control characters, and can't be read as an address or range,
// so a value naming either is never ambiguous.
func NormalizeTags(tags []string, max int) ([]string, bool) {
	if len(tags) > max {
		return nil, false
	}
	out := make([]string, 0, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" || len([]rune(tag)) > maxIPTagLen || strings.ContainsRune(tag, ',') || strings.IndexFunc(tag, unicode.IsControl) >= 0 {
			return nil, false
		}
		if _, isRange := normalizeRange(tag); isRange {
			return nil, false
		}
		if !slices.ContainsFunc(out, func(seen string) bool { return strings.EqualFold(seen, tag) }) {
			out = append(out, tag)
		}
	}
	return out, true
}

// Normalize checks an entry and puts its range in normalizeRange's form.
func (e IPAddress) Normalize() (IPAddress, bool) {
	r, ok := normalizeRange(strings.TrimSpace(e.Range))
	if !ok {
		return e, false
	}
	e.Range = r
	e.Note = strings.TrimSpace(e.Note)
	if len([]rune(e.Note)) > maxIPNote || strings.IndexFunc(e.Note, unicode.IsControl) >= 0 {
		return e, false
	}
	tags, ok := NormalizeTags(e.Tags, MaxIPTags)
	if !ok {
		return e, false
	}
	e.Tags = tags
	return e, true
}

// Tagged returns the ranges of the entries carrying tag.
func (a IPAddresses) Tagged(tag string) []string {
	var ranges []string
	for _, e := range a.Entries {
		if slices.ContainsFunc(e.Tags, func(t string) bool { return strings.EqualFold(t, tag) }) {
			ranges = append(ranges, e.Range)
		}
	}
	return ranges
}

// Spelling returns tag as the address book spells it, and whether any
// entry carries it.
func (a IPAddresses) Spelling(tag string) (string, bool) {
	for _, e := range a.Entries {
		for _, t := range e.Tags {
			if strings.EqualFold(t, tag) {
				return t, true
			}
		}
	}
	return "", false
}

// Without returns the address book without the entry with ID id.
func (a IPAddresses) Without(id string) IPAddresses {
	out := IPAddresses{Entries: make([]IPAddress, 0, len(a.Entries))}
	for _, e := range a.Entries {
		if e.ID != id {
			out.Entries = append(out.Entries, e)
		}
	}
	return out
}

// With returns the address book with e in place of the entry with its ID,
// or added.
func (a IPAddresses) With(e IPAddress) IPAddresses {
	out := a.Without(e.ID)
	if i := slices.IndexFunc(a.Entries, func(old IPAddress) bool { return old.ID == e.ID }); i >= 0 {
		out.Entries = slices.Insert(out.Entries, i, e)
	} else {
		out.Entries = append(out.Entries, e)
	}
	return out
}

func readIPAddresses(ctx context.Context, q conn) (IPAddresses, error) {
	raw, err := getSetting(ctx, q, settingIPAddresses)
	if err != nil || raw == "" {
		return IPAddresses{Entries: []IPAddress{}}, err
	}
	var stored IPAddresses
	if json.Unmarshal([]byte(raw), &stored) != nil || stored.Entries == nil || len(stored.Entries) > MaxIPAddresses {
		return IPAddresses{Entries: []IPAddress{}}, ErrStoredIPAddresses
	}
	seen := map[string]bool{}
	for i, e := range stored.Entries {
		normalized, ok := e.Normalize()
		if !ok || !validRuleID(e.ID) || seen[e.ID] {
			return IPAddresses{Entries: []IPAddress{}}, ErrStoredIPAddresses
		}
		seen[e.ID] = true
		stored.Entries[i] = normalized
	}
	return stored, nil
}

// IPAddresses returns the address book: empty, with ErrStoredIPAddresses,
// when the stored one can't be read.
func (d *Database) IPAddresses(ctx context.Context) (IPAddresses, error) {
	return readIPAddresses(ctx, d.db)
}

// usedTags are the tags the access policies and the admin console limit
// name, lowercased. Unreadable settings name none.
func usedTags(ctx context.Context, q conn) (map[string]bool, error) {
	used := map[string]bool{}
	raw, err := getSetting(ctx, q, settingAccessPolicies)
	if err != nil {
		return nil, err
	}
	var policies AccessPolicies
	if raw != "" && json.Unmarshal([]byte(raw), &policies) == nil {
		for _, policy := range policies.Policies {
			for _, tag := range policy.IPTags {
				used[strings.ToLower(tag)] = true
			}
		}
	}
	if raw, err = getSetting(ctx, q, settingAdminAccess); err != nil {
		return nil, err
	}
	var admin AdminAccess
	if raw != "" && json.Unmarshal([]byte(raw), &admin) == nil {
		for _, tag := range admin.AllowedTags {
			used[strings.ToLower(tag)] = true
		}
	}
	return used, nil
}

// changeIPAddresses applies change to the address book in a transaction,
// refusing a result that leaves a tag in use with no address.
func (d *Database) changeIPAddresses(ctx context.Context, now time.Time, change func(IPAddresses) (IPAddresses, error)) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	current, err := readIPAddresses(ctx, tx)
	if err != nil && !errors.Is(err, ErrStoredIPAddresses) {
		return err
	}
	next, err := change(current)
	if err != nil {
		return err
	}
	used, err := usedTags(ctx, tx)
	if err != nil {
		return err
	}
	for tag := range used {
		if _, ok := current.Spelling(tag); ok {
			if _, still := next.Spelling(tag); !still {
				return ErrIPTagInUse
			}
		}
	}
	raw, err := json.Marshal(next)
	if err != nil {
		return err
	}
	if err := putSetting(ctx, tx, settingIPAddresses, string(raw), now); err != nil {
		return err
	}
	return tx.Commit()
}

func (d *Database) CreateIPAddress(ctx context.Context, e IPAddress, now time.Time) (IPAddress, error) {
	e, ok := e.Normalize()
	if !ok {
		return e, ErrInvalidIPAddress
	}
	id, err := newRuleID()
	if err != nil {
		return e, err
	}
	e.ID = id
	err = d.changeIPAddresses(ctx, now, func(book IPAddresses) (IPAddresses, error) {
		if len(book.Entries) >= MaxIPAddresses {
			return book, ErrInvalidIPAddress
		}
		if slices.ContainsFunc(book.Entries, func(old IPAddress) bool { return old.Range == e.Range }) {
			return book, ErrIPAddressTaken
		}
		return book.With(e), nil
	})
	return e, err
}

func (d *Database) UpdateIPAddress(ctx context.Context, id string, e IPAddress, now time.Time) (IPAddress, error) {
	e, ok := e.Normalize()
	if !ok {
		return e, ErrInvalidIPAddress
	}
	e.ID = id
	err := d.changeIPAddresses(ctx, now, func(book IPAddresses) (IPAddresses, error) {
		if !slices.ContainsFunc(book.Entries, func(old IPAddress) bool { return old.ID == id }) {
			return book, ErrIPAddressNotFound
		}
		if slices.ContainsFunc(book.Entries, func(old IPAddress) bool { return old.ID != id && old.Range == e.Range }) {
			return book, ErrIPAddressTaken
		}
		return book.With(e), nil
	})
	return e, err
}

func (d *Database) DeleteIPAddress(ctx context.Context, id string, now time.Time) (IPAddress, error) {
	var deleted IPAddress
	err := d.changeIPAddresses(ctx, now, func(book IPAddresses) (IPAddresses, error) {
		i := slices.IndexFunc(book.Entries, func(old IPAddress) bool { return old.ID == id })
		if i < 0 {
			return book, ErrIPAddressNotFound
		}
		deleted = book.Entries[i]
		return book.Without(id), nil
	})
	return deleted, err
}

// resolveTags puts tags in the address book's spelling, failing when one
// names no address.
func resolveTags(book IPAddresses, tags []string) ([]string, bool) {
	out := make([]string, 0, len(tags))
	for _, tag := range tags {
		spelled, ok := book.Spelling(tag)
		if !ok {
			return nil, false
		}
		out = append(out, spelled)
	}
	return out, true
}
