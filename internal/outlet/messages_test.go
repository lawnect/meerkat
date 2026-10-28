package outlet

import (
	"strings"
	"testing"
	"time"

	"github.com/lawnect/meerkat/internal/control"
)

func TestNoticeSnapshots(t *testing.T) {
	const host = "k7m2x9qa.example.com"
	at := time.Date(2026, 10, 26, 14, 7, 0, 0, time.UTC)
	seen := time.Date(2026, 10, 26, 14, 2, 0, 0, time.UTC)
	tests := []struct {
		name string
		n    notice
		host string
		want string
	}{
		{"tunnel down", tunnelDownNotice("home-server", "m_abc", 5*time.Minute, seen), host, `🔴 <b>Tunnel disconnected</b>
Tunnel: <code>home-server</code>
Down for: 5 min
Last seen: 2026-10-26 14:02 UTC
<a href="https://k7m2x9qa.example.com/connectors/m_abc">Open in Meerkat</a>`},
		{"tunnel down without last seen or host", tunnelDownNotice("home-server", "m_abc", 130*time.Minute, time.Time{}), "", `🔴 <b>Tunnel disconnected</b>
Tunnel: <code>home-server</code>
Down for: 2 h 10 min`},
		{"route error escapes the failure", routeErrorNotice("app.example.com", "m_abc", "x509: <bad> & \"quoted\"\n  retry"), host, `🔴 <b>Route error</b>
Route: <code>app.example.com</code>
Error: x509: &lt;bad&gt; &amp; &#34;quoted&#34; retry
<a href="https://k7m2x9qa.example.com/connectors/m_abc">Open in Meerkat</a>`},
		{"dns failure shortens the list", dnsFailureNotice("example.com", []string{"a.example.com", "b.example.com", "c.example.com", "d.example.com", "*.example.com"}), host, `🌐 <b>DNS lookup failing</b>
Domain: <code>example.com</code>
Names: <code>a.example.com</code>, <code>b.example.com</code>, <code>c.example.com</code> +2 more
<a href="https://k7m2x9qa.example.com/domains/example.com">Open in Meerkat</a>`},
		{"error spike", errorSpikeNotice(time.Date(2026, 10, 26, 14, 0, 0, 0, time.UTC), 25, 200), host, `🟠 <b>5xx spike</b>
Error rate: 12.5% (25 of 200 requests)
Hour: 2026-10-26 14:00 UTC
<a href="https://k7m2x9qa.example.com/">Open in Meerkat</a>`},
		{"login from the internet", loginNotice("admin@example.com", "203.0.113.7", false, "Chrome on macOS", at), host, `🔐 <b>Admin login</b>
Account: <code>admin@example.com</code>
From: <code>203.0.113.7</code>
Browser: Chrome on macOS
Time: 2026-10-26 14:07 UTC
<a href="https://k7m2x9qa.example.com/">Open in Meerkat</a>`},
		{"login on the local port", loginNotice("admin@example.com", "127.0.0.1", true, "", at), host, `🔐 <b>Admin login</b>
Account: <code>admin@example.com</code>
Via: local admin port
Time: 2026-10-26 14:07 UTC
<a href="https://k7m2x9qa.example.com/">Open in Meerkat</a>`},
		{"login code", loginCodeNotice("123456", 5*time.Minute), host, `🔑 <b>Login code</b>
Code: <code>123456</code>
Expires in: 5 min
If this wasn&#39;t you, change your password.`},
		{"test", testNotice(host, at), host, `✅ <b>Test notification</b>
Outlet: <code>k7m2x9qa.example.com</code>
Time: 2026-10-26 14:07 UTC
<a href="https://k7m2x9qa.example.com/">Open in Meerkat</a>`},
	}
	for _, tc := range tests {
		if got := tc.n.HTML(tc.host); got != tc.want {
			t.Errorf("%s:\n got: %s\nwant: %s", tc.name, got, tc.want)
		}
	}
}

