package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lawnect/meerkat/internal/outlet"
	"github.com/lawnect/meerkat/internal/securefs"
)

func run() error {
	path := flag.String("config", "configs/outlet.json", "bootstrap JSON configuration")
	dir := flag.String("data-dir", "/var/lib/meerkat/outlet", "persistent outlet state directory")
	flag.Parse()
	var cfg outlet.Config
	if err := securefs.ReadJSON(*path, &cfg); err != nil {
		return err
	}
	if cfg.TLSCertificate == "" || cfg.TLSKey == "" {
		return errors.New("outlet HTTPS certificate and key are required")
	}
	unlock, err := securefs.Lock(*dir)
	if err != nil {
		return err
	}
	defer unlock()
	store, err := outlet.Open(cfg, *dir)
	if err != nil {
		return err
	}
	server := &http.Server{Addr: cfg.ListenAddress, Handler: store.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16384, TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12}}
	// No request bodies, headers or child output go to logs.
	server.ErrorLog = slog.NewLogLogger(slog.NewJSONHandler(os.Stderr, nil), slog.LevelWarn)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- server.ListenAndServeTLS(cfg.TLSCertificate, cfg.TLSKey) }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return server.Shutdown(shutdown)
	}
}

func main() {
	if err := run(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("outlet stopped", "error", err.Error())
		os.Exit(1)
	}
}
