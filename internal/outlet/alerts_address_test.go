package outlet

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"math/big"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lawnect/meerkat/internal/contract"
	"github.com/lawnect/meerkat/internal/control"
)

// addressTestCA signs the certificates of fakeCaddy.
type addressTestCA struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
	pool *x509.CertPool
}

func newAddressTestCA() addressTestCA {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		panic(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Test CA"},
		NotBefore: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC), NotAfter: time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		panic(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		panic(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	return addressTestCA{cert, key, pool}
}

// issue signs a certificate for name that is valid between the two times.
func (ca addressTestCA) issue(name string, notBefore, notAfter time.Time) tls.Certificate {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		panic(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: name}, DNSNames: []string{name},
		NotBefore: notBefore, NotAfter: notAfter,
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, ca.cert, &key.PublicKey, ca.key)
	if err != nil {
		panic(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// fakeCaddy answers TLS handshakes on a local port the way Caddy does for the
// public addresses: with the certificate it holds for the name in the
// ClientHello, or with an alert.
type fakeCaddy struct {
	ca       addressTestCA
	listener net.Listener
	mu       sync.Mutex
	certs    map[string]tls.Certificate
	// handshakes counts the ClientHellos per name; dialed lists the addresses
	// the Outlet asked for.
	handshakes map[string]int
	dialed     []string
	// anyName serves a long-lived certificate for names without one.
	anyName bool
}

func startFakeCaddy() *fakeCaddy {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	c := &fakeCaddy{ca: newAddressTestCA(), listener: listener, certs: map[string]tls.Certificate{}, handshakes: map[string]int{}}
	config := &tls.Config{GetCertificate: c.certificate}
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				_ = tls.Server(conn, config).Handshake()
			}()
		}
	}()
	return c
}

func (c *fakeCaddy) certificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.handshakes[hello.ServerName]++
	cert, ok := c.certs[hello.ServerName]
	if !ok && c.anyName {
		cert = c.ca.issue(hello.ServerName, time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC))
		c.certs[hello.ServerName], ok = cert, true
	}
	if !ok {
		return nil, errors.New("no certificate for " + hello.ServerName)
	}
	return &cert, nil
}

func (c *fakeCaddy) dial(ctx context.Context, address string) (net.Conn, error) {
	c.mu.Lock()
	c.dialed = append(c.dialed, address)
	c.mu.Unlock()
	return (&net.Dialer{}).DialContext(ctx, "tcp", c.listener.Addr().String())
}

// serve gives name a certificate for itself that ends at notAfter.
func (c *fakeCaddy) serve(name string, notAfter time.Time) { c.serveAs(name, name, notAfter) }

// serveAs gives the ClientHello name a 90-day certificate issued for
// another name.
func (c *fakeCaddy) serveAs(name, issuedFor string, notAfter time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.certs[name] = c.ca.issue(issuedFor, notAfter.Add(-90*24*time.Hour), notAfter)
}

func (c *fakeCaddy) remove(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.certs, name)
}

func (c *fakeCaddy) seen(name string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.handshakes[name]
}

func (c *fakeCaddy) dials() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.dialed...)
}

// useAddressCaddy points the address check at c for one test.
func useAddressCaddy(t *testing.T, c *fakeCaddy) {
	t.Helper()
	dial, roots, timeout, budget := addressDial, addressRoots, addressTimeout, addressBudget
	addressDial, addressRoots = c.dial, c.ca.pool
	t.Cleanup(func() { addressDial, addressRoots, addressTimeout, addressBudget = dial, roots, timeout, budget })
}

// Tests that don't care about the public addresses meet a Caddy that serves
// every name with a valid certificate, so the check never reaches the network
// or raises an alert.
func init() {
	caddy := startFakeCaddy()
	caddy.anyName = true
	addressDial, addressRoots = caddy.dial, caddy.ca.pool
}

// addressFixture has Telegram set up, a Connector whose service is published
// at app.example.com, and a Caddy that serves the management host and
// app.example.com with certificates that are good for 60 days.
type addressFixture struct {
	alertFixture
	caddy     *fakeCaddy
	connector control.Connector
	service   control.Service
	route     control.Route
}

const addressApp = "app.example.com"

