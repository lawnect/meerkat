package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/lawnect/meerkat/internal/connector"
)

func main() {
	path := flag.String("config", "", "connector JSON configuration file (environment overrides)")
	flag.Parse()
	log := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	cfg, err := connector.Load(*path)
	if err != nil {
		log.Error("invalid connector configuration", "error", err.Error())
		os.Exit(1)
	}
	token, err := connector.EnrollmentToken()
	if err != nil {
		log.Error("cannot load enrollment token", "error", err.Error())
		os.Exit(1)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := connector.Run(ctx, cfg, token, log); err != nil {
		log.Error("connector stopped", "error", err.Error())
		os.Exit(1)
	}
}
