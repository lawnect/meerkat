// poc-init provisions a fresh, private local test fixture. It never overwrites
// an existing fixture and is not a production installation workflow.
package main

import (
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/lawnect/meerkat/internal/connector"
	"github.com/lawnect/meerkat/internal/contract"
	"github.com/lawnect/meerkat/internal/outlet"
	"github.com/lawnect/meerkat/internal/securefs"
)

func certificate(dir string) error {
	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	now := time.Now()
	// This self-signed certificate is a local development trust anchor only.
	template := &x509.Certificate{SerialNumber: big.NewInt(now.UnixNano()), Subject: pkix.Name{CommonName: "Meerkat local PoC"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(365 * 24 * time.Hour), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true, IsCA: true, DNSNames: []string{"outlet", "localhost"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &private.PublicKey, private)
	if err != nil {
		return err
	}
	key, err := x509.MarshalPKCS8PrivateKey(private)
	if err != nil {
		return err
	}
	for _, name := range []string{"root-ca.pem", "api-cert.pem"} {
		if err := securefs.Write(filepath.Join(dir, name), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})); err != nil {
			return err
		}
	}
	return securefs.Write(filepath.Join(dir, "api-key.pem"), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key}))
}

// The Outlet starts with no tunnels, as a production one does. The E2E test
// creates them through the admin API and leaves each run token in
// <name>-token/, where its Connector waits for it.
func run() error {
	dirFlag := flag.String("dir", ".local/poc", "new local fixture directory")
	flag.Parse()
	dir, err := filepath.Abs(*dirFlag)
	if err != nil {
		return err
	}
	if err := os.Mkdir(dir, 0700); err != nil {
		if os.IsNotExist(err) {
			if err := os.MkdirAll(filepath.Dir(dir), 0700); err != nil {
				return err
			}
			if err := os.Mkdir(dir, 0700); err != nil {
				return err
			}
		} else {
			return errors.New("fixture directory already exists or cannot be created; choose a new directory")
		}
	}
	configDir := filepath.Join(dir, "configs")
	if err := securefs.Directory(configDir); err != nil {
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
	cfg := outlet.Config{ListenAddress: "0.0.0.0:8443", TLSCertificate: "/config/api-cert.pem", TLSKey: "/config/api-key.pem", AdminTokenHash: outlet.Hash(admin), EnrollmentTTLSeconds: 60 * 60, CredentialTTLSeconds: 30 * 24 * 60 * 60, TunnelListenAddress: "0.0.0.0:2333", TunnelRemoteAddress: "outlet:2333", TunnelServerPublicKey: base64.StdEncoding.EncodeToString(noise.PublicKey().Bytes()), TunnelServerPrivateKey: base64.StdEncoding.EncodeToString(noise.Bytes()), Connectors: []outlet.Seed{}}
	for _, letter := range []string{"a", "b"} {
		name := "connector-" + letter
		// The target is the Connector's local allowlist: it refuses a service
		// the Outlet points anywhere else.
		local := connector.Config{OutletURL: "https://outlet:8443", Name: name, DataDir: "/state", Target: contract.Target{Host: "app-" + letter, Port: 3000}, RatholeBinary: "/usr/local/bin/rathole", RootCA: "/config/root-ca.pem", HeartbeatInterval: "5s"}
		if err := securefs.WriteJSON(filepath.Join(configDir, name+".json"), local); err != nil {
			return err
		}
		for _, sub := range []string{"-data", "-token"} {
			if err := securefs.Directory(filepath.Join(dir, name+sub)); err != nil {
				return err
			}
		}
	}
	if err := securefs.WriteJSON(filepath.Join(configDir, "outlet.json"), cfg); err != nil {
		return err
	}
	if err := securefs.Write(filepath.Join(configDir, "admin.token"), []byte(admin+"\n")); err != nil {
		return err
	}
	if err := securefs.Directory(filepath.Join(dir, "outlet-data")); err != nil {
		return err
	}
	env := fmt.Sprintf("MEERKAT_POC_DIR=%s\nMEERKAT_UID=%d\nMEERKAT_GID=%d\n", dir, os.Getuid(), os.Getgid())
	if err := securefs.Write(filepath.Join(dir, "compose.env"), []byte(env)); err != nil {
		return err
	}
	fmt.Println("Local fixture created:", dir)
	fmt.Println("Tokens and private keys are in permission-restricted files; none are printed.")
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