func newAddressFixture(t *testing.T) addressFixture {
	t.Helper()
	a := newAlertFixture(t)
	a.configureTelegram(t, false)
	ctx := context.Background()
	connector, err := a.db.CreateConnector(ctx, "edge-a")
	if err != nil {
		t.Fatal(err)
	}
	service, err := a.db.CreateService(ctx, connector.ID, "web", "http", contract.Target{Host: "app", Port: 3000})
	if err != nil {
		t.Fatal(err)
	}
	route, err := a.db.CreateHTTPRoute(ctx, service.ID, addressApp, false)
	if err != nil {
		t.Fatal(err)
	}
	f := addressFixture{alertFixture: a, caddy: startFakeCaddy(), connector: connector, service: service, route: route}
	t.Cleanup(func() { f.caddy.listener.Close() })
	useAddressCaddy(t, f.caddy)
	f.caddy.serve(a.host, f.in(60*24*time.Hour))
	f.caddy.serve(addressApp, f.in(60*24*time.Hour))
	return f
}

// in is a time d after the test clock.
func (f addressFixture) in(d time.Duration) time.Time { return f.clock.now().Add(d) }

// next lets a check interval pass, then runs a round.
func (f addressFixture) next() {
	f.clock.advance(addressInterval)
	f.evaluate()
}

func (f addressFixture) appRef() string {
	return "<a href=\"https://" + f.host + "/connectors/" + f.connector.ID + "\">Open in Meerkat</a>"
}

func TestHealthyAddressesDoNotAlert(t *testing.T) {
	f := newAddressFixture(t)
	// 15 days is outside the 14-day warning.
	f.caddy.serve(addressApp, f.in(15*24*time.Hour+time.Hour))
	f.evaluate()
	for range 3 {
		f.next()
	}
	if len(f.telegram.messages()) != 0 || len(f.events(t, "route_error")) != 0 {
		t.Fatalf("healthy addresses alerted: %q %+v", f.texts(), f.events(t, "route_error"))
	}
	// The check really ran: both hostnames were handshaken on each pass, at
	// the address derived from Caddy's metrics address.
	if f.caddy.seen(f.host) != 4 || f.caddy.seen(addressApp) != 4 {
		t.Fatalf("handshakes = %d for the management host, %d for the app", f.caddy.seen(f.host), f.caddy.seen(addressApp))
	}
	for _, address := range f.caddy.dials() {
		if address != "caddy:443" {
			t.Fatalf("dialed %q, want caddy:443", address)
		}
	}
}

func TestFailingAddressAlertsOnTheSecondCheck(t *testing.T) {
	f := newAddressFixture(t)
	f.caddy.remove(addressApp)
	f.evaluate()
	if len(f.telegram.messages()) != 0 || len(f.events(t, "route_error")) != 0 {
		t.Fatalf("alerted on the first failed check: %q", f.texts())
	}

	f.next()
	want := "🔴 <b>Public address failing</b>\nAddress: <code>app.example.com</code>\nProblem: TLS handshake failed: tls: internal error\n" + f.appRef()
	if texts := f.texts(); len(texts) != 1 || texts[0] != want {
		t.Fatalf("messages = %q", texts)
	}
	events := f.events(t, "route_error")
	if len(events) != 1 || events[0].Key != "route_error:tls:app.example.com" || events[0].Subject != addressApp || events[0].ResolvedAt != nil || !events[0].Sent ||
		events[0].Message != "Public address app.example.com failing: TLS handshake failed: tls: internal error" || events[0].Ref != "/connectors/"+f.connector.ID {
		t.Fatalf("events = %+v", events)
	}

	// Rounds between checks and later failed checks leave the open alert alone.
	f.clock.advance(time.Minute)
	f.evaluate()
	f.next()
	if len(f.telegram.messages()) != 1 || len(f.events(t, "route_error")) != 1 {
		t.Fatalf("an open alert was repeated: %q", f.texts())
	}

	// An alerted address is checked again every half hour.
	f.caddy.serve(addressApp, f.in(60*24*time.Hour))
	for range 4 {
		f.next()
	}
	if len(f.telegram.messages()) != 1 {
		t.Fatalf("rechecked before half an hour: %q", f.texts())
	}
	f.next()
	texts := f.texts()
	if len(texts) != 2 || !strings.HasPrefix(texts[1], "🟢 <b>Public address recovered</b>\nAddress: <code>app.example.com</code>\nAlerted: 2026-10-26 14:05 UTC\nOpen for: 31 min") {
		t.Fatalf("recovery = %q", texts)
	}
	if events := f.events(t, "route_error"); events[0].ResolvedAt == nil {
		t.Fatal("event still open")
	}
}

