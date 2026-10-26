package control

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestConnectionLimitsRoundTrip(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	got, err := db.ConnectionLimits(ctx)
	if err != nil || got.MaxUploadMB != 100 || got.PerIPConcurrent != 512 || got.IdleTimeoutSeconds != 30 || got.Hosts == nil {
		t.Fatalf("defaults = %+v, %v", got, err)
	}
	want := ConnectionLimits{MaxUploadMB: 0, PerIPConcurrent: 0, TotalConcurrent: 50, IdleTimeoutSeconds: 0, MinUploadKBPerSecond: 0,
		Hosts: []HostUpload{{Host: " Files.Example.com ", MaxUploadMB: 4096}}}
	saved, err := db.SetConnectionLimits(ctx, want, time.Now())
	if err != nil || saved.Hosts[0].Host != "files.example.com" {
		t.Fatalf("saved = %+v, %v", saved, err)
	}
	if got, err := db.ConnectionLimits(ctx); err != nil || got.TotalConcurrent != 50 || got.MaxUploadMB != 0 || got.Hosts[0] != (HostUpload{"files.example.com", 4096}) {
		t.Fatalf("stored = %+v, %v", got, err)
	}
	for _, bad := range []ConnectionLimits{
		{MaxUploadMB: maxUploadMB + 1},
		{PerIPConcurrent: -1},
		{IdleTimeoutSeconds: 4},
		{MinUploadKBPerSecond: maxMinUploadPerSecK + 1},
		{Hosts: []HostUpload{{Host: "*.example.com"}}},
		{Hosts: make([]HostUpload, MaxUploadHosts+1)},
	} {
		if _, err := db.SetConnectionLimits(ctx, bad, time.Now()); !errors.Is(err, ErrInvalidConnectionLimits) {
			t.Fatalf("%+v: %v", bad, err)
		}
	}
}
