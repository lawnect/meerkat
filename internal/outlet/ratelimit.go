package outlet

import (
	"container/list"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"path"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lawnect/meerkat/internal/control"
)

const (
	limiterShards   = 64
	limiterCapacity = 100_000
	// An untouched bucket is full again long before this, so dropping it changes nothing.
	limiterIdle = 10 * time.Minute

	rejectionWindow = 10 * time.Minute
	rejectionSlots  = 10
	// rejectionKeys bounds the clients remembered per minute for the alert.
	rejectionKeys = 500
	topCount      = 3
	// rateAlertEvery spaces alerts while a flood continues.
	rateAlertEvery = 30 * time.Minute
)

// bucket is a token bucket. The rate and burst come from the current
// settings on every use, so an edit applies at once.
type bucket struct {
	key      string
	tokens   float64
	refilled time.Time
	seen     time.Time
}

func (b *bucket) refill(now time.Time, rps, burst float64) {
	if elapsed := now.Sub(b.refilled).Seconds(); elapsed > 0 {
		b.tokens += elapsed * rps
		b.refilled = now
	}
	b.tokens = min(b.tokens, burst)
}

// bucketShard holds buckets in recency order; the back is the idlest.
type bucketShard struct {
	mu    sync.Mutex
	items map[string]*list.Element
	order *list.List
}

// bucketStore is memory-bounded: idle buckets expire and, past the capacity,
// the least recently used one goes first.
type bucketStore struct {
	shards   []bucketShard
	perShard int
	idle     time.Duration
}

func newBucketStore(shards, capacity int, idle time.Duration) *bucketStore {
	store := &bucketStore{shards: make([]bucketShard, shards), perShard: max(capacity/shards, 1), idle: idle}
	for i := range store.shards {
		store.shards[i].items, store.shards[i].order = map[string]*list.Element{}, list.New()
	}
	return store
}

func (s *bucketShard) expire(now time.Time, idle time.Duration) {
	for back := s.order.Back(); back != nil && now.Sub(back.Value.(*bucket).seen) >= idle; back = s.order.Back() {
		delete(s.items, back.Value.(*bucket).key)
		s.order.Remove(back)
	}
}

func (s *bucketShard) get(key string, now time.Time, burst float64, store *bucketStore) *bucket {
	s.expire(now, store.idle)
	if element, ok := s.items[key]; ok {
		s.order.MoveToFront(element)
		found := element.Value.(*bucket)
		found.seen = now
		return found
	}
	fresh := &bucket{key: key, tokens: burst, refilled: now, seen: now}
	s.items[key] = s.order.PushFront(fresh)
	for s.order.Len() > store.perShard {
		oldest := s.order.Back()
		delete(s.items, oldest.Value.(*bucket).key)
		s.order.Remove(oldest)
	}
	return fresh
}

// limit asks for one token from the bucket named key.
type limit struct {
	key        string
	rps, burst float64
}

func shardOf(key string, shards int) int {
	hash := uint32(2166136261)
	for i := 0; i < len(key); i++ {
		hash = (hash ^ uint32(key[i])) * 16777619
	}
	return int(hash % uint32(shards))
}

// take removes one token from every bucket, or from none: a request that one
// bucket rejects must not drain the others. It returns the index of the first
// empty bucket, or -1. Shards lock in ascending order, so callers never deadlock.
func (s *bucketStore) take(now time.Time, limits []limit) int {
	var owner [4]int
	var locked [4]int
	held := locked[:0]
	for i, l := range limits {
		owner[i] = shardOf(l.key, len(s.shards))
		at := sort.SearchInts(held, owner[i])
		if at < len(held) && held[at] == owner[i] {
			continue
		}
		held = append(held, 0)
		copy(held[at+1:], held[at:])
		held[at] = owner[i]
	}
	for _, index := range held {
		s.shards[index].mu.Lock()
	}
	defer func() {
		for _, index := range held {
			s.shards[index].mu.Unlock()
		}
	}()
	var fetched [4]*bucket
	denied := -1
	for i, l := range limits {
		fetched[i] = s.shards[owner[i]].get(l.key, now, l.burst, s)
		fetched[i].refill(now, l.rps, l.burst)
		if denied < 0 && fetched[i].tokens < 1 {
			denied = i
		}
	}
	if denied < 0 {
		for i := range limits {
			fetched[i].tokens--
		}
	}
	return denied
}

