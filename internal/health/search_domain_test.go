package health

import (
	"context"
	"encoding/binary"
	"errors"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lawnect/meerkat/internal/contract"
)

// fakeDNS is a UDP DNS server on loopback that answers A queries from a table
// of absolute names and NXDOMAIN for everything else. Nothing leaves the host.
type fakeDNS struct {
	conn    *net.UDPConn
	mu      sync.Mutex
	records map[string]net.IP
	asked   []string
}

func newFakeDNS(t *testing.T, records map[string]net.IP) *fakeDNS {
	t.Helper()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	s := &fakeDNS{conn: conn, records: records}
	t.Cleanup(func() { conn.Close() })
	go s.serve()
	return s
}

func (s *fakeDNS) serve() {
	buf := make([]byte, 512)
	for {
		n, from, err := s.conn.ReadFromUDP(buf)
		if err != nil {
			return
		}
		if reply := s.answer(buf[:n]); reply != nil {
			s.conn.WriteToUDP(reply, from)
		}
	}
}

// answer builds the reply to one single-question query.
func (s *fakeDNS) answer(query []byte) []byte {
	if len(query) < 17 {
		return nil
	}
	var labels []string
	i := 12
	for i < len(query) && query[i] != 0 {
		end := i + 1 + int(query[i])
		if end > len(query) {
			return nil
		}
		labels = append(labels, string(query[i+1:end]))
		i = end
	}
	if i+5 > len(query) {
		return nil
	}
	name := strings.ToLower(strings.Join(labels, ".")) + "."
	qtype := binary.BigEndian.Uint16(query[i+1:])
	question := query[12 : i+5]
	s.mu.Lock()
	s.asked = append(s.asked, name)
	ip, found := s.records[name]
	s.mu.Unlock()
	reply := append([]byte{}, query[:2]...)
	flags := uint16(0x8180) // response, recursion desired and available
	if !found {
		flags |= 3 // NXDOMAIN
	}
	answers := uint16(0)
	if found && qtype == 1 && ip.To4() != nil {
		answers = 1
	}
	reply = binary.BigEndian.AppendUint16(reply, flags)
	reply = binary.BigEndian.AppendUint16(reply, 1)
	reply = binary.BigEndian.AppendUint16(reply, answers)
	reply = append(reply, 0, 0, 0, 0)
	reply = append(reply, question...)
	if answers == 1 {
		reply = append(reply, 0xc0, 0x0c, 0, 1, 0, 1, 0, 0, 0, 30, 0, 4)
		reply = append(reply, ip.To4()...)
	}
	return reply
}

func (s *fakeDNS) queried() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.asked...)
}

// searchResolver stands for Go's resolver on a host whose resolv.conf lists
// search domains: a relative name with fewer dots than ndots (1) is tried in
// each search domain before as given; an absolute name is sent as it is. The
// test cannot change /etc/resolv.conf, so the expansion is spelled out here and
// the questions go to the real Go DNS client against the fake server.
type searchResolver struct {
	search []string
	dns    *net.Resolver
}

func newSearchResolver(server *fakeDNS, search ...string) searchResolver {
	return searchResolver{search: search, dns: &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "udp", server.conn.LocalAddr().String())
	}}}
}

func (r searchResolver) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	if strings.HasSuffix(host, ".") {
		return r.dns.LookupIPAddr(ctx, host)
	}
	for _, domain := range r.search {
		if addrs, err := r.dns.LookupIPAddr(ctx, host+"."+domain+"."); err == nil {
			return addrs, nil
		}
	}
	return r.dns.LookupIPAddr(ctx, host+".")
}

func useResolver(t *testing.T, r lookuper) {
	t.Helper()
	old := resolver
	resolver = r
	t.Cleanup(func() { resolver = old })
}

// listenCounting accepts connections on loopback and counts them.
func listenCounting(t *testing.T) (port int, accepted *atomic.Int32) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	accepted = new(atomic.Int32)
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			accepted.Add(1)
			conn.Close()
		}
	}()
	return l.Addr().(*net.TCPAddr).Port, accepted
}

