package control

import (
	"context"
	"errors"
	"fmt"
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
	if err != nil || got.MaxUploadMB != 100 || got.PerIPConcurrent != 512 || got.IdleTimeoutSeconds != 30 || got.Hosts == nil ||
		got.ResponseTimeoutSeconds != 60 || got.ResponseTimeouts == nil || got.RequestTimeoutMinutes != 60 || got.RequestTimeouts == nil {
		t.Fatalf("defaults = %+v, %v", got, err)
	}
	want := ConnectionLimits{MaxUploadMB: 0, PerIPConcurrent: 0, TotalConcurrent: 50, IdleTimeoutSeconds: 0, MinUploadKBPerSecond: 0,
		Hosts:                  []HostUpload{{Host: " Files.Example.com ", MaxUploadMB: 4096}},
		ResponseTimeoutSeconds: 0,
		ResponseTimeouts:       []HostResponseTimeout{{Host: "AI.Example.com", Seconds: 600}, {Host: "batch.example.com", Seconds: 0}},
		RequestTimeoutMinutes:  1440,
		RequestTimeouts:        []HostRequestTimeout{{Host: " Files.Example.com ", Minutes: 0}, {Host: "ai.example.com", Minutes: 1}}}
	saved, err := db.SetConnectionLimits(ctx, want, time.Now())
	if err != nil || saved.Hosts[0].Host != "files.example.com" || saved.ResponseTimeouts[0].Host != "ai.example.com" || saved.RequestTimeouts[0].Host != "files.example.com" {
		t.Fatalf("saved = %+v, %v", saved, err)
	}
	if got, err := db.ConnectionLimits(ctx); err != nil || got.TotalConcurrent != 50 || got.MaxUploadMB != 0 || got.Hosts[0] != (HostUpload{"files.example.com", 4096}) ||
		got.ResponseTimeoutSeconds != 0 || len(got.ResponseTimeouts) != 2 || got.ResponseTimeouts[0] != (HostResponseTimeout{"ai.example.com", 600}) || got.ResponseTimeouts[1].Seconds != 0 ||
		got.RequestTimeoutMinutes != 1440 || len(got.RequestTimeouts) != 2 || got.RequestTimeouts[0] != (HostRequestTimeout{"files.example.com", 0}) || got.RequestTimeouts[1] != (HostRequestTimeout{"ai.example.com", 1}) {
		t.Fatalf("stored = %+v, %v", got, err)
	}
	if _, err := db.SetConnectionLimits(ctx, ConnectionLimits{RequestTimeouts: manyDurations(MaxTimeoutHosts)}, time.Now()); err != nil {
		t.Fatalf("%d request timeouts: %v", MaxTimeoutHosts, err)
	}
	if _, err := db.SetConnectionLimits(ctx, ConnectionLimits{ResponseTimeouts: manyTimeouts(MaxTimeoutHosts)}, time.Now()); err != nil {
		t.Fatalf("%d response timeouts: %v", MaxTimeoutHosts, err)
	}
	for _, bad := range []ConnectionLimits{
		{MaxUploadMB: maxUploadMB + 1},
		{PerIPConcurrent: -1},
		{IdleTimeoutSeconds: 4},
		{MinUploadKBPerSecond: maxMinUploadPerSecK + 1},
		{Hosts: []HostUpload{{Host: "*.example.com"}}},
		{Hosts: make([]HostUpload, MaxUploadHosts+1)},
		{ResponseTimeoutSeconds: 4},
		{ResponseTimeoutSeconds: maxResponseTimeout + 1},
		{ResponseTimeouts: []HostResponseTimeout{{Host: "ai.example.com", Seconds: 4}}},
		{ResponseTimeouts: []HostResponseTimeout{{Host: "ai.example.com", Seconds: -1}}},
		{ResponseTimeouts: []HostResponseTimeout{{Host: "ai.example.com", Seconds: 600}, {Host: "AI.example.com", Seconds: 60}}},
		{ResponseTimeouts: []HostResponseTimeout{{Host: "ai.example.com:443", Seconds: 600}}},
		{ResponseTimeouts: manyTimeouts(MaxTimeoutHosts + 1)},
		{RequestTimeoutMinutes: -1},
		{RequestTimeoutMinutes: maxRequestTimeout + 1},
		{RequestTimeouts: []HostRequestTimeout{{Host: "files.example.com", Minutes: maxRequestTimeout + 1}}},
		{RequestTimeouts: []HostRequestTimeout{{Host: "files.example.com", Minutes: -1}}},
		{RequestTimeouts: []HostRequestTimeout{{Host: "files.example.com", Minutes: 60}, {Host: "FILES.example.com", Minutes: 0}}},
		{RequestTimeouts: []HostRequestTimeout{{Host: "*.example.com", Minutes: 60}}},
		{RequestTimeouts: manyDurations(MaxTimeoutHosts + 1)},
	} {
		if _, err := db.SetConnectionLimits(ctx, bad, time.Now()); !errors.Is(err, ErrInvalidConnectionLimits) {
			t.Fatalf("%+v: %v", bad, err)
		}
	}
}

