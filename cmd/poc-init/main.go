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
	"strconv"
	"time"

	"github.com/lawnect/meerkat/internal/connector"
	"github.com/lawnect/meerkat/internal/contract"
	"github.com/lawnect/meerkat/internal/outlet"
	"github.com/lawnect/meerkat/internal/rathole"
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

func run() error {
	dirFlag := flag.String("dir", ".local/poc", "new local fixture directory")
	enrollmentTTL := flag.Duration("enrollment-ttl", time.Hour, "initial one-time token lifetime")
	credentialTTL := flag.Duration("credential-ttl", 30*24*time.Hour, "connector API credential lifetime")
	flag.Parse()
	if *enrollmentTTL < time.Minute || *credentialTTL < time.Minute {
		return errors.New("credential lifetimes must be at least one minute")
	}
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
	public := base64.StdEncoding.EncodeToString(noise.PublicKey().Bytes())
	admin := outlet.Token()
	cfg := outlet.Config{ListenAddress: "0.0.0.0:8443", TLSCertificate: "/config/api-cert.pem", TLSKey: "/config/api-key.pem", AdminTokenHash: outlet.Hash(admin), EnrollmentTTLSeconds: int(enrollmentTTL.Seconds()), CredentialTTLSeconds: int(credentialTTL.Seconds())}
	services := []rathole.ServerService{}
	for i, letter := range []string{"a", "b"} {
		name, service, id := "connector-"+letter, "service_"+letter, "connector_"+letter
		token, serviceToken := outlet.Token(), outlet.Token()
		remote := contract.Configuration{ConnectorID: id, Revision: 1, ServiceID: service, Target: contract.Target{Host: "app-" + letter, Port: 3000}, Tunnel: contract.Tunnel{RemoteAddress: "outlet:2333", ServerPublicKey: public, ServiceName: service, ServiceToken: serviceToken}}
		probe := "127.0.0.1:" + strconv.Itoa(31001+i)
		cfg.Connectors = append(cfg.Connectors, outlet.Seed{Name: name, Configuration: remote, ProbeAddress: probe, EnrollmentHash: outlet.Hash(token), EnrollmentExpiresAt: time.Now().UTC().Add(*enrollmentTTL)})
		services = append(services, rathole.ServerService{Name: service, Token: serviceToken, BindAddress: probe})
		local := connector.Config{OutletURL: "https://outlet:8443", Name: name, DataDir: "/state", Target: remote.Target, RatholeBinary: "/usr/local/bin/rathole", RootCA: "/config/root-ca.pem", HeartbeatInterval: "5s"}
		if err := securefs.WriteJSON(filepath.Join(configDir, name+".json"), local); err != nil {
			return err
		}
		if err := securefs.Write(filepath.Join(configDir, name+".token"), []byte(token+"\n")); err != nil {
			return err
		}
		if err := securefs.Directory(filepath.Join(dir, name+"-data")); err != nil {
			return err
		}
	}
	server, err := rathole.Server("0.0.0.0:2333", base64.StdEncoding.EncodeToString(noise.Bytes()), services)
	if err != nil {
		return err
	}
	if err := securefs.Write(filepath.Join(configDir, "server.toml"), server); err != nil {
		return err
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
	fmt.Println("Enrollment tokens expire at:", cfg.Connectors[0].EnrollmentExpiresAt.Format(time.RFC3339))
	fmt.Println("Tokens and private keys are in permission-restricted files; none are printed.")
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
