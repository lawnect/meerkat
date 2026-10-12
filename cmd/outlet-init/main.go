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
	"time"

	"github.com/lawnect/meerkat/internal/outlet"
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
	flag.Parse()
	if *domain == "" {
		return errors.New("-domain is required")
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
	cfg := outlet.Config{
		ListenAddress:         "0.0.0.0:8443",
		TLSCertificate:        "/config/api-cert.pem",
		TLSKey:                "/config/api-key.pem",
		AdminTokenHash:        outlet.Hash(admin),
		EnrollmentTTLSeconds:  600,
		CredentialTTLSeconds:  30 * 24 * 60 * 60,
		TunnelRemoteAddress:   net.JoinHostPort(*domain, "2333"),
		TunnelServerPublicKey: base64.StdEncoding.EncodeToString(noise.PublicKey().Bytes()),
		Connectors:            []outlet.Seed{},
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
	caddyfile := fmt.Sprintf(`{
	email {env.CADDY_EMAIL}
}

%s, *.%s {
	tls {
		dns cloudflare {env.CF_API_TOKEN}
	}

	reverse_proxy https://outlet:8443 {
		transport http {
			tls_trust_pool file /etc/caddy/root-ca.pem
			tls_server_name outlet
		}
	}
}
`, *domain, *domain)
	if err := writeNew(filepath.Join(configDir, "Caddyfile"), []byte(caddyfile)); err != nil {
		return err
	}
	fmt.Println("Outlet bootstrap files created.")
	fmt.Println("Admin token: /configs/admin.token")
	fmt.Println("Generated secrets were not printed.")
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
