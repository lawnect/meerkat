// outlet-init creates the bootstrap files for a single production Outlet.
// It never prints generated secrets and refuses to overwrite an existing file.
package main

import (
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/lawnect/meerkat/internal/outlet"
	"github.com/lawnect/meerkat/internal/proxy"
	"github.com/lawnect/meerkat/internal/rathole"
	"github.com/lawnect/meerkat/internal/securefs"
)

func writeNew(path string, data []byte) error {
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("refusing to overwrite %s", path)
	} else if !os.IsNotExist(err) {
		return err
	}
	return securefs.Write(path, data)
}

func certificate(dir string) error {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(now.UnixNano()),
		Subject:               pkix.Name{CommonName: "meerkat-outlet"},
		NotBefore:             now.Add(-time.Minute),
		NotAfter:              now.Add(10 * 365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		DNSNames:              []string{"outlet", "localhost"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	if err := writeNew(filepath.Join(dir, "root-ca.pem"), certPEM); err != nil {
		return err
	}
	if err := writeNew(filepath.Join(dir, "api-cert.pem"), certPEM); err != nil {
		return err
	}
	return writeNew(filepath.Join(dir, "api-key.pem"), keyPEM)
}

func run() error {
	dir := flag.String("dir", "/out", "Outlet deployment directory")
	domain := flag.String("domain", "", "public DNS zone served by Caddy")
	region := flag.String("region", "", "human-readable Outlet region shown in diagnostics")
	upgrade := flag.Bool("upgrade", false, "add generated-runtime fields to an existing Outlet configuration")
	flag.Parse()
	if *upgrade {
		if *domain == "" {
			return errors.New("-domain is required for -upgrade")
		}
		return upgradeConfig(*dir, *domain)
	}
	if err := securefs.Directory(*dir); err != nil {
		return err
	}
	configDir := filepath.Join(*dir, "configs")
	if err := securefs.Directory(configDir); err != nil {
		return err
	}
	if err := securefs.Directory(filepath.Join(*dir, "outlet-data")); err != nil {
		return err
	}
	if err := securefs.Directory(filepath.Join(*dir, "caddy-data")); err != nil {
		return err
	}
	if err := securefs.Directory(filepath.Join(*dir, "caddy-config")); err != nil {
		return err
	}
	if err := certificate(configDir); err != nil {
		return err
	}

	noise, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	admin := outlet.Token()
	remoteAddress := ""
	if *domain != "" {
		remoteAddress = net.JoinHostPort(*domain, "2333")
	}
	cfg := outlet.Config{
		ListenAddress:          "0.0.0.0:8443",
		TLSCertificate:         "/config/api-cert.pem",
		TLSKey:                 "/config/api-key.pem",
		AdminTokenHash:         outlet.Hash(admin),
		EnrollmentTTLSeconds:   60 * 60,
		CredentialTTLSeconds:   30 * 24 * 60 * 60,
		Domain:                 *domain,
		Region:                 *region,
		TunnelListenAddress:    "0.0.0.0:2333",
		TunnelRemoteAddress:    remoteAddress,
		TunnelServerPublicKey:  base64.StdEncoding.EncodeToString(noise.PublicKey().Bytes()),
		TunnelServerPrivateKey: base64.StdEncoding.EncodeToString(noise.Bytes()),
		Connectors:             []outlet.Seed{},
	}
	configJSON, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := writeNew(filepath.Join(configDir, "outlet.json"), append(configJSON, '\n')); err != nil {
		return err
	}
	server, err := rathole.Server("0.0.0.0:2333", base64.StdEncoding.EncodeToString(noise.Bytes()), nil)
	if err != nil {
		return err
	}
	if err := writeNew(filepath.Join(configDir, "server.toml"), server); err != nil {
		return err
	}
	if err := writeNew(filepath.Join(configDir, "admin.token"), []byte(admin+"\n")); err != nil {
		return err
	}
	if err := writeNew(filepath.Join(*dir, "outlet-data", "caddy", "Caddyfile"), proxy.EmptyCaddyfile()); err != nil {
		return err
	}
	if *domain != "" {
		if err := writeNew(filepath.Join(configDir, "Caddyfile"), proxy.DynamicCaddyfile()); err != nil {
			return err
		}
	}
	fmt.Println("Outlet bootstrap files created.")
	fmt.Println("Admin token: /configs/admin.token")
	fmt.Println("Generated secrets were not printed.")
	return nil
}

var noiseKey = regexp.MustCompile(`(?m)^local_private_key\s*=\s*"([A-Za-z0-9+/=]+)"\s*$`)

func upgradeConfig(dir, domain string) error {
	configPath := filepath.Join(dir, "configs", "outlet.json")
	serverPath := filepath.Join(dir, "configs", "server.toml")
	var cfg outlet.Config
	if err := securefs.ReadJSON(configPath, &cfg); err != nil {
		return err
	}
	if cfg.Domain != "" || cfg.TunnelServerPrivateKey != "" {
		return errors.New("outlet configuration is already upgraded")
	}
	server, err := os.ReadFile(serverPath)
	if err != nil {
		return err
	}
	match := noiseKey.FindSubmatch(server)
	if len(match) != 2 {
		return errors.New("cannot read Noise private key from server.toml")
	}
	private, err := base64.StdEncoding.DecodeString(string(match[1]))
	if err != nil {
		return errors.New("invalid Noise private key")
	}
	key, err := ecdh.X25519().NewPrivateKey(private)
	if err != nil {
		return errors.New("invalid Noise private key")
	}
	cfg.Domain, cfg.TunnelListenAddress, cfg.TunnelRemoteAddress = domain, "0.0.0.0:2333", net.JoinHostPort(domain, "2333")
	cfg.TunnelServerPrivateKey = base64.StdEncoding.EncodeToString(private)
	cfg.TunnelServerPublicKey = base64.StdEncoding.EncodeToString(key.PublicKey().Bytes())
	return securefs.WriteJSON(configPath, cfg)
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
