package outlet

import (
	"context"
	"testing"
	"time"

	"github.com/lawnect/meerkat/internal/control"
)

// The access log never holds up a request: with it on, a refused request
// only adds an entry to a queue, and one let in anonymously adds nothing.
//
//	go test ./internal/outlet -run '^$' -bench AccessLog -benchtime 2s
func BenchmarkAccessLog(b *testing.B) {
	for _, c := range []struct {
		name   string
		action string
		path   string
		want   int
	}{
		{"blocked", control.AccessBlock, "/internal/x", 403},
		{"sign-in", control.AccessAllow, "/workspace", 401},
		{"anonymous bypass", control.AccessBypass, "/v1/x", 200},
	} {
		for _, logging := range []bool{false, true} {
			name := c.name + "/log off"
			if logging {
				name = c.name + "/log on"
			}
			b.Run(name, func(b *testing.B) {
				p := newPublicBench(b, 0)
				policy := control.AccessPolicy{Name: "p", Enabled: true, Action: c.action, Hosts: []string{"app.example.com"}, AllPaths: true}
				if c.action == control.AccessAllow {
					policy.AllowEveryone = true
				}
				ctx := context.Background()
				if _, err := p.db.SetAccessPolicies(ctx, control.AccessPolicies{Policies: []control.AccessPolicy{policy}}, time.Now()); err != nil {
					b.Fatal(err)
				}
				if _, err := p.db.SetAccessProvider(ctx, control.AccessProvider{Enabled: true, Issuer: "https://idp.example.com", ClientID: "access", ClientSecret: "access-secret-1234", Scopes: []string{"openid", "email"}, SessionHours: 8}, time.Now()); err != nil {
					b.Fatal(err)
				}
				if err := p.store.reloadAccess(ctx); err != nil {
					b.Fatal(err)
				}
				settings := control.DefaultLogSettings(control.LogAccess)
				settings.Enabled = logging
				p.store.logs.access.apply(settings)
				p.run(b, "app.example.com", c.path, c.want)
				b.StopTimer()
				p.store.logs.access.writer.Flush()
				b.ReportMetric(float64(p.store.logs.access.writer.Dropped()), "dropped")
			})
		}
	}
}