// sweep frees idle buckets of shards no request has touched lately.
func (s *bucketStore) sweep(now time.Time) {
	for i := range s.shards {
		shard := &s.shards[i]
		shard.mu.Lock()
		shard.expire(now, s.idle)
		shard.mu.Unlock()
	}
}

func (s *bucketStore) size() int {
	total := 0
	for i := range s.shards {
		s.shards[i].mu.Lock()
		total += s.shards[i].order.Len()
		s.shards[i].mu.Unlock()
	}
	return total
}

type endpointRule struct {
	control.EndpointLimit
	label string
}

// rateConfig is the compiled, immutable form of the settings.
type rateConfig struct {
	control.RateLimitSettings
	rules []endpointRule
}

// compileRateLimit orders the rules so the first match is the right one: the
// longest path prefix, and a named host before "*".
func compileRateLimit(settings control.RateLimitSettings) *rateConfig {
	config := &rateConfig{RateLimitSettings: settings}
	for _, e := range settings.Endpoints {
		config.rules = append(config.rules, endpointRule{e, e.Host + e.PathPrefix})
	}
	sort.SliceStable(config.rules, func(i, j int) bool {
		a, b := config.rules[i], config.rules[j]
		if len(a.PathPrefix) != len(b.PathPrefix) {
			return len(a.PathPrefix) > len(b.PathPrefix)
		}
		return a.Host != "*" && b.Host == "*"
	})
	return config
}

// match finds the endpoint rule for a request. Paths are cleaned and
// lowercased first, so "//Login/" cannot slip past a "/login" rule.
func (c *rateConfig) match(host, requestPath string) *endpointRule {
	requestPath = strings.ToLower(path.Clean("/" + requestPath))
	for i := range c.rules {
		if (c.rules[i].Host == "*" || c.rules[i].Host == host) && strings.HasPrefix(requestPath, c.rules[i].PathPrefix) {
			return &c.rules[i]
		}
	}
	return nil
}

type counted struct {
	Key string
	N   int
}

func topCounts(m map[string]int, n int) []counted {
	all := make([]counted, 0, len(m))
	for key, count := range m {
		all = append(all, counted{key, count})
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].N != all[j].N {
			return all[i].N > all[j].N
		}
		return all[i].Key < all[j].Key
	})
	return all[:min(n, len(all))]
}

func tally(m map[string]int, key string) {
	if _, known := m[key]; known || len(m) < rejectionKeys {
		m[key]++
	}
}

type rejectionSlot struct {
	minute                          int64
	total                           int
	limits, ips, subnets, endpoints map[string]int
}

// rejection describes one request answered with 429.
type rejection struct{ limit, ip, subnet, endpoint string }

type rejectionSummary struct {
	Total                           int
	Limits, IPs, Subnets, Endpoints []counted
}

// rejectionStats remembers the last ten minutes for the alert and counts
// rejections per hour until they are written to the database.
type rejectionStats struct {
	mu    sync.Mutex
	slots [rejectionSlots]rejectionSlot
	hours map[time.Time]int64
}

func (r *rejectionStats) record(now time.Time, hit rejection) {
	minute := now.Unix() / 60
	r.mu.Lock()
	defer r.mu.Unlock()
	slot := &r.slots[minute%rejectionSlots]
	if slot.minute != minute || slot.limits == nil {
		*slot = rejectionSlot{minute: minute, limits: map[string]int{}, ips: map[string]int{}, subnets: map[string]int{}, endpoints: map[string]int{}}
	}
	slot.total++
	tally(slot.limits, hit.limit)
	if hit.ip != "" {
		tally(slot.ips, hit.ip)
		tally(slot.subnets, hit.subnet)
	}
	if hit.endpoint != "" {
		tally(slot.endpoints, hit.endpoint)
	}
	if r.hours == nil {
		r.hours = map[time.Time]int64{}
	}
	r.hours[now.UTC().Truncate(time.Hour)]++
}