func TestRecoveryNoticeSnapshots(t *testing.T) {
	now := time.Date(2026, 10, 26, 14, 19, 0, 0, time.UTC)
	event := func(kind, subject, ref string) control.AlertEvent {
		return control.AlertEvent{Kind: kind, Subject: subject, Ref: ref, OpenedAt: "2026-10-26T14:07:00Z"}
	}
	tests := []struct{ name, want string }{
		{"tunnel_down", `🟢 <b>Tunnel reconnected</b>
Tunnel: <code>home-server</code>
Alerted: 2026-10-26 14:07 UTC
Open for: 12 min
<a href="https://k7m2x9qa.example.com/connectors/m_abc">Open in Meerkat</a>`},
		{"route_error", `🟢 <b>Public address recovered</b>
Address: <code>home-server</code>
Alerted: 2026-10-26 14:07 UTC
Open for: 12 min
<a href="https://k7m2x9qa.example.com/connectors/m_abc">Open in Meerkat</a>`},
		{"dns_failure", `🟢 <b>DNS lookup recovered</b>
Domain: <code>home-server</code>
Alerted: 2026-10-26 14:07 UTC
Open for: 12 min
<a href="https://k7m2x9qa.example.com/connectors/m_abc">Open in Meerkat</a>`},
	}
	for _, tc := range tests {
		n, ok := recoveryNotice(event(tc.name, "home-server", "/connectors/m_abc"), now)
		if got := n.HTML("k7m2x9qa.example.com"); !ok || got != tc.want {
			t.Errorf("%s:\n got: %s\nwant: %s", tc.name, got, tc.want)
		}
	}
	if _, ok := recoveryNotice(event("login", "x", "/"), now); ok {
		t.Fatal("login events have no recovery message")
	}
}

func TestNoticeSummariesArePlainText(t *testing.T) {
	at := time.Date(2026, 10, 26, 14, 7, 0, 0, time.UTC)
	for notice, want := range map[string]string{
		tunnelDownNotice("home-server", "m_abc", 5*time.Minute, at).summary:                   "Tunnel home-server disconnected (5 min)",
		routeErrorNotice("app.example.com", "m_abc", "tls <failure>").summary:                 "Route app.example.com failed: tls <failure>",
		dnsFailureNotice("example.com", []string{"a.example.com"}).summary:                    "DNS lookup failing for example.com: a.example.com",
		errorSpikeNotice(at.Truncate(time.Hour), 25, 200).summary:                             "5xx error rate 12.5% (25 of 200 requests) in the hour of 2026-10-26 14:00 UTC",
		loginNotice("admin@example.com", "203.0.113.7", false, "Chrome on macOS", at).summary: "Admin login: admin@example.com from 203.0.113.7 (Chrome on macOS)",
		loginNotice("admin@example.com", "", true, "", at).summary:                            "Admin login: admin@example.com via local admin port",
		loginNotice("admin@example.com", "", false, "", at).summary:                           "Admin login: admin@example.com from unknown address",
		testNotice("", at).HTML(""):                                                           "✅ <b>Test notification</b>\nTime: 2026-10-26 14:07 UTC",
	} {
		if notice != want {
			t.Errorf("got %q, want %q", notice, want)
		}
	}
}

func TestHumanDuration(t *testing.T) {
	for d, want := range map[time.Duration]string{
		30 * time.Second: "under 1 min", 5 * time.Minute: "5 min", 59 * time.Minute: "59 min", time.Hour: "1 h",
		130 * time.Minute: "2 h 10 min", 25 * time.Hour: "1 d 1 h", 48 * time.Hour: "2 d",
	} {
		if got := humanDuration(d); got != want {
			t.Errorf("humanDuration(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestUserAgentSummary(t *testing.T) {
	for agent, want := range map[string]string{
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36":                   "Chrome on macOS",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Safari/605.1.15":                   "Safari on macOS",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36 Edg/126.0.0.0":           "Edge on Windows",
		"Mozilla/5.0 (X11; Linux x86_64; rv:127.0) Gecko/20100101 Firefox/127.0":                                                                  "Firefox on Linux",
		"Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Mobile/15E148 Safari/604.1": "Safari on iOS",
		"Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Mobile Safari/537.36":                   "Chrome on Android",
		"curl/8.7.1":    "curl",
		"":              "",
		"custom-client": "",
	} {
		if got := userAgentSummary(agent); got != want {
			t.Errorf("userAgentSummary(%q) = %q, want %q", agent, got, want)
		}
	}
}

func TestNoticeEscapesEveryDynamicValue(t *testing.T) {
	hostile := `<b>"&'`
	for _, n := range []notice{
		tunnelDownNotice(hostile, hostile, time.Minute, time.Now()),
		routeErrorNotice(hostile, hostile, hostile),
		dnsFailureNotice(hostile, []string{hostile}),
		loginNotice(hostile, hostile, false, hostile, time.Now()),
	} {
		out := n.HTML(hostile)
		if strings.Contains(out, hostile) || strings.Count(out, "<b>") != 1 {
			t.Errorf("unescaped value in %q", out)
		}
	}
}