// A cloud host resolves `ip-10-0-1-23` through its search domain to the VPC
// address. A target of that name must stay unresolvable, so neither the tunnel
// nor the health check (an open/closed port oracle) reaches into the VPC. The
// fake search-domain record points at loopback, standing in for the VPC address
// without sending a packet off the machine.
func TestSearchDomainNeverResolvesATargetName(t *testing.T) {
	port, accepted := listenCounting(t)
	dns := newFakeDNS(t, map[string]net.IP{
		"ip-10-0-1-23.corp.internal.": net.IPv4(127, 0, 0, 1),
		"app.":                        net.IPv4(127, 0, 0, 1),
	})
	useResolver(t, newSearchResolver(dns, "corp.internal"))
	ctx := context.Background()

	hostile := contract.Target{Host: "ip-10-0-1-23", Port: port}
	if address, err := Resolve(ctx, hostile); err == nil {
		t.Fatalf("search-domain name resolved to %s", address)
	}
	if state := Check(ctx, hostile); state != "unhealthy" {
		t.Fatalf("search-domain target checked as %q", state)
	}
	if n := accepted.Load(); n != 0 {
		t.Fatalf("health check connected to the search-domain address %d times", n)
	}

	// A Compose service name is answered as an absolute name and keeps working.
	service := contract.Target{Host: "app", Port: port}
	address, err := Resolve(ctx, service)
	if err != nil || address != net.JoinHostPort("127.0.0.1", strconv.Itoa(port)) {
		t.Fatalf("service name: %q %v", address, err)
	}
	if state := Check(ctx, service); state != "healthy" {
		t.Fatalf("service target checked as %q", state)
	}
	for _, name := range dns.queried() {
		if name == "ip-10-0-1-23.corp.internal." {
			t.Fatalf("the search-domain name %q was asked", name)
		}
	}
}

// The questions that reach DNS are absolute, so a search list in resolv.conf
// has no say whichever resolver answers.
func TestTargetQuestionsAreAbsolute(t *testing.T) {
	dns := newFakeDNS(t, map[string]net.IP{"app.": net.IPv4(127, 0, 0, 1)})
	useResolver(t, &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "udp", dns.conn.LocalAddr().String())
	}})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := Resolve(ctx, contract.Target{Host: "app", Port: 80}); err != nil {
		t.Fatal(err)
	}
	asked := dns.queried()
	if len(asked) == 0 {
		t.Fatal("no DNS question seen")
	}
	for _, name := range asked {
		if name != "app." {
			t.Fatalf("question %q is not the bare absolute target name", name)
		}
	}
}

// The system resolver still answers localhost as an absolute name.
func TestLocalhostResolvesAsAbsoluteName(t *testing.T) {
	address, err := Resolve(context.Background(), contract.Target{Host: "localhost", Port: 3000})
	if err != nil {
		t.Fatal(err)
	}
	host, _, _ := net.SplitHostPort(address)
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		t.Fatalf("localhost pinned to %q", address)
	}
}

// noDNS answers NXDOMAIN to everything.
type noDNS struct{}

func (noDNS) LookupIPAddr(context.Context, string) ([]net.IPAddr, error) {
	return nil, errors.New("no such host")
}

func useHosts(t *testing.T, content string) {
	t.Helper()
	path := t.TempDir() + "/hosts"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	old := hostsFile
	hostsFile = path
	t.Cleanup(func() { hostsFile = old })
}

// A DNS server that knows no `localhost.` (Docker's, with some upstreams)
// doesn't take localhost away, with or without a hosts line for it.
func TestLocalhostNeedsNoDNS(t *testing.T) {
	useResolver(t, noDNS{})
	for _, hosts := range []string{"", "127.0.0.1\tlocalhost\n::1 localhost ip6-localhost\n"} {
		useHosts(t, hosts)
		for _, name := range []string{"localhost"} {
			address, err := Resolve(context.Background(), contract.Target{Host: name, Port: 3000})
			if err != nil || address != "127.0.0.1:3000" {
				t.Fatalf("%s with hosts %q pinned to %q: %v", name, hosts, address, err)
			}
		}
	}
}

// Names the operator wrote in the hosts file (--add-host, extra_hosts) answer
// without a dot too, and a public address there is refused like any other.
func TestHostsFileNamesResolve(t *testing.T) {
	useResolver(t, noDNS{})
	useHosts(t, "# local services\n10.0.0.7 nas.lan NAS Storage. # the NAS\n fe80::1%eth0 linklocal\n203.0.113.9 outside\nnot-an-address broken\n")
	for name, want := range map[string]string{"nas": "10.0.0.7:445", "storage": "10.0.0.7:445"} {
		address, err := Resolve(context.Background(), contract.Target{Host: name, Port: 445})
		if err != nil || address != want {
			t.Fatalf("%s pinned to %q: %v", name, address, err)
		}
	}
	for _, name := range []string{"outside", "broken", "the"} {
		if address, err := Resolve(context.Background(), contract.Target{Host: name, Port: 445}); err == nil {
			t.Fatalf("%s pinned to %q", name, address)
		}
	}
}
