package control

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestClientBanSettingsAreNormalizedAndChecked(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	got, err := db.ClientBans(ctx)
	if err != nil || got.Minutes != 60 || got.Exempt == nil || len(got.Exempt) != 0 {
		t.Fatalf("defaults = %+v, %v", got, err)
	}

	// Addresses and ranges come back in one form: ranges by their network,
	// single addresses bare, IPv4 written as IPv6 as IPv4, once each.
	saved, err := db.SetClientBans(ctx, ClientBanSettings{Minutes: 1440, Exempt: []string{
		" 203.0.113.9/24 ", "203.0.113.0/24", "198.51.100.7", "198.51.100.7/32", "::ffff:198.51.100.7",
		"2001:DB8::1", "2001:db8:0:0::1/128", "2001:db8:aa::7/32", "::ffff:192.0.2.0/120", "::ffff:0:0/80", "0.0.0.0/0",
	}}, time.Now())
	want := []string{"203.0.113.0/24", "198.51.100.7", "2001:db8::1", "2001:db8::/32", "192.0.2.0/24", "::/80", "0.0.0.0/0"}
	if err != nil || saved.Minutes != 1440 || !slices.Equal(saved.Exempt, want) {
		t.Fatalf("saved = %+v, %v", saved, err)
	}
	if again, err := db.ClientBans(ctx); err != nil || again.Minutes != 1440 || !slices.Equal(again.Exempt, want) {
		t.Fatalf("stored = %+v, %v", again, err)
	}
	if renormalized, ok := saved.Normalize(); !ok || !slices.Equal(renormalized.Exempt, want) {
		t.Fatalf("not stable: %+v", renormalized)
	}

	fifty := make([]string, 50)
	for i := range fifty {
		fifty[i] = fmt.Sprint("10.0.0.", i)
	}
	for _, c := range []struct {
		name    string
		minutes int
		exempt  []string
		ok      bool
	}{
		{"one minute", 1, nil, true},
		{"a week", 10080, nil, true},
		{"fifty entries", 60, fifty, true},
		{"no minutes", 0, nil, false},
		{"negative", -1, nil, false},
		{"over a week", 10081, nil, false},
		{"fifty-one entries", 60, append(slices.Clone(fifty), "10.1.0.1"), false},
		{"fifty-one entries before duplicates go", 60, append(slices.Clone(fifty[:1]), fifty...), false},
		{"empty entry", 60, []string{""}, false},
		{"blank entry", 60, []string{"  "}, false},
		{"a name", 60, []string{"example.com"}, false},
		{"short address", 60, []string{"203.0.113"}, false},
		{"long length", 60, []string{"203.0.113.0/33"}, false},
		{"long IPv6 length", 60, []string{"2001:db8::/129"}, false},
		{"negative length", 60, []string{"203.0.113.0/-1"}, false},
		{"two lengths", 60, []string{"203.0.113.0/24/8"}, false},
		{"a zone", 60, []string{"fe80::1%eth0"}, false},
		{"a zone in a range", 60, []string{"fe80::%eth0/64"}, false},
		{"too long", 60, []string{strings.Repeat("1", 65)}, false},
		{"a good entry and a bad one", 60, []string{"203.0.113.0/24", "bad"}, false},
	} {
		_, err := db.SetClientBans(ctx, ClientBanSettings{Minutes: c.minutes, Exempt: c.exempt}, time.Now())
		if (err == nil) != c.ok || err != nil && !errors.Is(err, ErrInvalidClientBans) {
			t.Errorf("%s: %v", c.name, err)
		}
	}
	// A refused save leaves what was saved.
	if again, err := db.ClientBans(ctx); err != nil || !slices.Equal(again.Exempt, fifty) || again.Minutes != 60 {
		t.Fatalf("after refusals = %+v, %v", again, err)
	}

	// Settings saved before the checks changed, or damaged, are the defaults.
	for _, raw := range []string{`{"minutes":0,"exempt":[]}`, `{"minutes":60,"exempt":["nonsense"]}`, `[`, `{"minutes":"x"}`} {
		if _, err := db.SQL().ExecContext(ctx, "UPDATE outlet_settings SET value = ? WHERE key = 'client_bans'", raw); err != nil {
			t.Fatal(err)
		}
		if got, err := db.ClientBans(ctx); err != nil || got.Minutes != 60 || len(got.Exempt) != 0 {
			t.Errorf("%s = %+v, %v", raw, got, err)
		}
	}
}