// Caddy gets certificates on demand: every handshake of an address it can't
// get one for may be a failed ACME validation, which Let's Encrypt limits to
// five an hour per name.
func TestAlertedAddressIsCheckedTwiceAnHour(t *testing.T) {
	f := newAddressFixture(t)
	f.caddy.remove(addressApp)
	f.evaluate()
	f.next()
	if len(f.telegram.messages()) != 1 {
		t.Fatalf("messages = %q", f.texts())
	}
	handshakes := f.caddy.seen(addressApp)
	for range 12 {
		f.next()
	}
	if got := f.caddy.seen(addressApp) - handshakes; got != 2 {
		t.Fatalf("handshakes in the hour after the alert = %d, want 2", got)
	}
	if f.caddy.seen(f.host) != 14 {
		t.Fatalf("a healthy address was checked %d times, want every 5 minutes", f.caddy.seen(f.host))
	}
}

// A Caddy that can't be reached fails every address for one reason, and
// says so once.
func TestUnreachableCaddyAlertsOnce(t *testing.T) {
	f := newAddressFixture(t)
	closed, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closed.Close()
	reach := addressDial
	addressDial = func(ctx context.Context, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", closed.Addr().String())
	}
	f.evaluate()
	f.next()
	want := "🔴 <b>Public addresses failing</b>\nCaddy: <code>caddy:443</code>\nProblem: Can&#39;t connect to Caddy: connection refused\n<a href=\"https://" + f.host + "/\">Open in Meerkat</a>"
	if texts := f.texts(); len(texts) != 1 || texts[0] != want {
		t.Fatalf("messages = %q", texts)
	}
	if events := f.events(t, "route_error"); len(events) != 1 || events[0].Key != "route_error:caddy" || events[0].Subject != "caddy:443" {
		t.Fatalf("events = %+v", events)
	}
	// Still unreachable, it is checked every 5 minutes, without more alerts.
	f.next()
	f.next()
	if len(f.telegram.messages()) != 1 {
		t.Fatalf("messages = %q", f.texts())
	}

	addressDial = reach
	f.next()
	texts := f.texts()
	if len(texts) != 2 || !strings.HasPrefix(texts[1], "🟢 <b>Caddy answering again</b>\nCaddy: <code>caddy:443</code>") {
		t.Fatalf("recovery = %q", texts)
	}
	if events := f.events(t, "route_error"); len(events) != 1 || events[0].ResolvedAt == nil {
		t.Fatalf("events = %+v", events)
	}
}

// An address skipped for its half-hourly recheck says nothing about Caddy
// now: when Caddy then stops answering, that is still one alert.
func TestCaddyAlertJudgesOnlyTheAddressesJustChecked(t *testing.T) {
	f := newAddressFixture(t)
	f.caddy.remove(addressApp)
	f.evaluate()
	f.next()
	if len(f.telegram.messages()) != 1 {
		t.Fatalf("messages = %q", f.texts())
	}
	closed, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closed.Close()
	addressDial = func(ctx context.Context, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", closed.Addr().String())
	}
	f.next()
	f.next()
	texts := f.texts()
	if len(texts) != 2 || !strings.HasPrefix(texts[1], "🔴 <b>Public addresses failing</b>\nCaddy: <code>caddy:443</code>") {
		t.Fatalf("messages = %q", texts)
	}
	if events := f.events(t, "route_error"); len(events) != 2 || events[0].Key != "route_error:caddy" || events[1].Key != "route_error:tls:"+addressApp || events[1].ResolvedAt != nil {
		t.Fatalf("events = %+v", events)
	}
}

