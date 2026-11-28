package main

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"flag"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lawnect/meerkat/internal/contract"
	"github.com/lawnect/meerkat/internal/transport"
)

func readCertificate(t *testing.T, path string) *x509.Certificate {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(raw)
	if block == nil || block.Type != "CERTIFICATE" {
		t.Fatalf("%s is not a certificate", path)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	return cert
}

func readKey(t *testing.T, path string) crypto.Signer {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(raw)
	if block == nil || block.Type != "PRIVATE KEY" {
		t.Fatalf("%s is not a private key", path)
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	return key.(crypto.Signer)
}

func samePublicKey(a, b crypto.PublicKey) bool {
	type equaler interface{ Equal(crypto.PublicKey) bool }
	return a.(equaler).Equal(b)
}

// The admin port's private key sits in the Outlet container, which faces the
// internet, while operators are told to trust root-ca.pem. Whoever can read
// api-key.pem must therefore not be able to sign a certificate that machines
// trusting root-ca.pem accept for another domain.
func TestAdminKeyCannotIssueCertificatesForOtherDomains(t *testing.T) {
	dir := t.TempDir()
	if err := certificate(dir); err != nil {
		t.Fatal(err)
	}
	root := readCertificate(t, filepath.Join(dir, "root-ca.pem"))
	api := readCertificate(t, filepath.Join(dir, "api-cert.pem"))
	key := readKey(t, filepath.Join(dir, "api-key.pem"))

	forged := &x509.Certificate{
		SerialNumber: big.NewInt(42),
		Subject:      pkix.Name{CommonName: "evil.example.com"},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"evil.example.com"},
	}
	evilKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.CreateCertificate(rand.Reader, forged, api, &evilKey.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	evil, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	roots, intermediates := x509.NewCertPool(), x509.NewCertPool()
	roots.AddCert(root)
	intermediates.AddCert(api)
	if _, err := evil.Verify(x509.VerifyOptions{Roots: roots, Intermediates: intermediates, DNSName: "evil.example.com", KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}); err == nil {
		t.Fatal("a certificate for another domain, signed with the on-disk admin key, verifies against root-ca.pem")
	}
}

// root-ca.pem is the only thing a machine has to trust, and nothing on disk
// holds the key that signed it.
func TestAdminCertificateChainShape(t *testing.T) {
	dir := t.TempDir()
	if err := certificate(dir); err != nil {
		t.Fatal(err)
	}
	root := readCertificate(t, filepath.Join(dir, "root-ca.pem"))
	api := readCertificate(t, filepath.Join(dir, "api-cert.pem"))
	key := readKey(t, filepath.Join(dir, "api-key.pem"))

	if !root.IsCA || !root.MaxPathLenZero || root.MaxPathLen != 0 {
		t.Fatalf("root must be a CA that can sign leaves only: IsCA=%v MaxPathLen=%d zero=%v", root.IsCA, root.MaxPathLen, root.MaxPathLenZero)
	}
	if !root.PermittedDNSDomainsCritical {
		t.Fatal("the root's name constraints must be critical")
	}
	if got := strings.Join(root.PermittedDNSDomains, ","); got != "localhost,outlet" {
		t.Fatalf("permitted DNS domains = %q", got)
	}
	if len(root.PermittedIPRanges) != 1 || root.PermittedIPRanges[0].String() != "127.0.0.1/32" {
		t.Fatalf("permitted IP ranges = %v", root.PermittedIPRanges)
	}
	if api.IsCA || api.KeyUsage&x509.KeyUsageCertSign != 0 {
		t.Fatal("the certificate the Outlet serves must not be able to sign certificates")
	}
	if api.Equal(root) {
		t.Fatal("the Outlet serves the CA certificate itself")
	}
	if api.NotAfter.After(root.NotAfter) {
		t.Fatal("the served certificate outlives its issuer")
	}
	if !samePublicKey(key.Public(), api.PublicKey) {
		t.Fatal("api-key.pem doesn't belong to api-cert.pem")
	}
	if samePublicKey(key.Public(), root.PublicKey) {
		t.Fatal("api-key.pem is the CA's key")
	}

	// Nothing in the directory but the three files, and no private key but api-key.pem.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("%d files written, want 3", len(entries))
	}
	for _, e := range entries {
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		switch e.Name() {
		case "root-ca.pem", "api-cert.pem":
			if strings.Contains(string(raw), "PRIVATE KEY") {
				t.Fatalf("%s holds a private key", e.Name())
			}
		case "api-key.pem":
		default:
			t.Fatalf("unexpected file %s", e.Name())
		}
		info, err := e.Info()
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode = %v, %v", e.Name(), info.Mode(), err)
		}
	}

	// Every name the admin port is reached by verifies against root-ca.pem.
	roots := x509.NewCertPool()
	roots.AddCert(root)
	for _, name := range []string{"localhost", "outlet", "127.0.0.1"} {
		if _, err := api.Verify(x509.VerifyOptions{Roots: roots, DNSName: name, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if _, err := api.Verify(x509.VerifyOptions{Roots: roots, DNSName: "example.com", KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}); err == nil {
		t.Fatal("certificate accepted for example.com")
	}
}

// What the Outlet loads, and what a Connector given root-ca.pem as its root
// CA checks, still work together end to end.
func TestConnectorTrustsTheOutletThroughRootCA(t *testing.T) {
	dir := t.TempDir()
	if err := certificate(dir); err != nil {
		t.Fatal(err)
	}
	pair, err := tls.LoadX509KeyPair(filepath.Join(dir, "api-cert.pem"), filepath.Join(dir, "api-key.pem"))
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	server.Listener.Close()
	server.Listener = listener
	server.TLS = &tls.Config{Certificates: []tls.Certificate{pair}}
	server.StartTLS()
	defer server.Close()

	_, port, _ := net.SplitHostPort(listener.Addr().String())
	for _, host := range []string{"127.0.0.1", "localhost"} {
		client, err := transport.New("https://"+net.JoinHostPort(host, port), filepath.Join(dir, "root-ca.pem"))
		if err != nil {
			t.Fatal(err)
		}
		err = client.Heartbeat(context.Background(), "credential", contract.Heartbeat{})
		client.Close()
		if err != nil {
			t.Fatalf("%s: %v", host, err)
		}
	}
	other, err := transport.New(server.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if err := other.Heartbeat(context.Background(), "credential", contract.Heartbeat{}); err == nil {
		t.Fatal("certificate accepted without root-ca.pem")
	}
}

func runInit(t *testing.T, args ...string) error {
	t.Helper()
	os.Args = append([]string{"outlet-init"}, args...)
	flag.CommandLine = flag.NewFlagSet("outlet-init", flag.ContinueOnError)
	return run()
}

// An Outlet set up by an earlier release serves a self-signed CA certificate
// whose key is on disk. -renew-certificate replaces just those three files
// with the new chain, and leaves the rest of the deployment alone.
func TestRenewCertificateReplacesAnOldDeploymentsFiles(t *testing.T) {
	dir := t.TempDir()
	if err := runInit(t, "-dir", dir); err != nil {
		t.Fatal(err)
	}
	configs := filepath.Join(dir, "configs")

	// What an earlier release wrote: one self-signed CA certificate as both
	// root-ca.pem and api-cert.pem, and its key as api-key.pem.
	old, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "meerkat-outlet"},
		NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true, IsCA: true, DNSNames: []string{"outlet", "localhost"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &old.PublicKey, old)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(old)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	for name, data := range map[string][]byte{"root-ca.pem": certPEM, "api-cert.pem": certPEM, "api-key.pem": pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})} {
		if err := os.WriteFile(filepath.Join(configs, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	keep := map[string][]byte{}
	for _, name := range []string{"outlet.json", "admin.token", "server.toml"} {
		raw, err := os.ReadFile(filepath.Join(configs, name))
		if err != nil {
			t.Fatal(err)
		}
		keep[name] = raw
	}

	if err := runInit(t, "-dir", dir, "-renew-certificate"); err != nil {
		t.Fatal(err)
	}
	for name, want := range keep {
		got, err := os.ReadFile(filepath.Join(configs, name))
		if err != nil || string(got) != string(want) {
			t.Fatalf("%s changed: %v", name, err)
		}
	}
	root := readCertificate(t, filepath.Join(configs, "root-ca.pem"))
	api := readCertificate(t, filepath.Join(configs, "api-cert.pem"))
	key := readKey(t, filepath.Join(configs, "api-key.pem"))
	if string(root.Raw) == string(der) || !root.IsCA || api.IsCA || !samePublicKey(key.Public(), api.PublicKey) || samePublicKey(key.Public(), root.PublicKey) {
		t.Fatal("the files were not replaced by the new chain")
	}
	roots := x509.NewCertPool()
	roots.AddCert(root)
	if _, err := api.Verify(x509.VerifyOptions{Roots: roots, DNSName: "localhost", KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}); err != nil {
		t.Fatal(err)
	}
	if _, err := tls.LoadX509KeyPair(filepath.Join(configs, "api-cert.pem"), filepath.Join(configs, "api-key.pem")); err != nil {
		t.Fatal(err)
	}

	if err := runInit(t, "-dir", t.TempDir(), "-renew-certificate"); err == nil {
		t.Fatal("renewing in a directory that holds no Outlet was accepted")
	}
}
