// poc-probe verifies an HTTP response on an Outlet loopback tunnel port. It is
// a test utility, not the production health-check implementation.
package main

import (
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

func main() {
	address := flag.String("url", "http://127.0.0.1:31001/", "loopback HTTP tunnel URL")
	expect := flag.String("expect", "meerkat:app-a", "expected response body")
	flag.Parse()
	u, err := url.Parse(*address)
	if err != nil || u.Scheme != "http" || net.ParseIP(u.Hostname()) == nil || !net.ParseIP(u.Hostname()).IsLoopback() {
		fmt.Fprintln(os.Stderr, "probe requires loopback HTTP")
		os.Exit(1)
	}
	c := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	r, err := c.Get(*address)
	if err != nil {
		fmt.Fprintln(os.Stderr, "tunnel probe failed")
		os.Exit(1)
	}
	defer r.Body.Close()
	b, err := io.ReadAll(io.LimitReader(r.Body, 1024))
	if err != nil || r.StatusCode != 200 || strings.TrimSpace(string(b)) != *expect {
		fmt.Fprintln(os.Stderr, "unexpected tunnel response")
		os.Exit(1)
	}
	fmt.Println(strings.TrimSpace(string(b)))
}