// A Caddy that takes connections but never answers the handshake is as down
// as one that refuses them, and is checked every 5 minutes.
func TestHungCaddyAlertsOnce(t *testing.T) {
	f := newAddressFixture(t)
	addressTimeout = 50 * time.Millisecond
	reach := addressDial
	addressDial = func(context.Context, string) (net.Conn, error) {
		client, server := net.Pipe()
		t.Cleanup(func() { server.Close() })
		return client, nil
	}
	f.evaluate()
	f.next()
	if texts := f.texts(); len(texts) != 1 || !strings.Contains(texts[0], "Problem: TLS handshake failed: timed out") || len(f.events(t, "route_error")) != 1 {
		t.Fatalf("messages = %q", texts)
	}
	addressDial = reach
	f.next()
	if texts := f.texts(); len(texts) != 2 || !strings.HasPrefix(texts[1], "🟢 <b>Caddy answering again</b>\nCaddy: <code>caddy:443</code>") {
		t.Fatalf("recovery = %q", texts)
	}
}

// An address added while Caddy is down is on its first failed check: it
// doesn't undo the one Caddy alert into one per address.
func TestAddressAddedWhileCaddyIsDownWaitsWithIt(t *testing.T) {
	f := newAddressFixture(t)
	closed, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closed.Close()
	addressDial = func(ctx context.Context, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", closed.Addr().String())
	}
	f.evaluate()
	f.next()
	if _, err := f.db.CreateHTTPRoute(context.Background(), f.service.ID, "new.example.com", false); err != nil {
		t.Fatal(err)
	}
	f.next()
	f.next()
	if events := f.events(t, "route_error"); len(f.telegram.messages()) != 1 || len(events) != 1 || events[0].Key != "route_error:caddy" {
		t.Fatalf("messages = %q, events = %+v", f.texts(), events)
	}
}

