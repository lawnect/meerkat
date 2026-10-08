package rathole

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The test executable acts as the child process, so no shell script or real
// rathole installation is needed to test supervision.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "--client" {
		mode, _ := os.ReadFile(os.Args[2])
		if string(mode) == "wait" {
			time.Sleep(time.Hour)
		}
		os.Exit(2)
	}
	os.Exit(m.Run())
}

func testSupervisor(t *testing.T, mode string) *Supervisor {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(path, []byte(mode), 0600); err != nil {
		t.Fatal(err)
	}
	s := NewSupervisor(binary, path)
	s.MinBackoff = time.Millisecond
	s.MaxBackoff = 4 * time.Millisecond
	s.StopTimeout = 100 * time.Millisecond
	return s
}

func TestSupervisorBoundsRestarts(t *testing.T) {
	s := testSupervisor(t, "exit")
	s.MaxRestarts = 2
	if err := s.Run(context.Background()); err == nil {
		t.Fatal("crash loop did not fail")
	}
	if snap := s.Snapshot(); snap.State != "failed" || snap.Restarts != 2 {
		t.Fatalf("snapshot %+v", snap)
	}
}

func TestSupervisorStopsChildOnCancellation(t *testing.T) {
	s := testSupervisor(t, "wait")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	deadline := time.Now().Add(2 * time.Second)
	for s.Snapshot().State != "running" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if s.Snapshot().State != "running" {
		t.Fatal("child did not start")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("child was not reaped")
	}
	if s.Snapshot().State != "stopped" {
		t.Fatal("wrong shutdown state")
	}
}
