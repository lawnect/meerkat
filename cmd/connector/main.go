package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
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
	if flag.NArg() == 2 && flag.Arg(0) == "connect" {
		var invitation struct {
			Version int    `json:"v"`
			Outlet  string `json:"outlet"`
			Name    string `json:"name"`
			Token   string `json:"token"`
		}
		var raw []byte
		raw, err = base64.RawURLEncoding.DecodeString(flag.Arg(1))
		if err == nil {
			err = json.Unmarshal(raw, &invitation)
		}
		if err == nil && invitation.Version != 1 {
			err = &invalidInvitation{}
		}
		if err == nil {
			cfg = connector.Config{OutletURL: invitation.Outlet, Name: invitation.Name, DataDir: "/var/lib/meerkat", RatholeBinary: "/usr/local/bin/rathole", HeartbeatInterval: "5s"}
			token = invitation.Token
			err = cfg.Validate()
		}
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

type invalidInvitation struct{}

func (*invalidInvitation) Error() string { return "invalid connector invitation" }
