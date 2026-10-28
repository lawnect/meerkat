package outlet

import (
	"fmt"
	"html"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/lawnect/meerkat/internal/control"
)

// Every Telegram message is built here, so they share one layout: an emoji
// and bold title, a few "label: value" lines, then a link to the admin page.

// markup is Telegram HTML whose dynamic parts went through esc or code.
type markup string

func esc(s string) markup { return markup(html.EscapeString(s)) }

func code(s string) markup { return "<code>" + esc(s) + "</code>" }

func codes(values []string) markup {
	parts := make([]string, len(values))
	for i, value := range values {
		parts[i] = string(code(value))
	}
	return markup(strings.Join(parts, ", "))
}

type noticeLine struct {
	label string
	value markup
}

type notice struct {
	icon, title string
	lines       []noticeLine
	footer      string
	// ref is the admin page path; the link is dropped without an admin hostname.
	ref string
	// summary is the plain text kept in the event list.
	summary string
}

func (n notice) line(label string, value markup) notice {
	n.lines = append(slices.Clone(n.lines), noticeLine{label, value})
	return n
}

// HTML renders the message for the Outlet whose admin page is at host.
func (n notice) HTML(host string) string {
	var b strings.Builder
	b.WriteString(n.icon + " <b>" + string(esc(n.title)) + "</b>")
	for _, line := range n.lines {
		b.WriteString("\n" + string(esc(line.label)) + ": " + string(line.value))
	}
	if n.footer != "" {
		b.WriteString("\n" + string(esc(n.footer)))
	}
	if host != "" && n.ref != "" {
		b.WriteString("\n<a href=\"" + string(esc("https://"+host+n.ref)) + "\">Open in Meerkat</a>")
	}
	return b.String()
}

func utc(t time.Time) markup { return esc(t.UTC().Format("2006-01-02 15:04") + " UTC") }

// humanDuration reads "5 min", "2 h 10 min" or "1 d 3 h".
func humanDuration(d time.Duration) string {
	minutes := int(d.Minutes())
	switch {
	case minutes < 1:
		return "under 1 min"
	case minutes < 60:
		return strconv.Itoa(minutes) + " min"
	case minutes < 24*60:
		if minutes%60 == 0 {
			return fmt.Sprintf("%d h", minutes/60)
		}
		return fmt.Sprintf("%d h %d min", minutes/60, minutes%60)
	}
	if hours := minutes / 60 % 24; hours > 0 {
		return fmt.Sprintf("%d d %d h", minutes/(24*60), hours)
	}
	return fmt.Sprintf("%d d", minutes/(24*60))
}

func connectorRef(id string) string { return "/connectors/" + url.PathEscape(id) }

func tunnelDownNotice(name, connectorID string, down time.Duration, lastSeen time.Time) notice {
	n := notice{icon: "🔴", title: "Tunnel disconnected", ref: connectorRef(connectorID),
		summary: "Tunnel " + name + " disconnected (" + humanDuration(down) + ")"}
	n = n.line("Tunnel", code(name)).line("Down for", esc(humanDuration(down)))
	if !lastSeen.IsZero() {
		n = n.line("Last seen", utc(lastSeen))
	}
	return n
}

// serviceDownNotice says which side fails, as the console's service status
// does: the Connector can't reach the app, or the Outlet's request through
// the tunnel gets no answer or a 5xx.
func serviceDownNotice(tunnel, service, connectorID, status string, down time.Duration) notice {
	problem := "Not checked yet"
	switch status {
	case "error":
		problem = "The Connector can't reach the app"
	case "connecting":
		problem = "The check through the tunnel fails"
	}
	n := notice{icon: "🔴", title: "Service down", ref: connectorRef(connectorID),
		summary: "Service " + service + " on " + tunnel + " down (" + humanDuration(down) + "): " + problem}
	return n.line("Service", code(service)).line("Tunnel", code(tunnel)).line("Problem", esc(problem)).line("Down for", esc(humanDuration(down)))
}

func routeErrorNotice(hostname, connectorID, failure string) notice {
	failure = strings.Join(strings.Fields(failure), " ")
	if runes := []rune(failure); len(runes) > 160 {
		failure = string(runes[:160]) + "…"
	}
	return notice{icon: "🔴", title: "Route error", ref: connectorRef(connectorID),
		summary: "Route " + hostname + " failed: " + failure}.
		line("Route", code(hostname)).line("Error", esc(failure))
}