// A pass the budget cuts short starts from the addresses checked longest
// ago next time, so none is left unchecked for good.
func TestAddressPassesRotateThroughTheBudget(t *testing.T) {
	f := newAddressFixture(t)
	addressTimeout, addressBudget = 30*time.Millisecond, 50*time.Millisecond
	addressDial = func(ctx context.Context, _ string) (net.Conn, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	var hostnames []string
	for i := range addressWorkers + 4 {
		hostnames = append(hostnames, fmt.Sprintf("h%02d.example.com", i))
	}
	f.store.checkAddresses(context.Background(), hostnames, f.clock.now())
	if checked := len(f.store.alerts.address.checks); checked == len(hostnames) {
		t.Fatalf("the first pass wasn't cut short: %d checked", checked)
	}
	f.clock.advance(addressInterval)
	f.store.checkAddresses(context.Background(), hostnames, f.clock.now())
	if checked := len(f.store.alerts.address.checks); checked != len(hostnames) {
		t.Fatalf("after two passes %d of %d addresses were checked", checked, len(hostnames))
	}
}

func TestAddressFailuresMustBeConsecutive(t *testing.T) {
	f := newAddressFixture(t)
	f.caddy.remove(addressApp)
	f.evaluate()
	f.caddy.serve(addressApp, f.in(60*24*time.Hour))
	f.next()
	f.caddy.remove(addressApp)
	f.next()
	if len(f.telegram.messages()) != 0 {
		t.Fatalf("a failure after a pass counted as the second: %q", f.texts())
	}
	f.next()
	if len(f.telegram.messages()) != 1 {
		t.Fatalf("messages = %q", f.texts())
	}
}

func TestAddressFailureOlderThanTheChecksIsForgotten(t *testing.T) {
	f := newAddressFixture(t)
	f.caddy.remove(addressApp)
	f.evaluate()
	// The evaluator didn't run for a while, as when alerts were off.
	f.clock.advance(addressStale + time.Minute)
	f.evaluate()
	if len(f.telegram.messages()) != 0 {
		t.Fatalf("checks an hour apart counted as consecutive: %q", f.texts())
	}
	f.next()
	if len(f.telegram.messages()) != 1 {
		t.Fatalf("messages = %q", f.texts())
	}
}

func TestExpiringCertificateAlerts(t *testing.T) {
	f := newAddressFixture(t)
	f.caddy.serve(addressApp, f.in(6*24*time.Hour+3*time.Hour))
	f.evaluate()
	f.next()
	want := "🔴 <b>Public address failing</b>\nAddress: <code>app.example.com</code>\nProblem: Certificate expires 2026-11-01 (in 6 days)\n" + f.appRef()
	if texts := f.texts(); len(texts) != 1 || texts[0] != want {
		t.Fatalf("messages = %q", texts)
	}
	// A handshake inside the renewal window renews on demand, so the alerted
	// address is checked every half hour too.
	f.caddy.serve(addressApp, f.in(60*24*time.Hour))
	for range 5 {
		f.next()
	}
	if len(f.telegram.messages()) != 1 {
		t.Fatalf("rechecked before half an hour: %q", f.texts())
	}
	f.next()
	if texts := f.texts(); len(texts) != 2 || !strings.HasPrefix(texts[1], "🟢 <b>Public address recovered</b>") {
		t.Fatalf("recovery = %q", texts)
	}
}

func TestManagementHostAlertLinksToTheConsole(t *testing.T) {
	f := newAddressFixture(t)
	f.caddy.serveAs(f.host, "other.example.com", f.in(60*24*time.Hour))
	f.evaluate()
	f.next()
	want := "🔴 <b>Public address failing</b>\nAddress: <code>" + f.host + "</code>\nProblem: Certificate isn&#39;t valid for this address\n<a href=\"https://" + f.host + "/\">Open in Meerkat</a>"
	if texts := f.texts(); len(texts) != 1 || texts[0] != want {
		t.Fatalf("messages = %q", texts)
	}
	if events := f.events(t, "route_error"); len(events) != 1 || events[0].Subject != f.host || events[0].Ref != "/" {
		t.Fatalf("events = %+v", events)
	}
}

func TestAddressChecksRunEveryFiveMinutes(t *testing.T) {
	f := newAddressFixture(t)
	f.evaluate()
	for minute := 1; minute < 15; minute++ {
		f.clock.advance(time.Minute)
		f.evaluate()
		if want := 1 + minute/5; f.caddy.seen(addressApp) != want {
			t.Fatalf("after %d min the app was handshaken %d times, want %d", minute, f.caddy.seen(addressApp), want)
		}
	}
}

func TestAddressChecksCoverEnabledRoutesOnce(t *testing.T) {
	f := newAddressFixture(t)
	ctx := context.Background()
	// A second service shares app.example.com; two more are switched off,
	// one by its route and one by its service.
	other, err := f.db.CreateService(ctx, f.connector.ID, "api", "http", contract.Target{Host: "api", Port: 3001})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.CreateHTTPRoute(ctx, other.ID, addressApp, true); err != nil {
		t.Fatal(err)
	}
	routeOff, err := f.db.CreateHTTPRoute(ctx, other.ID, "route-off.example.com", false)
	if err != nil {
		t.Fatal(err)
	}
	serviceOff, err := f.db.CreateService(ctx, f.connector.ID, "old", "http", contract.Target{Host: "old", Port: 3002})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.CreateHTTPRoute(ctx, serviceOff.ID, "service-off.example.com", false); err != nil {
		t.Fatal(err)
	}
	for query, args := range map[string][]any{"UPDATE routes SET enabled = 0 WHERE id = ?": {routeOff.ID}, "UPDATE services SET enabled = 0 WHERE id = ?": {serviceOff.ID}} {
		if _, err := f.db.SQL().ExecContext(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	// Direct updates don't invalidate the routing cache; deleting a route does.
	bump, err := f.db.CreateHTTPRoute(ctx, f.service.ID, "bump.example.com", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.db.DeleteHTTPRoute(ctx, f.service.ID, bump.ID); err != nil {
		t.Fatal(err)
	}

	f.evaluate()
	if f.caddy.seen(addressApp) != 1 || f.caddy.seen(f.host) != 1 {
		t.Fatalf("handshakes = %d for the app, %d for the management host", f.caddy.seen(addressApp), f.caddy.seen(f.host))
	}
	for _, name := range []string{"route-off.example.com", "service-off.example.com", "bump.example.com"} {
		if f.caddy.seen(name) != 0 {
			t.Fatalf("%s was checked although nothing serves it", name)
		}
	}
}

func TestAddressRuleOffStopsChecksAndClosesAlerts(t *testing.T) {
	f := newAddressFixture(t)
	f.caddy.remove(addressApp)
	f.evaluate()
	f.next()
	if len(f.telegram.messages()) != 1 {
		t.Fatalf("messages = %q", f.texts())
	}
	seen := f.caddy.seen(addressApp)

	f.setRules(t, func(r *control.AlertRules) { r.RouteError.Enabled = false })
	for range 3 {
		f.next()
	}
	if f.caddy.seen(addressApp) != seen {
		t.Fatal("the address was checked while the rule was off")
	}
	if len(f.telegram.messages()) != 1 {
		t.Fatalf("closing the alert sent %q", f.texts())
	}
	if events := f.events(t, "route_error"); len(events) != 1 || events[0].ResolvedAt == nil {
		t.Fatalf("events = %+v", events)
	}
}

func TestAddressFailuresStartAfreshWhenTheRuleIsSwitchedBackOn(t *testing.T) {
	f := newAddressFixture(t)
	f.caddy.remove(addressApp)
	f.evaluate()
	f.setRules(t, func(r *control.AlertRules) { r.RouteError.Enabled = false })
	f.clock.advance(time.Minute)
	f.evaluate()
	f.setRules(t, func(r *control.AlertRules) {})
	f.clock.advance(addressInterval)
	f.evaluate()
	if len(f.telegram.messages()) != 0 {
		t.Fatalf("a failure from before the rule was off counted: %q", f.texts())
	}
}

func TestOpenAlertSurvivesTheFirstCheckAfterARestart(t *testing.T) {
	f := newAddressFixture(t)
	f.caddy.remove(addressApp)
	f.evaluate()
	f.next()
	// The Outlet restarts: the alert stays open in the database, the
	// results are gone.
	f.store.alerts.address = addressAlertState{}
	f.next()
	if events := f.events(t, "route_error"); len(events) != 1 || events[0].ResolvedAt != nil || len(f.telegram.messages()) != 1 {
		t.Fatalf("events = %+v, messages = %q", events, f.texts())
	}
	// If the address recovered meanwhile, it says so.
	f.store.alerts.address = addressAlertState{}
	f.caddy.serve(addressApp, f.in(60*24*time.Hour))
	f.next()
	if texts := f.texts(); len(texts) != 2 || !strings.HasPrefix(texts[1], "🟢 <b>Public address recovered</b>") {
		t.Fatalf("recovery = %q", texts)
	}
}

func TestRouteLastErrorStillAlertsBesideTheAddressCheck(t *testing.T) {
	f := newAddressFixture(t)
	ctx := context.Background()
	f.caddy.remove(addressApp)
	if _, err := f.db.SQL().ExecContext(ctx, "UPDATE routes SET last_error = ? WHERE id = ?", "tls: <certificate> failed & retrying", f.route.ID); err != nil {
		t.Fatal(err)
	}
	f.evaluate()
	f.evaluate()
	wantRoute := "🔴 <b>Route error</b>\nRoute: <code>app.example.com</code>\nError: tls: &lt;certificate&gt; failed &amp; retrying\n" + f.appRef()
	if texts := f.texts(); len(texts) != 1 || texts[0] != wantRoute {
		t.Fatalf("messages = %q", texts)
	}

	f.next()
	if texts := f.texts(); len(texts) != 2 || !strings.HasPrefix(texts[1], "🔴 <b>Public address failing</b>") || len(f.events(t, "route_error")) != 2 {
		t.Fatalf("messages = %q", texts)
	}

	// Clearing the stored error closes its alert and not the address's.
	if _, err := f.db.SQL().ExecContext(ctx, "UPDATE routes SET last_error = NULL WHERE id = ?", f.route.ID); err != nil {
		t.Fatal(err)
	}
	f.clock.advance(time.Minute)
	f.evaluate()
	texts := f.texts()
	if len(texts) != 3 || !strings.HasPrefix(texts[2], "🟢 <b>Public address recovered</b>\nAddress: <code>app.example.com</code>") {
		t.Fatalf("recovery = %q", texts)
	}
	open := 0
	for _, e := range f.events(t, "route_error") {
		if e.ResolvedAt == nil {
			open++
			if e.Key != "route_error:tls:app.example.com" {
				t.Fatalf("open event = %+v", e)
			}
		}
	}
	if open != 1 {
		t.Fatalf("%d events open, want the address's alone", open)
	}
}

func TestProbeAddressProblems(t *testing.T) {
	f := newAddressFixture(t)
	now := f.clock.now()
	day := 24 * time.Hour
	other := newAddressTestCA()
	f.caddy.serve("good.example.com", now.Add(60*day))
	f.caddy.serve("edge.example.com", now.Add(14*day))
	f.caddy.serve("soon.example.com", now.Add(14*day-time.Minute))
	f.caddy.serve("old.example.com", now.Add(-2*day))
	f.caddy.serveAs("wrong.example.com", "other.example.com", now.Add(60*day))
	f.caddy.mu.Lock()
	f.caddy.certs["strange.example.com"] = other.issue("strange.example.com", now.Add(-day), now.Add(60*day))
	// 45-day certificates are warned about at a sixth of that, 7.5 days.
	f.caddy.certs["short.example.com"] = f.caddy.ca.issue("short.example.com", now.Add(-35*day), now.Add(10*day))
	f.caddy.certs["shorter.example.com"] = f.caddy.ca.issue("shorter.example.com", now.Add(-38*day), now.Add(7*day))
	f.caddy.mu.Unlock()

	for hostname, want := range map[string]string{
		"good.example.com":    "",
		"edge.example.com":    "",
		"soon.example.com":    "Certificate expires 2026-11-09 (in 13 days)",
		"old.example.com":     "Certificate expired 2026-10-24",
		"wrong.example.com":   "Certificate isn't valid for this address",
		"strange.example.com": "Certificate isn't trusted",
		"none.example.com":    "TLS handshake failed: tls: internal error",
		"short.example.com":   "",
		"shorter.example.com": "Certificate expires 2026-11-02 (in 7 days)",
	} {
		// Caddy answered every one of them.
		if got := probeAddress(context.Background(), "caddy:443", hostname, now); got != (addressResult{problem: want}) {
			t.Errorf("%s: %+v, want problem %q", hostname, got, want)
		}
	}
}

func TestProbeAddressUnreachableAndSlow(t *testing.T) {
	f := newAddressFixture(t)
	now := f.clock.now()

	closed, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closed.Close()
	addressDial = func(ctx context.Context, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", closed.Addr().String())
	}
	if got := probeAddress(context.Background(), "caddy:443", addressApp, now); got != (addressResult{problem: "Can't connect to Caddy: connection refused", caddyFault: true}) {
		t.Errorf("refused: %+v", got)
	}

	addressTimeout = 50 * time.Millisecond
	addressDial = func(ctx context.Context, _ string) (net.Conn, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if got := probeAddress(context.Background(), "caddy:443", addressApp, now); got != (addressResult{problem: "Can't connect to Caddy: timed out", caddyFault: true}) {
		t.Errorf("no connection: %+v", got)
	}
	// Connected, but Caddy never answers the ClientHello.
	addressDial = func(context.Context, string) (net.Conn, error) {
		client, server := net.Pipe()
		t.Cleanup(func() { server.Close() })
		return client, nil
	}
	if got := probeAddress(context.Background(), "caddy:443", addressApp, now); got != (addressResult{problem: "TLS handshake failed: timed out", caddyFault: true}) {
		t.Errorf("no answer: %+v", got)
	}
}

func TestAddressPassCutShortCountsNothing(t *testing.T) {
	f := newAddressFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	addressDial = func(call context.Context, _ string) (net.Conn, error) {
		cancel()
		return nil, call.Err()
	}
	f.store.checkAddresses(ctx, []string{f.host, addressApp}, f.clock.now())
	if len(f.store.alerts.address.checks) != 0 {
		t.Fatalf("a pass that was cut short recorded %+v", f.store.alerts.address.checks)
	}
}

func TestCaddyHTTPSAddress(t *testing.T) {
	for metrics, want := range map[string]string{"caddy:9090": "caddy:443", "10.0.0.5:2019": "10.0.0.5:443", "[fd00::5]:9090": "[fd00::5]:443"} {
		if got := caddyHTTPSAddress(metrics); got != want {
			t.Errorf("caddyHTTPSAddress(%q) = %q, want %q", metrics, got, want)
		}
	}
}
