package connector

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"strings"
)

var errInvitation = errors.New("invalid connector invitation")

// Connect reads the install code the console shows and returns the
// configuration and run token it carries, for `connector connect`. args are
// what follows `connect`: the code as an argument still works for existing
// deployments, but an argument is readable by every local user in
// /proc/<pid>/cmdline, so MEERKAT_INSTALL_CODE or MEERKAT_INSTALL_CODE_FILE is
// preferred.
func Connect(args []string) (Config, string, error) {
	code, err := installCode(args)
	if err != nil {
		return Config{}, "", err
	}
	return invitation(code)
}

// installCode takes the code from its one source and removes the variables from
// the process environment, as EnrollmentToken does, so nothing started later
// inherits them. They are removed on every path, errors included.
func installCode(args []string) (string, error) {
	fromEnv, path := os.Getenv("MEERKAT_INSTALL_CODE"), os.Getenv("MEERKAT_INSTALL_CODE_FILE")
	_ = os.Unsetenv("MEERKAT_INSTALL_CODE")
	_ = os.Unsetenv("MEERKAT_INSTALL_CODE_FILE")
	sources := 0
	for _, set := range []bool{len(args) > 0, fromEnv != "", path != ""} {
		if set {
			sources++
		}
	}
	if sources > 1 || len(args) > 1 {
		return "", errors.New("provide only one install code source")
	}
	switch {
	case len(args) == 1:
		return args[0], nil
	case path != "":
		b, err := os.ReadFile(path)
		if err != nil {
			return "", errors.New("cannot read install code file")
		}
		return strings.TrimSpace(string(b)), nil
	case fromEnv != "":
		return strings.TrimSpace(fromEnv), nil
	}
	return "", errors.New("connect needs an install code: set MEERKAT_INSTALL_CODE or MEERKAT_INSTALL_CODE_FILE")
}

// invitation decodes an install code. Errors never repeat the code.
func invitation(code string) (Config, string, error) {
	var invite struct {
		Version int    `json:"v"`
		Outlet  string `json:"outlet"`
		Name    string `json:"name"`
		Token   string `json:"token"`
	}
	raw, err := base64.RawURLEncoding.DecodeString(code)
	if err == nil {
		err = json.Unmarshal(raw, &invite)
	}
	if err != nil || invite.Version != 1 {
		return Config{}, "", errInvitation
	}
	cfg := Config{OutletURL: invite.Outlet, Name: invite.Name, DataDir: "/var/lib/meerkat", RatholeBinary: "/usr/local/bin/rathole", HeartbeatInterval: "5s"}
	// Connect mode has no MEERKAT_TARGET to pin what the Outlet may ask for.
	if err := cfg.applyAllowedTargets(); err != nil {
		return Config{}, "", err
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, "", err
	}
	return cfg, invite.Token, nil
}
