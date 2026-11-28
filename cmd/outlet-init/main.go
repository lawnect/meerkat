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

// outlet.json is written last and names the admin token's hash and the
// tunnel keys, so it marks a finished bootstrap. Without it, files left by
// an interrupted run belong to nothing and are written again.
func writeNew(path string, data []byte) error {
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("refusing to overwrite %s", path)
	} else if !os.IsNotExist(err) {
		return err
	}
	return securefs.Write(path, data)
}

func serialNumber() (*big.Int, error) {
	return rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
}

// certificate writes the admin port's TLS files into dir:
//
//   - root-ca.pem, the certificate of a private CA that exists only in this
//     process. Its key is never written anywhere, so nothing on disk can sign
//     with it. Its critical name constraints and path length of zero confine
//     it to the names the admin port is reached by (localhost, outlet and
//     127.0.0.1), so even a machine that trusts it accepts no other domain.
//   - api-cert.pem and api-key.pem, a leaf certificate for those names and its
//     own key. The key is mounted into the Outlet container, which faces the
//     internet, and the leaf cannot sign certificates.
//
// Operators trust root-ca.pem, not the leaf. The CA key being gone means a
// leaf can't be renewed: -renew-certificate makes a new CA and leaf, valid for
// ten years like the one it replaces.
func certificate(dir string) error {
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	notBefore, notAfter := now.Add(-time.Minute), now.Add(10*365*24*time.Hour)
	caSerial, err := serialNumber()
	if err != nil {
		return err
	}
	leafSerial, err := serialNumber()
	if err != nil {
		return err
	}
	_, loopback, err := net.ParseCIDR("127.0.0.1/32")
	if err != nil {
		return err
	}
	caTemplate := &x509.Certificate{
		SerialNumber:                caSerial,
		Subject:                     pkix.Name{CommonName: "Meerkat Outlet admin CA"},
		NotBefore:                   notBefore,
		NotAfter:                    notAfter,
		KeyUsage:                    x509.KeyUsageCertSign,
		BasicConstraintsValid:       true,
		IsCA:                        true,
		MaxPathLen:                  0,
		MaxPathLenZero:              true,
		PermittedDNSDomainsCritical: true,
		PermittedDNSDomains:         []string{"localhost", "outlet"},
		PermittedIPRanges:           []*net.IPNet{loopback},
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		return err
	}
	ca, err := x509.ParseCertificate(caDER)
	if err != nil {
		return err
	}
	leafTemplate := &x509.Certificate{
		SerialNumber:          leafSerial,
		Subject:               pkix.Name{CommonName: "meerkat-outlet"},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  false,
		DNSNames:              []string{"outlet", "localhost"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTemplate, ca, &leafKey.PublicKey, caKey)
	if err != nil {
		return err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(leafKey)
	if err != nil {
		return err
	}
	if err := securefs.Write(filepath.Join(dir, "root-ca.pem"), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})); err != nil {
		return err
	}
	if err := securefs.Write(filepath.Join(dir, "api-cert.pem"), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER})); err != nil {
		return err
	}
	return securefs.Write(filepath.Join(dir, "api-key.pem"), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}))
}

// renewCertificate replaces root-ca.pem, api-cert.pem and api-key.pem of an
// Outlet that is already set up, including one whose files came from a
// release that served the CA itself and kept its key on disk. Nothing else is
// touched. The Outlet reads the files at start: restart it, and trust the
// new root-ca.pem where the old one was trusted.
func renewCertificate(dir string) error {
	configDir := filepath.Join(dir, "configs")
	if _, err := os.Stat(filepath.Join(configDir, "outlet.json")); err != nil {
		return fmt.Errorf("%s is not an Outlet deployment directory: %w", dir, err)
	}
	if err := certificate(configDir); err != nil {
		return err
	}
	fmt.Println("Admin port certificate replaced: restart the Outlet, then trust the new configs/root-ca.pem.")
	return nil
}

func run() error {
	dir := flag.String("dir", "/out", "Outlet deployment directory")
	domain := flag.String("domain", "", "public DNS zone served by Caddy")
	region := flag.String("region", "", "human-readable Outlet region shown in diagnostics")
	upgrade := flag.Bool("upgrade", false, "add generated-runtime fields to an existing Outlet configuration")
	renew := flag.Bool("renew-certificate", false, "replace the admin port's CA and certificate of an existing Outlet")
	flag.Parse()
	if *renew {
		return renewCertificate(*dir)
	}
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
	if _, err := os.Stat(filepath.Join(configDir, "outlet.json")); err == nil {
		return fmt.Errorf("refusing to overwrite %s", filepath.Join(configDir, "outlet.json"))
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
	// The Outlet listens on the public port 2333 and passes the connections
	// it admits to rathole, which listens on a loopback address.
	server, err := rathole.Server(outlet.DefaultTunnelInternalAddress, base64.StdEncoding.EncodeToString(noise.Bytes()), nil)
	if err != nil {
		return err
	}
	if err := securefs.Write(filepath.Join(configDir, "server.toml"), server); err != nil {
		return err
	}
	if err := securefs.Write(filepath.Join(configDir, "admin.token"), []byte(admin+"\n")); err != nil {
		return err
	}
	if err := securefs.Write(filepath.Join(*dir, "outlet-data", "caddy", "Caddyfile"), proxy.EmptyCaddyfile()); err != nil {
		return err
	}
	if *domain != "" {
		if err := securefs.Write(filepath.Join(configDir, "Caddyfile"), proxy.DynamicCaddyfile(proxy.DefaultUpstream)); err != nil {
			return err
		}
	}
	if err := writeNew(filepath.Join(configDir, "outlet.json"), append(configJSON, '\n')); err != nil {
		return err
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
	securefs.RestrictUmask()
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
