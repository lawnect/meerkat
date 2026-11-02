package control

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestIPAddressBook(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Now()
	office, err := db.CreateIPAddress(ctx, IPAddress{Range: " 203.0.113.0/24 ", Note: " 본사 ", Tags: []string{"사무실", " VPN ", "vpn"}}, now)
	if err != nil || office.Range != "203.0.113.0/24" || office.Note != "본사" || !slices.Equal(office.Tags, []string{"사무실", "VPN"}) || office.ID == "" {
		t.Fatalf("create = %+v, %v", office, err)
	}
	home, err := db.CreateIPAddress(ctx, IPAddress{Range: "198.51.100.7/32", Note: "재택", Tags: []string{"vpn"}}, now)
	if err != nil || home.Range != "198.51.100.7" {
		t.Fatalf("create = %+v, %v", home, err)
	}
	if _, err := db.CreateIPAddress(ctx, IPAddress{Range: "203.0.113.9/24", Tags: []string{}}, now); !errors.Is(err, ErrIPAddressTaken) {
		t.Fatalf("same range twice: %v", err)
	}
	for _, bad := range []IPAddress{
		{Range: "not-an-ip", Tags: []string{}},
		{Range: "10.0.0.1", Tags: []string{"10.0.0.0/8"}},
		{Range: "10.0.0.1", Tags: []string{"a,b"}},
		{Range: "10.0.0.1", Tags: []string{""}},
		{Range: "10.0.0.1", Tags: []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11"}},
	} {
		if _, err := db.CreateIPAddress(ctx, bad, now); !errors.Is(err, ErrInvalidIPAddress) {
			t.Fatalf("%+v: %v", bad, err)
		}
	}
	book, err := db.IPAddresses(ctx)
	if err != nil || len(book.Entries) != 2 || !slices.Equal(book.Tagged("vpn"), []string{"203.0.113.0/24", "198.51.100.7"}) {
		t.Fatalf("book = %+v, %v", book, err)
	}

	// Tags in use are saved as the address book spells them, and must exist.
	saved, err := db.SetAdminAccess(ctx, AdminAccess{Restricted: true, Allowed: []string{}, AllowedTags: []string{"vpn"}}, now)
	if err != nil || !slices.Equal(saved.AllowedTags, []string{"VPN"}) {
		t.Fatalf("admin access = %+v, %v", saved, err)
	}
	if _, err := db.SetAdminAccess(ctx, AdminAccess{Restricted: true, Allowed: []string{}, AllowedTags: []string{"nobody"}}, now); !errors.Is(err, ErrInvalidAdminAccess) {
		t.Fatalf("unknown tag in the admin limit: %v", err)
	}
	policy := AccessPolicy{Name: "office", Enabled: true, Action: AccessBypass, Hosts: []string{"app.example.com"}, Conditions: []AccessCondition{fromIPs("사무실", "192.0.2.1/32")}}
	policies, err := db.SetAccessPolicies(ctx, AccessPolicies{Policies: []AccessPolicy{policy}}, now)
	if err != nil || !slices.Equal(policies.Policies[0].Conditions[0].Values, []string{"사무실", "192.0.2.1"}) {
		t.Fatalf("policies = %+v, %v", policies, err)
	}
	policy.Conditions = []AccessCondition{fromIPs("없는 태그")}
	if _, err := db.SetAccessPolicies(ctx, AccessPolicies{Policies: []AccessPolicy{policy}}, now); !errors.Is(err, ErrInvalidAccessPolicies) {
		t.Fatalf("unknown tag in a policy: %v", err)
	}

	// A tag in use keeps at least one address.
	if _, err := db.DeleteIPAddress(ctx, office.ID, now); !errors.Is(err, ErrIPTagInUse) {
		t.Fatalf("deleting the only 사무실 entry: %v", err)
	}
	if _, err := db.UpdateIPAddress(ctx, office.ID, IPAddress{Range: office.Range, Tags: []string{"VPN"}}, now); !errors.Is(err, ErrIPTagInUse) {
		t.Fatalf("untagging the only 사무실 entry: %v", err)
	}
	// VPN is also on the home entry, so the office one can drop it.
	if updated, err := db.UpdateIPAddress(ctx, office.ID, IPAddress{Range: "203.0.113.0/25", Note: "본사 1층", Tags: []string{"사무실"}}, now); err != nil || updated.Range != "203.0.113.0/25" {
		t.Fatalf("update = %+v, %v", updated, err)
	}
	if _, err := db.DeleteIPAddress(ctx, home.ID, now); !errors.Is(err, ErrIPTagInUse) {
		t.Fatalf("deleting the last VPN entry: %v", err)
	}
	if _, err := db.DeleteIPAddress(ctx, "missing", now); !errors.Is(err, ErrIPAddressNotFound) {
		t.Fatalf("missing entry: %v", err)
	}
	// Once nothing names it, the tag can go.
	if _, err := db.SetAdminAccess(ctx, AdminAccess{Allowed: []string{}, AllowedTags: []string{}}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DeleteIPAddress(ctx, home.ID, now); err != nil {
		t.Fatalf("delete after the limit let go of VPN: %v", err)
	}
}

func TestIPTagsThatLookLikeAddressesAreRefused(t *testing.T) {
	for _, tag := range []string{"cafe:babe", "a:b", "1.2.3.4/33", "999.1.1.1", "10.0.0.1/", "::ffff:1.2.3.4/90", "203.0.113.7"} {
		if _, ok := NormalizeTags([]string{tag}, MaxIPTags); ok {
			t.Fatalf("%q accepted as a tag", tag)
		}
	}
	for _, tag := range []string{"office", "사무실", "2024", "vpn-1", "본사 3층", "a.b"} {
		if _, ok := NormalizeTags([]string{tag}, MaxIPTags); !ok {
			t.Fatalf("%q refused as a tag", tag)
		}
	}
}

// A new entry takes the spelling of a tag the address book already has, and
// the book holds MaxIPAddresses entries at most.
func TestIPAddressBookSpellingAndCapacity(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Now()
	if _, err := db.CreateIPAddress(ctx, IPAddress{Range: "10.0.0.1", Tags: []string{"Office"}}, now); err != nil {
		t.Fatal(err)
	}
	second, err := db.CreateIPAddress(ctx, IPAddress{Range: "10.0.0.2", Tags: []string{"OFFICE", "vpn"}}, now)
	if err != nil || !slices.Equal(second.Tags, []string{"Office", "vpn"}) {
		t.Fatalf("second = %+v, %v", second, err)
	}
	for i := 3; i <= MaxIPAddresses; i++ {
		if _, err := db.CreateIPAddress(ctx, IPAddress{Range: fmt.Sprintf("10.0.%d.%d", i/256, i%256), Tags: []string{}}, now); err != nil {
			t.Fatalf("entry %d: %v", i, err)
		}
	}
	if _, err := db.CreateIPAddress(ctx, IPAddress{Range: "192.0.2.1", Tags: []string{}}, now); !errors.Is(err, ErrIPAddressesFull) {
		t.Fatalf("entry %d: %v", MaxIPAddresses+1, err)
	}
}