// Limits saved before the response timeout existed keep the wait they had,
// and limits saved before the request time limit get the default hour.
func TestConnectionLimitsStoredBeforeResponseTimeouts(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	old := `{"max_upload_mb":0,"per_ip_concurrent":10,"total_concurrent":20,"idle_timeout_seconds":0,"min_upload_kb_per_second":0,"hosts":[{"host":"files.example.com","max_upload_mb":1}]}`
	if err := putSetting(ctx, db.db, settingConnectionLimits, old, time.Now()); err != nil {
		t.Fatal(err)
	}
	got, err := db.ConnectionLimits(ctx)
	if err != nil || got.ResponseTimeoutSeconds != DefaultResponseTimeoutSeconds || got.ResponseTimeouts == nil || len(got.ResponseTimeouts) != 0 ||
		got.MaxUploadMB != 0 || got.IdleTimeoutSeconds != 0 || got.PerIPConcurrent != 10 || len(got.Hosts) != 1 ||
		got.RequestTimeoutMinutes != DefaultRequestTimeoutMinutes || got.RequestTimeouts == nil || len(got.RequestTimeouts) != 0 {
		t.Fatalf("stored before = %+v, %v", got, err)
	}
	// What 0.8.1 saved, with response timeouts and no request time limit.
	before := `{"max_upload_mb":0,"per_ip_concurrent":10,"total_concurrent":20,"idle_timeout_seconds":0,"min_upload_kb_per_second":0,"hosts":[],"response_timeout_seconds":0,"response_timeouts":[{"host":"ai.example.com","response_timeout_seconds":630}]}`
	if err := putSetting(ctx, db.db, settingConnectionLimits, before, time.Now()); err != nil {
		t.Fatal(err)
	}
	got, err = db.ConnectionLimits(ctx)
	if err != nil || got.ResponseTimeoutSeconds != 0 || len(got.ResponseTimeouts) != 1 || got.RequestTimeoutMinutes != DefaultRequestTimeoutMinutes || got.RequestTimeouts == nil {
		t.Fatalf("stored by 0.8.1 = %+v, %v", got, err)
	}
}

func manyDurations(n int) []HostRequestTimeout {
	durations := make([]HostRequestTimeout, n)
	for i := range durations {
		durations[i] = HostRequestTimeout{Host: fmt.Sprintf("app%d.example.com", i), Minutes: 240}
	}
	return durations
}

func manyTimeouts(n int) []HostResponseTimeout {
	timeouts := make([]HostResponseTimeout, n)
	for i := range timeouts {
		timeouts[i] = HostResponseTimeout{Host: fmt.Sprintf("app%d.example.com", i), Seconds: 600}
	}
	return timeouts
}
