package outlet

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/lawnect/meerkat/internal/proxy"
)

// Run against the exact executable shipped in meerkat-caddy, for example:
// MEERKAT_CADDY_BINARY=/path/to/caddy go test ./internal/outlet -run TestCaddyMetricsHostCardinality
// The unpatched official 2.11.7 binary fails this test.
func TestCaddyMetricsHostCardinality(t *testing.T) {
	binary := os.Getenv("MEERKAT_CADDY_BINARY")
	if binary == "" {
		t.Skip("set MEERKAT_CADDY_BINARY to test the pinned Caddy runtime")
	}
	version, err := exec.Command(binary, "version").CombinedOutput()
	if err != nil || !strings.HasPrefix(string(version), "v2.11.7") {
		t.Fatalf("expected pinned Caddy 2.11.7, got %q: %v", version, err)
	}
	t.Logf("runtime: %s", strings.TrimSpace(string(version)))
	f := newProxyFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if r.URL.Path == "/error" {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = io.WriteString(w, "broken")
			return
		}
		_, _ = io.WriteString(w, "hello")
	}))
	routeID := f.route(t, 0, "app.example.com")
	outlet := httptest.NewServer(f.handler)
	defer outlet.Close()

	dir := t.TempDir()
	certFile, keyFile := caddyTestCertificate(t, dir)
	httpsAddress, httpAddress, metricsAddress := caddyTestAddress(t), caddyTestAddress(t), caddyTestAddress(t)
	config := string(proxy.DynamicCaddyfile(strings.TrimPrefix(outlet.URL, "http://")))
	config = strings.Replace(config, "{\n", "{\n\tadmin off\n\tauto_https off\n\tdefault_bind 127.0.0.1\n", 1)
	config = strings.Replace(config, ":9090 {", ":"+strings.Split(metricsAddress, ":")[1]+" {", 1)
	config = strings.Replace(config, "http:// {", "http://:"+strings.Split(httpAddress, ":")[1]+" {", 1)
	config = strings.Replace(config, "https:// {", "https://:"+strings.Split(httpsAddress, ":")[1]+" {", 1)
	config = strings.Replace(config, "\ttls {\n\t\ton_demand\n\t\tissuer acme {\n\t\t\tdisable_tlsalpn_challenge\n\t\t}\n\t}", "\ttls "+certFile+" "+keyFile, 1)
	configFile := filepath.Join(dir, "Caddyfile")
	if err := os.WriteFile(configFile, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	logFile, err := os.Create(filepath.Join(dir, "caddy.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer logFile.Close()
	cmd := exec.Command(binary, "run", "--config", configFile, "--adapter", "caddyfile")
	cmd.Env = append(os.Environ(), "XDG_DATA_HOME="+dir, "XDG_CONFIG_HOME="+dir)
	cmd.Stdout, cmd.Stderr = logFile, logFile
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		if t.Failed() {
			logs, _ := os.ReadFile(logFile.Name())
			t.Logf("Caddy log:\n%s", logs)
		}
	})
	metricsClient := &http.Client{Timeout: time.Second}
	readMetrics := func() (string, error) {
		resp, err := metricsClient.Get("http://" + metricsAddress + "/metrics")
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		data, err := io.ReadAll(resp.Body)
		return string(data), err
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err = readMetrics(); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("Caddy did not start: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true, ServerName: "app.example.com"}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	var responseBytes int64
	send := func(scheme, address, host, path, body string, status int) {
		t.Helper()
		req, err := http.NewRequest(http.MethodPost, scheme+"://"+address+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Host = host
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("host %q: %v", host, err)
		}
		data, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil || resp.StatusCode != status {
			t.Fatalf("host %q: status %d body %q, want %d: %v", host, resp.StatusCode, data, status, err)
		}
		if scheme == "https" && status != http.StatusMisdirectedRequest {
			responseBytes += int64(len(data))
		}
	}
	send("https", httpsAddress, "app.example.com", "/", "", 200)
	baselineMetrics, err := readMetrics()
	if err != nil {
		t.Fatal(err)
	}
	baseline, _, err := parseUsageMetrics(strings.NewReader(baselineMetrics), map[string]string{"app.example.com": routeID})
	if err != nil {
		t.Fatal(err)
	}
	send("https", httpsAddress, "app.example.com:443", "/", "upload", 200)
	portMetrics, err := readMetrics()
	if err != nil {
		t.Fatal(err)
	}
	withPort, _, err := parseUsageMetrics(strings.NewReader(portMetrics), map[string]string{"app.example.com": routeID})
	if err != nil {
		t.Fatal(err)
	}
	// The :443 request adds exactly four authority bytes and six body bytes
	// relative to the otherwise identical request. Caddy's size estimate and
	// the collector must retain both, while using the same host label.
	base, port := baseline[routeID], withPort[routeID]
	if base.Requests != 1 || base.ResponseBytes != 5 || port.Requests != 2 || port.RequestBytes != 2*base.RequestBytes+10 || port.ResponseBytes != 10 {
		t.Fatalf(":443 accounting: baseline %+v, cumulative after upload %+v", base, port)
	}
	send("https", httpsAddress, "APP.EXAMPLE.COM:443", "/", "", 200)
	send("https", httpsAddress, "app.example.com", "/error", "", 502)
	const variations = 128
	for i := range variations {
		// Numeric padding and arbitrary nonnumeric port strings both passed
		// strict_sni_host in the vulnerable runtime before growing its vectors.
		suffix := fmt.Sprintf("port-%d", i)
		if i%2 == 0 {
			suffix = fmt.Sprintf("%06d", i)
		}
		send("https", httpsAddress, "app.example.com:"+suffix, "/", "", 404)
		send("http", httpAddress, fmt.Sprintf("unknown-%d.example.com:%s", i, suffix), "/", "", 404)
	}
	// SNI mismatch must still be rejected before route metric allocation.
	send("https", httpsAddress, "unregistered.example.com", "/", "", 421)
	metrics, err := readMetrics()
	if err != nil {
		t.Fatal(err)
	}
	hostLabel := regexp.MustCompile(`(?:\{|,)host="([^"]*)"`)
	hosts := map[string]bool{}
	for _, line := range strings.Split(metrics, "\n") {
		if !strings.HasPrefix(line, "caddy_http_") {
			continue
		}
		if match := hostLabel.FindStringSubmatch(line); len(match) == 2 {
			hosts[match[1]] = true
		}
	}
	if len(hosts) != 2 || !hosts["app.example.com"] || !hosts["_other"] {
		t.Fatalf("unbounded or missing metric hosts after %d variants: %+v", variations, hosts)
	}
	samples, generation, err := parseUsageMetrics(strings.NewReader(metrics), map[string]string{"app.example.com": routeID})
	if err != nil {
		t.Fatal(err)
	}
	sample := samples[routeID]
	if sample.Requests != 4+variations || sample.ResponseBytes != responseBytes || sample.ErrorResponses != 1 || sample.RequestBytes < 6 || generation == "" {
		t.Fatalf("parsed usage = %+v generation %q, want %d requests, %d response bytes, one error and nonzero request bytes", sample, generation, 4+variations, responseBytes)
	}
	// The existing collector and history store must still accept the runtime's
	// normalized counters, including :443 traffic, without double-counting.
	f.store.config.CaddyMetrics = metricsAddress
	for range 2 {
		if err := f.store.CollectUsage(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	usage, err := f.db.ConnectorUsage(context.Background(), f.connector.ID, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	last := usage.Hours[len(usage.Hours)-1]
	if last.Requests != sample.Requests || last.ResponseBytes != sample.ResponseBytes || last.RequestBytes != sample.RequestBytes || last.ErrorResponses != sample.ErrorResponses {
		t.Fatalf("persisted usage %+v differs from runtime %+v", last, sample)
	}
}

func caddyTestAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	return address
}

func caddyTestCertificate(t *testing.T, dir string) (string, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	certificate := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "app.example.com"}, DNSNames: []string{"app.example.com"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, certificate, certificate, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certFile, keyFile := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0600); err != nil {
		t.Fatal(err)
	}
	return certFile, keyFile
}
