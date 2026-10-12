// rathole-watch restarts rathole only when its atomically-written server
// configuration changes. It owns no credentials and never logs child output.
package main

import (
	"context"
	"crypto/sha256"
	"flag"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"
)

func configDigest(path string) ([32]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return [32]byte{}, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return [32]byte{}, err
	}
	var sum [32]byte
	copy(sum[:], h.Sum(nil))
	return sum, nil
}

func main() {
	path := flag.String("config", "/state/rathole/server.toml", "generated rathole server config")
	binary := flag.String("binary", "/usr/local/bin/rathole", "rathole binary")
	flag.Parse()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	var last [32]byte
	for {
		if ctx.Err() != nil {
			return
		}
		next, err := configDigest(*path)
		if err != nil {
			time.Sleep(time.Second)
			continue
		}
		last = next
		cmd := exec.Command(*binary, "--server", *path)
		cmd.Env = []string{"RUST_LOG=off"}
		cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
		if err := cmd.Start(); err != nil {
			time.Sleep(time.Second)
			continue
		}
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		tick := time.NewTicker(time.Second)
		restart := false
		for !restart {
			select {
			case <-ctx.Done():
				_ = cmd.Process.Signal(syscall.SIGTERM)
				<-done
				tick.Stop()
				return
			case <-done:
				restart = true
			case <-tick.C:
				if current, err := configDigest(*path); err == nil && current != last {
					_ = cmd.Process.Signal(syscall.SIGTERM)
					<-done
					restart = true
				}
			}
		}
		tick.Stop()
		time.Sleep(250 * time.Millisecond)
	}
}