func (r *rejectionStats) recent(now time.Time) rejectionSummary {
	minute := now.Unix() / 60
	limits, ips, subnets, endpoints := map[string]int{}, map[string]int{}, map[string]int{}, map[string]int{}
	var summary rejectionSummary
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.slots {
		slot := &r.slots[i]
		if slot.limits == nil || slot.minute > minute || slot.minute <= minute-rejectionSlots {
			continue
		}
		summary.Total += slot.total
		for _, pair := range []struct{ into, from map[string]int }{{limits, slot.limits}, {ips, slot.ips}, {subnets, slot.subnets}, {endpoints, slot.endpoints}} {
			for key, n := range pair.from {
				pair.into[key] += n
			}
		}
	}
	summary.Limits, summary.IPs, summary.Subnets, summary.Endpoints = topCounts(limits, 4), topCounts(ips, topCount), topCounts(subnets, topCount), topCounts(endpoints, topCount)
	return summary
}

// drain takes the counts not yet stored.
func (r *rejectionStats) drain() map[time.Time]int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	pending := r.hours
	r.hours = nil
	return pending
}

func (r *rejectionStats) restore(pending map[time.Time]int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.hours == nil {
		r.hours = map[time.Time]int64{}
	}
	for hour, n := range pending {
		r.hours[hour] += n
	}
}

// rateLimiter is the state of the request flood protection.
type rateLimiter struct {
	buckets *bucketStore
	config  atomic.Pointer[rateConfig]
	stats   rejectionStats
}

func newRateLimiter() *rateLimiter {
	return &rateLimiter{buckets: newBucketStore(limiterShards, limiterCapacity, limiterIdle)}
}

// subnetOf is the IPv4 /24 or IPv6 /64 holding ip.
func subnetOf(ip string) string {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return ""
	}
	addr = addr.Unmap()
	bits := 64
	if addr.Is4() {
		bits = 24
	}
	prefix, err := addr.Prefix(bits)
	if err != nil {
		return ""
	}
	return prefix.String()
}

// exemptFromRateLimit keeps certificate issuance working under any limit.
func exemptFromRateLimit(requestPath string) bool {
	return requestPath == certificateAskPath || strings.HasPrefix(requestPath, "/.well-known/acme-challenge/")
}

func (s *Store) reloadRateLimit(ctx context.Context) error {
	if s.control == nil || s.limiter == nil {
		return nil
	}
	outlet, err := s.control.Outlet(ctx)
	if err != nil {
		return err
	}
	settings, err := s.control.RateLimit(ctx, outlet.AdminHostname)
	if err != nil {
		return err
	}
	s.limiter.config.Store(compileRateLimit(settings))
	return nil
}

// limitRequest answers 429 when a public request is over a limit and reports
// whether it did. It needs no database, so a flood is turned away before any
// other work. The address is the one Caddy forwarded; without it the
// address-based buckets are skipped rather than shared by every client.
func (s *Store) limitRequest(w http.ResponseWriter, r *http.Request, host string) bool {
	limiter := s.limiter
	if limiter == nil {
		return false
	}
	config := limiter.config.Load()
	if config == nil || !config.Enabled || exemptFromRateLimit(r.URL.Path) {
		return false
	}
	ip, _ := requestOrigin(r)
	subnet := ""
	var limits [4]limit
	var kinds [4]string
	count := 0
	add := func(kind, key string, l control.Limit) {
		limits[count], kinds[count] = limit{key: key, rps: float64(l.RPS), burst: float64(l.Burst)}, kind
		count++
	}
	if ip != "" {
		if config.PerIP.On() {
			add("ip", "ip|"+ip, config.PerIP)
		}
		if subnet = subnetOf(ip); subnet != "" && config.PerSubnet.On() {
			add("subnet", "net|"+subnet, config.PerSubnet)
		}
	}
	if config.PerAddress.On() {
		add("address", "host|"+host, config.PerAddress)
	}
	endpoint := ""
	if rule := config.match(host, r.URL.Path); rule != nil {
		key := "ep|" + rule.label
		if rule.Per == control.RateEndpointIP {
			key = "epi|" + rule.label + "|" + ip
		}
		if rule.Per == control.RateEndpointTotal || ip != "" {
			add("endpoint", key, control.Limit{RPS: rule.RPS, Burst: rule.Burst})
			endpoint = rule.label
		}
	}
	now := s.now()
	denied := limiter.buckets.take(now, limits[:count])
	if denied < 0 {
		return false
	}
	hit := rejection{limit: kinds[denied], ip: ip, subnet: subnet}
	if kinds[denied] == "endpoint" {
		hit.endpoint = endpoint
	}
	limiter.stats.record(now, hit)
	w.Header().Set("Retry-After", "1")
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusTooManyRequests)
	_, _ = io.WriteString(w, "Too many requests ("+kinds[denied]+" limit)\n")
	return true
}

