package rathole

import (
	"context"
	"errors"
	"io"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

type Snapshot struct {
	State    string `json:"state"`
	Restarts int    `json:"restarts"`
}

type Supervisor struct {
	Binary                                           string
	ConfigPath                                       string
	MaxRestarts                                      int
	MinBackoff, MaxBackoff, StableAfter, StopTimeout time.Duration
	mu                                               sync.RWMutex
	snapshot                                         Snapshot
}

func NewSupervisor(binary, config string) *Supervisor {
	return &Supervisor{Binary: binary, ConfigPath: config, MaxRestarts: 5, MinBackoff: time.Second, MaxBackoff: 30 * time.Second, StableAfter: time.Minute, StopTimeout: 5 * time.Second, snapshot: Snapshot{State: "not_started"}}
}

func (s *Supervisor) Snapshot() Snapshot { s.mu.RLock(); defer s.mu.RUnlock(); return s.snapshot }
func (s *Supervisor) set(state string, restarts int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshot = Snapshot{state, restarts}
}

func VerifyBinary(ctx context.Context, binary string) error {
	if !filepath.IsAbs(binary) {
		return errors.New("rathole binary must be an absolute path")
	}
	c := exec.CommandContext(ctx, binary, "--version")
	c.Env = []string{"RUST_LOG=off"}
	// Only version output is inspected; child service logs are never echoed.
	b, err := c.Output()
	valid := false
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "Build Version:") && strings.TrimSpace(strings.TrimPrefix(line, "Build Version:")) == Version {
			valid = true
		}
	}
	if err != nil || !valid {
		return errors.New("rathole v0.5.0 binary required")
	}
	return nil
}

func (s *Supervisor) Run(ctx context.Context) error {
	if !filepath.IsAbs(s.Binary) || !filepath.IsAbs(s.ConfigPath) {
		return errors.New("absolute binary and config paths required")
	}
	if s.MinBackoff <= 0 || s.MaxBackoff < s.MinBackoff || s.StableAfter <= 0 || s.StopTimeout <= 0 || s.MaxRestarts < 0 {
		return errors.New("invalid supervisor limits")
	}
	consecutive, total := 0, 0
	backoff := s.MinBackoff
	for {
		if ctx.Err() != nil {
			s.set("stopped", total)
			return nil
		}
		s.set("starting", total)
		cmd := exec.Command(s.Binary, "--client", s.ConfigPath)
		cmd.Env = []string{"RUST_LOG=off"}
		cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
		started := time.Now()
		if err := cmd.Start(); err == nil {
			s.set("running", total)
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			select {
			case <-ctx.Done():
				_ = cmd.Process.Signal(os.Signal(syscall.SIGTERM))
				timer := time.NewTimer(s.StopTimeout)
				select {
				case <-done:
				case <-timer.C:
					_ = cmd.Process.Kill()
					<-done
				}
				timer.Stop()
				s.set("stopped", total)
				return nil
			case <-done:
			}
		}
		if time.Since(started) >= s.StableAfter {
			consecutive = 0
			backoff = s.MinBackoff
		}
		if consecutive >= s.MaxRestarts {
			s.set("failed", total)
			return errors.New("rathole restart limit exceeded")
		}
		consecutive++
		total++
		s.set("restarting", total)
		delay := backoff + time.Duration(rand.Int64N(int64(backoff/4)+1))
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			s.set("stopped", total)
			return nil
		case <-timer.C:
		}
		backoff = min(backoff*2, s.MaxBackoff)
	}
}
