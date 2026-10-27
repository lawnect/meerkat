package outlet

import (
	"container/list"
	"context"
	"errors"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lawnect/meerkat/internal/control"
	"github.com/lawnect/meerkat/internal/eventlog"
)

const (
	// maxBans bounds the table of banned clients: when it is full the oldest
	// ban goes first, so a flood of clients can fill memory no further.
	maxBans = 65536
	// maxBanPath is the longest path a ban keeps of the request that earned
	// it.
	maxBanPath = 512

	defaultBanPage = 20
	maxBanPage     = 200
	// maxBanQuery is the longest text the bans are searched for.
	maxBanQuery = 128
)

// ban refuses a client everything on the public addresses until a time.
// Everything but hits is set when the ban is made and never changes.
type ban struct {
	key    netip.Prefix // who: an IPv4 address, or the IPv6 /64 it is in
	client string       // key as listed: the address, or the "/64" prefix
	rule   string       // ID of the block rule that was matched
	host   string       // what the request that earned it asked for
	path   string
	since  time.Time
	until  time.Time
	hits   atomic.Int64 // requests refused since

	element *list.Element // its place in the order; guarded by banState.mu
}

// banConfig is the saved settings in the form requests use them.
type banConfig struct {
	duration time.Duration
	exempt   []netip.Prefix
}

