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
	"path/filepath"
	"syscall"
	"time"

	"github.com/lawnect/meerkat/internal/control"
	"github.com/lawnect/meerkat/internal/outlet"
	"github.com/lawnect/meerkat/internal/securefs"
)

func run() error {
	path := flag.String("config", "configs/outlet.json", "bootstrap JSON configuration")
	dir := flag.String("data-dir", "/var/lib/meerkat/outlet", "persistent outlet state directory")
	controlDBPath := flag.String("control-db", "", "SQLite control-plane database path (defaults under data-dir)")
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
	if *controlDBPath == "" {
		*controlDBPath = filepath.Join(*dir, "control.db")
	}
	controlDB, err := control.Open(context.Background(), *controlDBPath)
	if err != nil {
		return err
	}
	defer controlDB.Close()
	defaults, warnings := environmentDefaults(os.Getenv)
	for _, warning := range warnings {
		slog.Warn(warning)
	}
	controlDB.SetDefaults(defaults)
	// The environment's domain and management host apply before the Outlet
	// writes Caddy's and the tunnel's configuration. A refused change (public
	// addresses still use the old domain) keeps the Outlet running.
	if before, err := controlDB.Outlet(context.Background()); err == nil {
		if after, err := controlDB.ApplyEnvironmentDomain(context.Background()); err != nil {
			slog.Warn("the domain from MEERKAT_DOMAIN or MEERKAT_ADMIN_HOSTNAME isn't applied", "error", err.Error())
		} else if before.AdminHostname != "" && before.AdminHostname != after.AdminHostname {
			// Saved limits follow the management host, as when the console changes it.
			if err := controlDB.RenameRateLimitHost(context.Background(), before.AdminHostname, after.AdminHostname, time.Now()); err != nil {
				return err
			}
			slog.Warn("management host changed; Connectors set up with the old address need a new enrollment", "from", before.AdminHostname, "to", after.AdminHostname)
		}
	}
	upstream, metrics, caddyWarnings := caddyAddresses(os.Getenv)
	for _, warning := range caddyWarnings {
		slog.Warn(warning)
	}
	cfg.CaddyUpstream, cfg.CaddyMetrics = upstream, metrics
	slog.Info("settings from the environment", "sso", defaults.SSO != nil, "telegram", defaults.Telegram != nil, "caddy_upstream", upstream, "caddy_metrics", metrics)
	store, err := outlet.OpenWithControl(cfg, *dir, controlDB)
	if err != nil {
		return err
	}
	// Caddy listens on 80 and 443 only once a domain exists.
	if current, err := controlDB.Outlet(context.Background()); err == nil && current.AdminHostname != "" {
		slog.Info("admin console", "url", "https://"+current.AdminHostname+"/")
	} else if err == nil {
		slog.Warn("no domain yet, so Caddy doesn't listen on 80 or 443: set MEERKAT_DOMAIN, or add one in the console on the local admin port")
	}
	server := &http.Server{Addr: cfg.ListenAddress, Handler: store.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16384, TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12}}
	// Caddy refuses client headers over 16 KB; the headers it adds must still fit here.
	publicServer := &http.Server{Addr: ":8080", Handler: store.PublicHandler(), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 32 << 10}
	// No request bodies, headers or child output go to logs.
	server.ErrorLog = slog.NewLogLogger(slog.NewJSONHandler(os.Stderr, nil), slog.LevelWarn)
	publicServer.ErrorLog = server.ErrorLog
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	go store.RunUsageCollector(ctx)
	go store.RunAlerts(ctx)
	go store.RunRateLimits(ctx)
	done := make(chan error, 2)
	go func() { done <- server.ListenAndServeTLS(cfg.TLSCertificate, cfg.TLSKey) }()
	go func() { done <- publicServer.ListenAndServe() }()
	shutdown := func() {
		shutdownCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		_ = server.Shutdown(shutdownCtx)
		_ = publicServer.Shutdown(shutdownCtx)
		// Audit entries and connector state still in memory are written (or
		// set aside on disk) before the database closes.
		store.Close()
	}
	select {
	case err := <-done:
		shutdown()
		return err
	case <-ctx.Done():
		shutdown()
		return nil
	}
}

func main() {
	if err := run(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("outlet stopped", "error", err.Error())
		os.Exit(1)
	}
}