// dnsFailureNotice lists at most three names so the message stays short.
func dnsFailureNotice(domain string, names []string) notice {
	shown, more := names, 0
	if len(shown) > 3 {
		shown, more = shown[:3], len(shown)-3
	}
	list := codes(shown)
	if more > 0 {
		list += markup(fmt.Sprintf(" +%d more", more))
	}
	return notice{icon: "🌐", title: "DNS lookup failing", ref: "/domains/" + url.PathEscape(domain),
		summary: "DNS lookup failing for " + domain + ": " + strings.Join(shown, ", ")}.
		line("Domain", code(domain)).line("Names", list)
}

func errorSpikeNotice(hour time.Time, failed, requests int64) notice {
	rate := strconv.FormatFloat(float64(failed)*100/float64(requests), 'f', 1, 64) + "%"
	detail := fmt.Sprintf("%s (%d of %d requests)", rate, failed, requests)
	return notice{icon: "🟠", title: "5xx spike", ref: "/",
		summary: "5xx error rate " + detail + " in the hour of " + string(utc(hour))}.
		line("Error rate", esc(detail)).line("Hour", utc(hour))
}

func loginNotice(email, ip string, local bool, agent string, at time.Time) notice {
	n := notice{icon: "🔐", title: "Admin login", ref: "/"}.line("Account", code(email))
	summary := "Admin login: " + email
	switch {
	case local:
		n, summary = n.line("Via", esc("local admin port")), summary+" via local admin port"
	case ip != "":
		n, summary = n.line("From", code(ip)), summary+" from "+ip
	default:
		n, summary = n.line("From", esc("unknown")), summary+" from unknown address"
	}
	if agent != "" {
		n, summary = n.line("Browser", esc(agent)), summary+" ("+agent+")"
	}
	n.summary = summary
	return n.line("Time", utc(at))
}

func loginCodeNotice(secret string, ttl time.Duration) notice {
	n := notice{icon: "🔑", title: "Login code", footer: "If this wasn't you, change your password."}
	return n.line("Code", code(secret)).line("Expires in", esc(humanDuration(ttl)))
}

func testNotice(host string, at time.Time) notice {
	n := notice{icon: "✅", title: "Test notification", ref: "/", summary: "Test notification"}
	if host != "" {
		n = n.line("Outlet", code(host))
	}
	return n.line("Time", utc(at))
}

// recoveryNotice announces that an open event cleared. Events of other kinds
// have no recovery message.
func recoveryNotice(e control.AlertEvent, now time.Time) (notice, bool) {
	n := notice{icon: "🟢", ref: e.Ref}
	var label string
	switch e.Kind {
	case "tunnel_down":
		n.title, label = "Tunnel reconnected", "Tunnel"
	case "service_down":
		n.title, label = "Service recovered", "Service"
	case "disk_low":
		n.title, label = "Disk space recovered", "Disk"
	case "memory_low":
		n.title, label = "Memory recovered", "Memory"
	case "route_error":
		n.title, label = "Route error cleared", "Route"
	case "dns_failure":
		n.title, label = "DNS lookup recovered", "Domain"
	default:
		return notice{}, false
	}
	n = n.line(label, code(e.Subject))
	if opened, err := time.Parse(time.RFC3339, e.OpenedAt); err == nil {
		n = n.line("Alerted", utc(opened)).line("Open for", esc(humanDuration(now.Sub(opened))))
	}
	return n, true
}

func countedCodes(items []counted) markup {
	parts := make([]string, len(items))
	for i, item := range items {
		parts[i] = string(code(item.Key)) + " (" + strconv.Itoa(item.N) + ")"
	}
	return markup(strings.Join(parts, ", "))
}

// rateLimitNotice names the limits that tripped and the busiest clients and endpoints.
func rateLimitNotice(summary rejectionSummary, window time.Duration) notice {
	tripped := make([]string, len(summary.Limits))
	for i, item := range summary.Limits {
		tripped[i] = item.Key + " " + strconv.Itoa(item.N)
	}
	detail := fmt.Sprintf("%d in %s (%s)", summary.Total, humanDuration(window), strings.Join(tripped, ", "))
	n := notice{icon: "🛑", title: "Requests rate limited", ref: "/", summary: "Blocked " + strings.Replace(detail, " in ", " requests in ", 1)}
	n = n.line("Blocked", esc(detail))
	for _, top := range []struct {
		label string
		items []counted
	}{{"Top IPs", summary.IPs}, {"Top subnets", summary.Subnets}, {"Top endpoints", summary.Endpoints}} {
		if len(top.items) != 0 {
			n = n.line(top.label, countedCodes(top.items))
		}
	}
	return n
}