func (c *banConfig) exempts(addr netip.Addr) bool {
	for _, prefix := range c.exempt {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

// covers reports whether an exempt range holds the whole of a ban's range.
func (c *banConfig) covers(key netip.Prefix) bool {
	for _, prefix := range c.exempt {
		if prefix.Bits() <= key.Bits() && prefix.Contains(key.Addr()) {
			return true
		}
	}
	return false
}

// banState holds the clients banned by block rules, in memory only: a
// restart clears them. The zero value is ready to use.
//
// Requests only read it: with no bans they cost the load of held, otherwise
// a lookup in byKey, which takes no lock and writes nothing shared, so
// requests on every core look clients up without queueing. Changes, rare
// next to requests, hold mu and keep byKey and order in step.
type banState struct {
	config atomic.Pointer[banConfig]
	// held is how many bans the table holds; with none a request skips the
	// table at the cost of this load.
	held atomic.Int64

	byKey sync.Map // netip.Prefix to *ban

	mu    sync.Mutex
	order list.List // of *ban, oldest first
}

// banKey is who a ban holds for a client address: the IPv4 address, or the
// /64 an IPv6 one is in, which is what a provider hands one customer at the
// least. It allocates nothing. An empty or unreadable address has no key.
func banKey(ip string) (key netip.Prefix, addr netip.Addr, ok bool) {
	if ip == "" {
		return key, addr, false
	}
	addr, err := netip.ParseAddr(ip)
	if err != nil || addr.Zone() != "" {
		return key, addr, false
	}
	addr = addr.Unmap()
	bits := 64
	if addr.Is4() {
		bits = 32
	}
	key, err = addr.Prefix(bits)
	return key, addr, err == nil
}

// banLabel is how a ban's key is listed and released by.
func banLabel(key netip.Prefix) string {
	if key.Addr().Is4() {
		return key.Addr().String()
	}
	return key.String()
}

// parseBanLabel reads a listed client back: an IPv4 address or an IPv6 /64
// prefix; an IPv6 address stands for its /64.
func parseBanLabel(label string) (netip.Prefix, bool) {
	label = strings.TrimSpace(label)
	if !strings.Contains(label, "/") {
		key, _, ok := banKey(label)
		return key, ok
	}
	prefix, err := netip.ParsePrefix(label)
	if err != nil || !prefix.Addr().Is6() || prefix.Addr().Is4In6() || prefix.Bits() != 64 {
		return netip.Prefix{}, false
	}
	return prefix.Masked(), true
}

func (b *banState) settings() *banConfig {
	if config := b.config.Load(); config != nil {
		return config
	}
	return &banConfig{duration: control.DefaultBanMinutes * time.Minute}
}

// find returns the ban that holds the client of ip at now, or nil. An exempt
// client is never held, even by a ban made for another address in its /64.
func (b *banState) find(ip string, now time.Time) *ban {
	if b.held.Load() == 0 {
		return nil
	}
	key, addr, ok := banKey(ip)
	if !ok {
		return nil
	}
	value, ok := b.byKey.Load(key)
	if !ok {
		return nil
	}
	found := value.(*ban)
	if !now.Before(found.until) {
		b.expire(key, now)
		return nil
	}
	if b.settings().exempts(addr) {
		return nil
	}
	return found
}

// expire drops the ban of key if it has run out.
func (b *banState) expire(key netip.Prefix, now time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if value, ok := b.byKey.Load(key); ok && !now.Before(value.(*ban).until) {
		b.remove(value.(*ban))
	}
}

// remove takes a ban out of the table; the caller holds the lock.
func (b *banState) remove(entry *ban) {
	b.byKey.Delete(entry.key)
	b.order.Remove(entry.element)
	b.held.Store(int64(b.order.Len()))
}

// add bans key until the configured time has passed, unless it is held
// already. When the table is full the oldest ban goes.
func (b *banState) add(key netip.Prefix, rule, host, path string, now time.Time, duration time.Duration) {
	entry := &ban{key: key, client: banLabel(key), rule: rule, host: host, path: eventlog.Clip(path, maxBanPath), since: now, until: now.Add(duration)}
	b.mu.Lock()
	defer b.mu.Unlock()
	if value, ok := b.byKey.Load(key); ok {
		if now.Before(value.(*ban).until) {
			return
		}
		b.remove(value.(*ban))
	}
	for b.order.Len() >= maxBans {
		b.remove(b.order.Front().Value.(*ban))
	}
	entry.element = b.order.PushBack(entry)
	b.byKey.Store(key, entry)
	b.held.Store(int64(b.order.Len()))
}

// sweep drops the bans that have run out. Lookups drop the ones they meet,
// this the rest, so the table does not keep clients that never come back.
func (b *banState) sweep(now time.Time) {
	b.releaseWhere(now, func(*ban) bool { return false })
}

// releaseWhere drops the bans that have run out and those match accepts, and
// returns how many of the latter were still in force.
func (b *banState) releaseWhere(now time.Time, match func(*ban) bool) int {
	if b.held.Load() == 0 {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	released := 0
	for element := b.order.Front(); element != nil; {
		next := element.Next()
		entry := element.Value.(*ban)
		live := now.Before(entry.until)
		if !live || match(entry) {
			b.remove(entry)
			if live {
				released++
			}
		}
		element = next
	}
	return released
}

// release lets one client go, and reports whether it was banned.
func (b *banState) release(key netip.Prefix, now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	value, ok := b.byKey.Load(key)
	if !ok {
		return false
	}
	entry := value.(*ban)
	b.remove(entry)
	return now.Before(entry.until)
}

// banView is a ban as the API lists it.
type banView struct {
	Client string    `json:"client"`
	Rule   string    `json:"rule"`
	Host   string    `json:"host"`
	Path   string    `json:"path"`
	Since  time.Time `json:"since"`
	Until  time.Time `json:"until"`
	Hits   int64     `json:"hits"`
}

// list returns a page of the bans in force, newest first, and how many there
// are; text, lowercase, narrows them to those with it in the client, host or
// path.
func (b *banState) list(now time.Time, text string, skip, limit int) (int, []banView) {
	b.mu.Lock()
	defer b.mu.Unlock()
	total, page := 0, []banView{}
	for element := b.order.Back(); element != nil; element = element.Prev() {
		e := element.Value.(*ban)
		if !now.Before(e.until) || text != "" && !strings.Contains(strings.ToLower(e.client), text) &&
			!strings.Contains(strings.ToLower(e.host), text) && !strings.Contains(strings.ToLower(e.path), text) {
			continue
		}
		if total >= skip && len(page) < limit {
			page = append(page, banView{e.client, e.rule, e.host, e.path, e.since, e.until, e.hits.Load()})
		}
		total++
	}
	return total, page
}

// banClient bans the client of ip for matching rule with a request for host
// and path, unless it can't be told apart or is exempt.
func (s *Store) banClient(ip string, rule *blockRule, host, path string) {
	key, addr, ok := banKey(ip)
	if !ok {
		return
	}
	config := s.bans.settings()
	if config.exempts(addr) {
		return
	}
	s.bans.add(key, rule.id, host, path, s.now(), config.duration)
}

// fromABrowser reports whether a browser sent the request. A trap path
// reaches a browser only by mistake or by someone else's design: a link
// pasted in a chat, a typed address, an image another site embeds. Banning
// there would lock out a person, or everyone behind their address, instead
// of a scanner, so such a request is only refused. Browsers send
// Sec-Fetch-Site with every request (older ones a Referer when they follow
// or embed a link); scanners send neither, and one that forges them is
// still refused, just not banned.
func fromABrowser(r *http.Request) bool {
	return r.Header.Get("Sec-Fetch-Site") != "" || r.Header.Get("Referer") != ""
}

// refuseBanned turns a request away with 403 when its client is banned, and
// reports whether it did. Paths under /.well-known/ are exempt, as they are
// from block rules: certificate checks and service discovery live there.
func (s *Store) refuseBanned(w http.ResponseWriter, r *http.Request, host string, began time.Time, ip string) bool {
	if s.bans.held.Load() == 0 {
		return false
	}
	found := s.bans.find(ip, s.now())
	if found == nil || wellKnown(rulePaths(r.URL.Path)) {
		return false
	}
	found.hits.Add(1)
	blocked(w)
	s.logFailure(r, host, began, failBanned, http.StatusForbidden, "")
	return true
}

func (s *Store) reloadClientBans(ctx context.Context) error {
	if s.control == nil {
		return nil
	}
	settings, err := s.control.ClientBans(ctx)
	if err != nil {
		return err
	}
	s.applyClientBans(settings)
	return nil
}

// applyClientBans takes saved settings: the duration for bans made from now
// on, and the exempt clients, whose bans end at once. Other bans keep the
// time they were made until. It returns how many bans ended.
func (s *Store) applyClientBans(settings control.ClientBanSettings) int {
	config := &banConfig{duration: time.Duration(settings.Minutes) * time.Minute}
	for _, entry := range settings.Exempt {
		if prefix, ok := control.ParseBanExempt(entry); ok {
			config.exempt = append(config.exempt, prefix)
		}
	}
	s.bans.config.Store(config)
	if len(config.exempt) == 0 {
		return 0
	}
	return s.bans.releaseWhere(s.now(), func(e *ban) bool { return config.covers(e.key) })
}

type clientBansView struct {
	Settings control.ClientBanSettings `json:"settings"`
	Total    int                       `json:"total"`
	Bans     []banView                 `json:"bans"`
}

func (s *Store) clientBansView(ctx context.Context, text string, page, perPage int) (clientBansView, error) {
	settings, err := s.control.ClientBans(ctx)
	if err != nil {
		return clientBansView{}, err
	}
	view := clientBansView{Settings: settings}
	view.Total, view.Bans = s.bans.list(s.now(), strings.ToLower(text), (page-1)*perPage, perPage)
	return view, nil
}

// replyClientBans answers like GET for the first page.
func (s *Store) replyClientBans(w http.ResponseWriter, r *http.Request) {
	view, err := s.clientBansView(r.Context(), "", 1, defaultBanPage)
	if err != nil {
		fail(w, r, 503, "persistence_failed")
		return
	}
	reply(w, 200, view)
}

func (s *Store) getClientBans(w http.ResponseWriter, r *http.Request) {
	if !s.alertsAdmin(w, r) {
		return
	}
	query := r.URL.Query()
	page, perPage := 1, defaultBanPage
	for _, p := range []struct {
		name     string
		into     *int
		min, max int
	}{{"page", &page, 1, 1 << 20}, {"per_page", &perPage, 1, maxBanPage}} {
		if value := query.Get(p.name); value != "" {
			n, err := strconv.Atoi(value)
			if err != nil || n < p.min || n > p.max {
				fail(w, r, 400, "invalid_ban_query")
				return
			}
			*p.into = n
		}
	}
	text := query.Get("q")
	if len(text) > maxBanQuery {
		fail(w, r, 400, "invalid_ban_query")
		return
	}
	view, err := s.clientBansView(r.Context(), text, page, perPage)
	if err != nil {
		fail(w, r, 503, "persistence_failed")
		return
	}
	reply(w, 200, view)
}

func (s *Store) putClientBans(w http.ResponseWriter, r *http.Request) {
	if !s.alertsAdmin(w, r) {
		return
	}
	var req control.ClientBanSettings
	if !decode(w, r, &req, "minutes", "exempt") {
		return
	}
	s.saving.Lock()
	saved, err := s.control.SetClientBans(r.Context(), req, s.now())
	if err == nil {
		s.applyClientBans(saved)
	}
	s.saving.Unlock()
	if errors.Is(err, control.ErrInvalidClientBans) {
		fail(w, r, 400, "invalid_client_bans")
		return
	}
	if err != nil {
		fail(w, r, 503, "persistence_failed")
		return
	}
	s.audited(r, "security.client_bans", "security", "client_bans", map[string]any{"minutes": saved.Minutes, "exempt": saved.Exempt})
	s.replyClientBans(w, r)
}

func (s *Store) releaseClientBans(w http.ResponseWriter, r *http.Request) {
	if !s.alertsAdmin(w, r) {
		return
	}
	var req struct {
		Client string `json:"client"`
		All    bool   `json:"all"`
	}
	if !decode(w, r, &req) {
		return
	}
	// One client, or all of them.
	if req.All == (req.Client != "") {
		fail(w, r, 400, "invalid_request")
		return
	}
	now := s.now()
	if req.All {
		count := s.bans.releaseWhere(now, func(*ban) bool { return true })
		s.audited(r, "security.client_ban_release", "security", "client_bans", map[string]any{"all": true, "count": count})
	} else {
		key, ok := parseBanLabel(req.Client)
		if !ok || !s.bans.release(key, now) {
			fail(w, r, 404, "ban_not_found")
			return
		}
		s.audited(r, "security.client_ban_release", "security", "client_bans", map[string]any{"client": banLabel(key), "count": 1})
	}
	s.replyClientBans(w, r)
}
