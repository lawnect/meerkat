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
	var cfg connector.Config
	var token string
	var err error
	if flag.NArg() > 0 && flag.Arg(0) == "connect" {
		// The install code comes from MEERKAT_INSTALL_CODE(_FILE), or still from
		// the argument, which every local user can read in /proc/<pid>/cmdline.
		cfg, token, err = connector.Connect(flag.Args()[1:])
	} else {
		cfg, err = connector.Load(*path)
		if err == nil {
			token, err = connector.EnrollmentToken()
		}
	}
	if err != nil {
		log.Error("invalid connector configuration", "error", err.Error())
		os.Exit(1)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := connector.Run(ctx, cfg, token, log); err != nil {
		log.Error("connector stopped", "error", err.Error())
		os.Exit(1)
	}
}