// RunRateLimits writes the rejection counts to the usage table and frees idle
// buckets. Counts of the last seconds are flushed on shutdown.
func (s *Store) RunRateLimits(ctx context.Context) {
	if s.limiter == nil || s.control == nil {
		return
	}
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			final, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			s.flushRejections(final)
			cancel()
			return
		case <-ticker.C:
			s.limiter.buckets.sweep(s.now())
			s.flushRejections(ctx)
		}
	}
}

func (s *Store) flushRejections(ctx context.Context) {
	pending := s.limiter.stats.drain()
	failed := map[time.Time]int64{}
	for hour, n := range pending {
		if err := s.control.AddRateLimited(ctx, hour, n); err != nil {
			failed[hour] = n
			slog.Warn("rate limit counts not stored", "error", err)
		}
	}
	if len(failed) != 0 {
		s.limiter.stats.restore(failed)
	}
}

func (s *Store) getRateLimit(w http.ResponseWriter, r *http.Request) {
	if !s.alertsAdmin(w, r) {
		return
	}
	outlet, err := s.control.Outlet(r.Context())
	if err != nil {
		fail(w, r, 503, "persistence_failed")
		return
	}
	settings, err := s.control.RateLimit(r.Context(), outlet.AdminHostname)
	if err != nil {
		fail(w, r, 503, "persistence_failed")
		return
	}
	reply(w, 200, settings)
}

func (s *Store) putRateLimit(w http.ResponseWriter, r *http.Request) {
	if !s.alertsAdmin(w, r) {
		return
	}
	var req control.RateLimitSettings
	if !decode(w, r, &req, "enabled", "per_ip", "per_subnet", "per_address", "endpoints") {
		return
	}
	saved, err := s.control.SetRateLimit(r.Context(), req, s.now())
	if errors.Is(err, control.ErrInvalidRateLimit) {
		fail(w, r, 400, "invalid_rate_limit")
		return
	}
	if err != nil {
		fail(w, r, 503, "persistence_failed")
		return
	}
	if err := s.reloadRateLimit(r.Context()); err != nil {
		fail(w, r, 503, "persistence_failed")
		return
	}
	raw, _ := json.Marshal(saved)
	var detail map[string]any
	_ = json.Unmarshal(raw, &detail)
	if s.audited(w, r, "security.rate_limit", "security", "rate_limit", detail) {
		reply(w, 200, saved)
	}
}

// rateLimitAlerts reports a flood once per rateAlertEvery while it lasts.
func (s *Store) rateLimitAlerts(ctx context.Context, env alertEnv, now time.Time) error {
	rule := env.rules.RateLimit
	if !rule.Enabled || s.limiter == nil {
		return nil
	}
	summary := s.limiter.stats.recent(now)
	if summary.Total < rule.MinBlocked {
		return nil
	}
	n := rateLimitNotice(summary, rejectionWindow)
	e, opened, err := s.control.RecordAlert(ctx, control.AlertEvent{Key: "rate_limit", Kind: "rate_limit", Subject: "requests", Message: n.summary, Ref: n.ref}, rateAlertEvery, now)
	if err != nil || !opened {
		return err
	}
	s.deliver(ctx, env, e.ID, n)
	return nil
}
