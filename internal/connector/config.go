package connector

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/lawnect/meerkat/internal/contract"
	"github.com/lawnect/meerkat/internal/securefs"
	"github.com/lawnect/meerkat/internal/validation"
)

type Config struct {
	OutletURL         string          `json:"outlet_url"`
	Name              string          `json:"name"`
	DataDir           string          `json:"data_dir"`
	Target            contract.Target `json:"target"`
	RatholeBinary     string          `json:"rathole_binary"`
	RootCA            string          `json:"root_ca"`
	HeartbeatInterval string          `json:"heartbeat_interval"`
	DevelopmentHTTP   bool            `json:"development_http"`
}

func Load(path string) (Config, error) {
	c := Config{DataDir: "/var/lib/meerkat", RatholeBinary: "/usr/local/bin/rathole", HeartbeatInterval: "5s"}
	if path != "" {
		if err := securefs.ReadJSON(path, &c); err != nil {
			return c, err
		}
	}
	for _, field := range []struct {
		key  string
		dest *string
	}{
		{"MEERKAT_OUTLET", &c.OutletURL}, {"MEERKAT_NAME", &c.Name}, {"MEERKAT_DATA_DIR", &c.DataDir},
		{"MEERKAT_RATHOLE_BINARY", &c.RatholeBinary}, {"MEERKAT_ROOT_CA", &c.RootCA}, {"MEERKAT_HEARTBEAT_INTERVAL", &c.HeartbeatInterval},
	} {
		if value := os.Getenv(field.key); value != "" {
			*field.dest = value
		}
	}
	if target := os.Getenv("MEERKAT_TARGET"); target != "" {
		// Parse the same way as a tunnel address, including IPv6 loopback.
		host, port, err := splitTarget(target)
		if err != nil {
			return c, err
		}
		c.Target = contract.Target{Host: host, Port: port}
	}
	if value := os.Getenv("MEERKAT_DEVELOPMENT_HTTP"); value != "" {
		v, err := strconv.ParseBool(value)
		if err != nil {
			return c, errors.New("invalid development HTTP setting")
		}
		c.DevelopmentHTTP = v
	}
	c.DataDir, _ = filepath.Abs(c.DataDir)
	return c, c.Validate()
}

func (c Config) Validate() error {
	if err := validation.OutletURL(c.OutletURL, c.DevelopmentHTTP); err != nil {
		return err
	}
	if !validation.Label(c.Name) {
		return errors.New("connector name must be a service label")
	}
	if err := validation.Target(c.Target); err != nil {
		return err
	}
	if !filepath.IsAbs(c.RatholeBinary) || !filepath.IsAbs(c.DataDir) {
		return errors.New("binary and data directory must be absolute paths")
	}
	d, err := time.ParseDuration(c.HeartbeatInterval)
	if err != nil || d < time.Second || d > 10*time.Second {
		return errors.New("heartbeat interval must be between 1s and 10s")
	}
	return nil
}

func EnrollmentToken() (string, error) {
	token, path := os.Getenv("MEERKAT_ENROLLMENT_TOKEN"), os.Getenv("MEERKAT_ENROLLMENT_TOKEN_FILE")
	_ = os.Unsetenv("MEERKAT_ENROLLMENT_TOKEN")
	_ = os.Unsetenv("MEERKAT_ENROLLMENT_TOKEN_FILE")
	if token != "" && path != "" {
		return "", errors.New("provide only one enrollment token source")
	}
	if path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			return "", errors.New("cannot read enrollment token file")
		}
		token = strings.TrimSpace(string(b))
	}
	return token, nil
}
