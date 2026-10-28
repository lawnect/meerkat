package outlet

import (
	"errors"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lawnect/meerkat/internal/contract"
	"github.com/lawnect/meerkat/internal/control"
	"github.com/lawnect/meerkat/internal/securefs"
)

func gibSpace(free, total uint64) freeSpace { return freeSpace{free << 30, total << 30} }

func mibSpace(free, total uint64) freeSpace { return freeSpace{free << 20, total << 20} }

// The suite never reads the machine it runs on: however full its disk or
// memory is, no host alert may fire in a test that isn't about one.
func init() {
	diskUsage = func(string) (freeSpace, error) { return gibSpace(60, 100), nil }
	memoryUsage = func() (memoryReading, error) { return memoryReading{freeSpace: gibSpace(60, 100)}, nil }
}

// fakeHost is what the disk and memory read as; tests change it between rounds.
type fakeHost struct {
	disk      freeSpace
	diskErr   error
	diskPaths []string
	memory    memoryReading
	memoryErr error
}

func hostReadings(t *testing.T) *fakeHost {
	t.Helper()
	h := &fakeHost{disk: gibSpace(60, 100), memory: memoryReading{freeSpace: gibSpace(60, 100)}}
	disk, memory := diskUsage, memoryUsage
	diskUsage = func(path string) (freeSpace, error) {
		h.diskPaths = append(h.diskPaths, path)
		return h.disk, h.diskErr
	}
	memoryUsage = func() (memoryReading, error) { return h.memory, h.memoryErr }
	t.Cleanup(func() { diskUsage, memoryUsage = disk, memory })
	return h
}

// round runs one evaluation a minute after the last.
func (a alertFixture) hostRound() {
	a.evaluate()
	a.clock.advance(time.Minute)
}

func (a alertFixture) hostLink() string {
	return "<a href=\"https://" + a.host + "/\">Open in Meerkat</a>"
}

func (a alertFixture) wantHostSent(t *testing.T, n int) {
	t.Helper()
	if got := a.telegram.messages(); len(got) != n {
		t.Fatalf("messages = %q, want %d", a.texts(), n)
	}
}

func TestDiskLowAlertLifecycle(t *testing.T) {
	a := newAlertFixture(t)
	a.configureTelegram(t, false)
	h := hostReadings(t)

	// Exactly the threshold isn't below it.
	h.disk = gibSpace(10, 100)
	a.hostRound()
	a.wantHostSent(t, 0)

	h.disk = gibSpace(9, 100)
	a.hostRound()
	want := "🔴 <b>Disk space low</b>\nFree: 9% (9 GB of 100 GB)\nPath: <code>" + a.dir + "</code>\n" + a.hostLink()
	if texts := a.texts(); len(texts) != 1 || texts[0] != want {
		t.Fatalf("messages = %q", texts)
	}
	events := a.events(t, "disk_low")
	if len(events) != 1 || events[0].Subject != a.dir || events[0].ResolvedAt != nil || !events[0].Sent || events[0].Message != "Disk space low on "+a.dir+": 9% (9 GB of 100 GB) free" {
		t.Fatalf("events = %+v", events)
	}
	for _, path := range h.diskPaths {
		if path != a.dir {
			t.Fatalf("read %q, want the data directory %q", path, a.dir)
		}
	}

	h.disk = gibSpace(5, 100)
	a.hostRound()
	a.wantHostSent(t, 1)

	// Between the threshold and two points above it the alert stays open.
	h.disk = gibSpace(11, 100)
	a.hostRound()
	a.wantHostSent(t, 1)
	if events := a.events(t, "disk_low"); events[0].ResolvedAt != nil {
		t.Fatalf("closed inside the margin: %+v", events)
	}

	h.disk = gibSpace(12, 100)
	a.hostRound()
	texts := a.texts()
	if len(texts) != 2 || !strings.HasPrefix(texts[1], "🟢 <b>Disk space recovered</b>\nDisk: <code>"+a.dir+"</code>\nAlerted: 2026-10-26 14:01 UTC\nOpen for: 3 min") {
		t.Fatalf("recovery = %q", texts)
	}
	if events := a.events(t, "disk_low"); events[0].ResolvedAt == nil {
		t.Fatalf("event still open: %+v", events)
	}

	// Falling again within the hour doesn't page again.
	h.disk = gibSpace(5, 100)
	a.hostRound()
	a.wantHostSent(t, 2)
}

func TestDiskLowAlertShowsFractionsOfAGigabyte(t *testing.T) {
	a := newAlertFixture(t)
	a.configureTelegram(t, false)
	h := hostReadings(t)
	// 3.1 GB is 6.2% of 50 GB.
	h.disk = freeSpace{free: 3328599654, total: 50 << 30}
	a.hostRound()
	want := "🔴 <b>Disk space low</b>\nFree: 6% (3.1 GB of 50 GB)\nPath: <code>" + a.dir + "</code>\n" + a.hostLink()
	if texts := a.texts(); len(texts) != 1 || texts[0] != want {
		t.Fatalf("messages = %q", texts)
	}
}

func TestDiskLowAlertFollowsItsRule(t *testing.T) {
	a := newAlertFixture(t)
	a.configureTelegram(t, false)
	h := hostReadings(t)
	a.setRules(t, func(r *control.AlertRules) { r.DiskLow.MinFreePercent = 20 })

	h.disk = gibSpace(19, 100)
	a.hostRound()
	a.wantHostSent(t, 1)
	h.disk = gibSpace(21, 100)
	a.hostRound()
	a.wantHostSent(t, 1)
	h.disk = gibSpace(22, 100)
	a.hostRound()
	a.wantHostSent(t, 2)
}

func TestDiskLowAlertSwitchedOff(t *testing.T) {
	a := newAlertFixture(t)
	a.configureTelegram(t, false)
	h := hostReadings(t)
	a.setRules(t, func(r *control.AlertRules) { r.DiskLow.Enabled = false })
	h.disk = gibSpace(1, 100)
	a.hostRound()
	a.wantHostSent(t, 0)
	if len(a.events(t, "disk_low")) != 0 || len(h.diskPaths) != 0 {
		t.Fatalf("a switched-off rule recorded %+v or read the disk", a.events(t, "disk_low"))
	}

	// An alert open when the rule is switched off closes without a recovery message.
	a.setRules(t, func(*control.AlertRules) {})
	a.hostRound()
	a.wantHostSent(t, 1)
	a.setRules(t, func(r *control.AlertRules) { r.DiskLow.Enabled = false })
	a.hostRound()
	a.wantHostSent(t, 1)
	if events := a.events(t, "disk_low"); len(events) != 1 || events[0].ResolvedAt == nil {
		t.Fatalf("events = %+v", events)
	}
}

func TestDiskReadFailureLeavesTheAlertAsItIs(t *testing.T) {
	a := newAlertFixture(t)
	a.configureTelegram(t, false)
	h := hostReadings(t)
	h.disk = gibSpace(5, 100)
	a.hostRound()
	a.wantHostSent(t, 1)

	h.diskErr = errors.New("statfs failed")
	for range 3 {
		a.hostRound()
	}
	a.wantHostSent(t, 1)
	if events := a.events(t, "disk_low"); len(events) != 1 || events[0].ResolvedAt != nil {
		t.Fatalf("events = %+v", events)
	}
}

func TestMemoryLowAlertWaitsForThreeRounds(t *testing.T) {
	a := newAlertFixture(t)
	a.configureTelegram(t, false)
	h := hostReadings(t)
	h.memory = memoryReading{mibSpace(150, 2048), true}

	a.hostRound()
	a.hostRound()
	a.wantHostSent(t, 0)
	a.hostRound()
	want := "🔴 <b>Memory low</b>\nFree: 7% (150 MB of 2 GB)\nLimit: Container limit\n" + a.hostLink()
	if texts := a.texts(); len(texts) != 1 || texts[0] != want {
		t.Fatalf("messages = %q", texts)
	}
	events := a.events(t, "memory_low")
	if len(events) != 1 || events[0].Subject != "Container limit" || events[0].ResolvedAt != nil || !events[0].Sent || events[0].Message != "Memory low: 7% (150 MB of 2 GB) free (container limit)" {
		t.Fatalf("events = %+v", events)
	}
	for range 3 {
		a.hostRound()
	}
	a.wantHostSent(t, 1)
}

func TestMemoryLowAlertNamesTheHost(t *testing.T) {
	a := newAlertFixture(t)
	a.configureTelegram(t, false)
	h := hostReadings(t)
	h.memory = memoryReading{mibSpace(150, 2048), false}
	for range 3 {
		a.hostRound()
	}
	want := "🔴 <b>Memory low</b>\nFree: 7% (150 MB of 2 GB)\nLimit: Host memory\n" + a.hostLink()
	if texts := a.texts(); len(texts) != 1 || texts[0] != want {
		t.Fatalf("messages = %q", texts)
	}
}

// A spike has to last: any round at or above the threshold starts the count again.
func TestMemoryLowAlertIgnoresShortSpikes(t *testing.T) {
	a := newAlertFixture(t)
	a.configureTelegram(t, false)
	h := hostReadings(t)
	for _, between := range []freeSpace{mibSpace(500, 1000), mibSpace(110, 1000)} {
		h.memory = memoryReading{mibSpace(50, 1000), true}
		a.hostRound()
		a.hostRound()
		h.memory = memoryReading{between, true}
		a.hostRound()
	}
	a.wantHostSent(t, 0)
	h.memory = memoryReading{mibSpace(50, 1000), true}
	a.hostRound()
	a.hostRound()
	a.wantHostSent(t, 0)
	a.hostRound()
	a.wantHostSent(t, 1)
}

// Rounds that didn't run (alerts were off) don't count as in a row.
func TestMemoryLowAlertCountsOnlyRoundsInARow(t *testing.T) {
	a := newAlertFixture(t)
	a.configureTelegram(t, false)
	h := hostReadings(t)
	h.memory = memoryReading{mibSpace(50, 1000), true}
	a.hostRound()
	a.hostRound()
	a.clock.advance(10 * time.Minute)
	a.hostRound()
	a.hostRound()
	a.wantHostSent(t, 0)
	a.hostRound()
	a.wantHostSent(t, 1)
}

func TestMemoryLowAlertRecoversWithMargin(t *testing.T) {
	a := newAlertFixture(t)
	a.configureTelegram(t, false)
	h := hostReadings(t)
	h.memory = memoryReading{mibSpace(50, 1000), true}
	for range 3 {
		a.hostRound()
	}
	a.wantHostSent(t, 1)

	// Inside the margin the alert stays open, and a dip back below it is
	// held until it has lasted three rounds, not closed meanwhile.
	h.memory = memoryReading{mibSpace(110, 1000), true}
	a.hostRound()
	h.memory = memoryReading{mibSpace(90, 1000), true}
	a.hostRound()
	a.hostRound()
	a.wantHostSent(t, 1)
	if events := a.events(t, "memory_low"); events[0].ResolvedAt != nil {
		t.Fatalf("closed too early: %+v", events)
	}

	h.memory = memoryReading{mibSpace(120, 1000), true}
	a.hostRound()
	texts := a.texts()
	if len(texts) != 2 || !strings.HasPrefix(texts[1], "🟢 <b>Memory recovered</b>\nMemory: <code>Container limit</code>\nAlerted: 2026-10-26 14:02 UTC\nOpen for: 4 min") {
		t.Fatalf("recovery = %q", texts)
	}
	if events := a.events(t, "memory_low"); events[0].ResolvedAt == nil {
		t.Fatalf("event still open: %+v", events)
	}
}

func TestMemoryLowAlertFollowsItsRule(t *testing.T) {
	a := newAlertFixture(t)
	a.configureTelegram(t, false)
	h := hostReadings(t)
	a.setRules(t, func(r *control.AlertRules) { r.MemoryLow.MinFreePercent = 20 })
	h.memory = memoryReading{mibSpace(150, 1000), true}
	for range 3 {
		a.hostRound()
	}
	a.wantHostSent(t, 1)
	h.memory = memoryReading{mibSpace(210, 1000), true}
	a.hostRound()
	a.wantHostSent(t, 1)
	h.memory = memoryReading{mibSpace(220, 1000), true}
	a.hostRound()
	a.wantHostSent(t, 2)
}

func TestMemoryLowAlertSwitchedOff(t *testing.T) {
	a := newAlertFixture(t)
	a.configureTelegram(t, false)
	h := hostReadings(t)
	h.memory = memoryReading{mibSpace(50, 1000), true}
	a.setRules(t, func(r *control.AlertRules) { r.MemoryLow.Enabled = false })
	for range 5 {
		a.hostRound()
	}
	a.wantHostSent(t, 0)
	if events := a.events(t, "memory_low"); len(events) != 0 {
		t.Fatalf("events = %+v", events)
	}

	// Switching the rule off forgets the rounds counted before.
	a.setRules(t, func(*control.AlertRules) {})
	a.hostRound()
	a.hostRound()
	a.setRules(t, func(r *control.AlertRules) { r.MemoryLow.Enabled = false })
	a.hostRound()
	a.setRules(t, func(*control.AlertRules) {})
	a.hostRound()
	a.wantHostSent(t, 0)
	a.hostRound()
	a.hostRound()
	a.wantHostSent(t, 1)

	a.setRules(t, func(r *control.AlertRules) { r.MemoryLow.Enabled = false })
	a.hostRound()
	a.wantHostSent(t, 1)
	if events := a.events(t, "memory_low"); len(events) != 1 || events[0].ResolvedAt == nil {
		t.Fatalf("events = %+v", events)
	}
}

func TestMemoryLowAlertNeedsAReading(t *testing.T) {
	a := newAlertFixture(t)
	a.configureTelegram(t, false)
	h := hostReadings(t)
	h.memory = memoryReading{mibSpace(50, 1000), true}
	// Where memory isn't read there is never a condition.
	h.memoryErr = errors.ErrUnsupported
	for range 5 {
		a.hostRound()
	}
	a.wantHostSent(t, 0)
	if events := a.events(t, "memory_low"); len(events) != 0 {
		t.Fatalf("events = %+v", events)
	}

	// A reading that fails now and then changes nothing.
	h.memoryErr = nil
	for range 3 {
		a.hostRound()
	}
	a.wantHostSent(t, 1)
	h.memoryErr = errors.New("proc unreadable")
	for range 3 {
		a.hostRound()
	}
	a.wantHostSent(t, 1)
	if events := a.events(t, "memory_low"); len(events) != 1 || events[0].ResolvedAt != nil {
		t.Fatalf("events = %+v", events)
	}
}

// restartedStore starts the Outlet again on the same data directory, as a restart
// does. Whether the previous store was closed decides what it finds there.
func (a alertFixture) restartedStore(t *testing.T) alertFixture {
	t.Helper()
	store, err := OpenWithControl(a.cfg, a.dir, a.db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	store.now, store.sendTelegram, store.lookupIP = a.clock.now, a.telegram.send, a.store.lookupIP
	a.store = store
	return a
}

func readMarker(t *testing.T, dir string) runMarker {
	t.Helper()
	var marker runMarker
	if err := securefs.ReadJSON(filepath.Join(dir, runMarkerFile), &marker); err != nil {
		t.Fatal(err)
	}
	return marker
}

func plantMarker(t *testing.T, dir string, marker runMarker) {
	t.Helper()
	if err := securefs.WriteJSON(filepath.Join(dir, runMarkerFile), marker); err != nil {
		t.Fatal(err)
	}
}

// crashedRun leaves what a run that was killed leaves: its marker.
var crashedRun = runMarker{StartedAt: time.Date(2026, 10, 26, 13, 0, 0, 0, time.UTC), Version: "0.9.0"}

// clockAfterStart moves the clock to the given time after the run began.
func (a alertFixture) clockAfterStart(t *testing.T, d time.Duration) time.Time {
	t.Helper()
	start := readMarker(t, a.dir).StartedAt
	a.clock.mu.Lock()
	a.clock.t = start.Add(d)
	a.clock.mu.Unlock()
	return start
}

func minuteText(t time.Time) string { return t.UTC().Format("2006-01-02 15:04") + " UTC" }

func TestUnexpectedRestartIsAlertedOnce(t *testing.T) {
	a := newAlertFixture(t)
	a.configureTelegram(t, false)
	// The fixture's own store is the previous run, never closed.
	plantMarker(t, a.dir, crashedRun)
	b := a.restartedStore(t)
	start := b.clockAfterStart(t, time.Minute)

	b.evaluate()
	want := "🔴 <b>Outlet restarted unexpectedly</b>\nPrevious start: 2026-10-26 13:00 UTC\nVersion: <code>" + contract.Version + "</code>\nRunning since: " + minuteText(start) + "\n" + b.hostLink()
	if texts := b.texts(); len(texts) != 1 || texts[0] != want {
		t.Fatalf("messages = %q", texts)
	}
	events := b.events(t, "outlet_restart")
	if len(events) != 1 || events[0].Subject != "Outlet" || events[0].Ref != "/" || !events[0].Sent || events[0].ResolvedAt == nil || events[0].Message != "Outlet restarted unexpectedly (previous start 2026-10-26 13:00 UTC)" {
		t.Fatalf("events = %+v", events)
	}

	for range 3 {
		b.hostRound()
	}
	b.wantHostSent(t, 1)
	if b.store.alerts.host.restart != nil {
		t.Fatal("the restart is still news after it was told")
	}
}

func TestCleanStopIsNotAnUnexpectedRestart(t *testing.T) {
	a := newAlertFixture(t)
	a.configureTelegram(t, false)
	a.store.Close()
	if _, err := os.Stat(filepath.Join(a.dir, runMarkerFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the marker outlived a clean stop: %v", err)
	}
	b := a.restartedStore(t)
	b.clockAfterStart(t, time.Minute)
	for range 3 {
		b.hostRound()
	}
	b.wantHostSent(t, 0)
	if events := b.events(t, "outlet_restart"); len(events) != 0 {
		t.Fatalf("events = %+v", events)
	}
}

// A marker that can't be read still says the last run was cut off.
func TestUnexpectedRestartWithUnreadableMarker(t *testing.T) {
	a := newAlertFixture(t)
	a.configureTelegram(t, false)
	if err := os.WriteFile(filepath.Join(a.dir, runMarkerFile), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	b := a.restartedStore(t)
	start := b.clockAfterStart(t, time.Minute)
	b.evaluate()
	want := "🔴 <b>Outlet restarted unexpectedly</b>\nVersion: <code>" + contract.Version + "</code>\nRunning since: " + minuteText(start) + "\n" + b.hostLink()
	if texts := b.texts(); len(texts) != 1 || texts[0] != want {
		t.Fatalf("messages = %q", texts)
	}
	if events := b.events(t, "outlet_restart"); len(events) != 1 || events[0].Message != "Outlet restarted unexpectedly" {
		t.Fatalf("events = %+v", events)
	}
}

func TestUnexpectedRestartWaitsForAlertsToBeSetUp(t *testing.T) {
	a := newAlertFixture(t)
	plantMarker(t, a.dir, crashedRun)
	b := a.restartedStore(t)
	b.clockAfterStart(t, time.Minute)
	b.evaluate()
	b.wantHostSent(t, 0)

	b.clockAfterStart(t, 59*time.Minute)
	b.configureTelegram(t, false)
	b.evaluate()
	b.wantHostSent(t, 1)
	if !strings.HasPrefix(b.texts()[0], "🔴 <b>Outlet restarted unexpectedly</b>") {
		t.Fatalf("messages = %q", b.texts())
	}
}

func TestStaleRestartNewsIsDropped(t *testing.T) {
	a := newAlertFixture(t)
	plantMarker(t, a.dir, crashedRun)
	b := a.restartedStore(t)
	b.clockAfterStart(t, time.Minute)
	b.evaluate()

	// Alerts are set up an hour and a minute after the start.
	b.clockAfterStart(t, time.Hour+time.Minute)
	b.configureTelegram(t, false)
	b.evaluate()
	b.wantHostSent(t, 0)
	if events := b.events(t, "outlet_restart"); len(events) != 0 {
		t.Fatalf("events = %+v", events)
	}
}

func TestUnexpectedRestartNeedsItsRule(t *testing.T) {
	a := newAlertFixture(t)
	a.configureTelegram(t, false)
	a.setRules(t, func(r *control.AlertRules) { r.OutletRestart.Enabled = false })
	plantMarker(t, a.dir, crashedRun)
	b := a.restartedStore(t)
	b.clockAfterStart(t, time.Minute)
	b.evaluate()
	b.wantHostSent(t, 0)

	// Switched on later, it has nothing to tell: the news was dropped.
	b.setRules(t, func(*control.AlertRules) {})
	b.hostRound()
	b.wantHostSent(t, 0)
	if events := b.events(t, "outlet_restart"); len(events) != 0 {
		t.Fatalf("events = %+v", events)
	}
}

// A run that crashes over and over pages once an hour.
func TestUnexpectedRestartsInARowPageOnce(t *testing.T) {
	a := newAlertFixture(t)
	a.configureTelegram(t, false)
	plantMarker(t, a.dir, crashedRun)
	b := a.restartedStore(t)
	b.clockAfterStart(t, time.Minute)
	b.evaluate()
	c := b.restartedStore(t)
	c.clockAfterStart(t, time.Minute)
	c.evaluate()
	c.wantHostSent(t, 1)
	if events := c.events(t, "outlet_restart"); len(events) != 1 {
		t.Fatalf("events = %+v", events)
	}
}

func TestRunMarker(t *testing.T) {
	f := setup(t)
	path := filepath.Join(f.dir, runMarkerFile)
	// Without a control database there are no alerts to tell.
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a store without a control database left a marker: %v", err)
	}
	before := time.Now()
	store, _ := controlStore(t, f)
	after := time.Now()
	marker := readMarker(t, f.dir)
	if marker.Version != contract.Version || marker.StartedAt.Before(before.Add(-time.Second)) || marker.StartedAt.After(after.Add(time.Second)) {
		t.Fatalf("marker = %+v, want version %q started between %v and %v", marker, contract.Version, before, after)
	}
	store.Close()
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the marker outlived Close: %v", err)
	}
}

// Not being able to mark the run costs a crash its alert, never the start.
func TestRunMarkerThatCannotBeWrittenDoesNotStopTheOutlet(t *testing.T) {
	f := setup(t)
	if err := os.Mkdir(filepath.Join(f.dir, runMarkerFile), 0o700); err != nil {
		t.Fatal(err)
	}
	store, _ := controlStore(t, f)
	if store.runMarkerPath != "" {
		t.Fatalf("runMarkerPath = %q", store.runMarkerPath)
	}
	store.Close()
	if info, err := os.Stat(filepath.Join(f.dir, runMarkerFile)); err != nil || !info.IsDir() {
		t.Fatalf("Close removed what it didn't write: %v", err)
	}
}

func TestMemoryFrom(t *testing.T) {
	const meminfo = "MemTotal:       16384000 kB\nMemFree:          100000 kB\nMemAvailable:    8192000 kB\nBuffers:           5000 kB\n"
	v2 := map[string]string{
		"/sys/fs/cgroup/memory.max":     "2147483648\n",
		"/sys/fs/cgroup/memory.current": "1610612736\n",
		"/sys/fs/cgroup/memory.stat":    "anon 1000000\nfile 700000000\ninactive_anon 5\nactive_file 100\ninactive_file 536870912\n",
	}
	v1 := map[string]string{
		"/sys/fs/cgroup/memory/memory.limit_in_bytes": "1073741824\n",
		"/sys/fs/cgroup/memory/memory.usage_in_bytes": "805306368\n",
		"/sys/fs/cgroup/memory/memory.stat":           "cache 1\ninactive_file 4096\ntotal_cache 2\ntotal_inactive_file 268435456\n",
	}
	both := maps.Clone(v2)
	maps.Copy(both, v1)
	with := func(extra map[string]string, edits ...string) map[string]string {
		files := map[string]string{"/proc/meminfo": meminfo}
		for path, text := range extra {
			files[path] = text
		}
		for i := 0; i+1 < len(edits); i += 2 {
			if edits[i+1] == "" {
				delete(files, edits[i])
			} else {
				files[edits[i]] = edits[i+1]
			}
		}
		return files
	}
	host := memoryReading{freeSpace: freeSpace{8192000 * 1024, 16384000 * 1024}}
	for _, c := range []struct {
		name  string
		files map[string]string
		want  memoryReading
	}{
		// docker stats: usage is memory.current less the inactive file cache.
		{"cgroup v2", with(v2), memoryReading{freeSpace{1 << 30, 2 << 30}, true}},
		{"cgroup v2 without a limit", with(v2, "/sys/fs/cgroup/memory.max", "max\n"), host},
		{"cgroup v2 limit above the host's memory", with(v2, "/sys/fs/cgroup/memory.max", "34359738368\n"), host},
		{"cgroup v2 limit equal to the host's memory", with(v2, "/sys/fs/cgroup/memory.max", "16777216000\n"), host},
		{"cgroup v2 without usage", with(v2, "/sys/fs/cgroup/memory.current", ""), host},
		{"cgroup v2 over its limit", with(v2, "/sys/fs/cgroup/memory.current", "9999999999\n", "/sys/fs/cgroup/memory.stat", ""), memoryReading{freeSpace{0, 2 << 30}, true}},
		{"cgroup v2 cache larger than usage", with(v2, "/sys/fs/cgroup/memory.current", "100\n", "/sys/fs/cgroup/memory.stat", "inactive_file 200\n"), memoryReading{freeSpace{2<<30 - 100, 2 << 30}, true}},
		{"cgroup v2 without stat", with(v2, "/sys/fs/cgroup/memory.stat", ""), memoryReading{freeSpace{2<<30 - 1610612736, 2 << 30}, true}},
		{"cgroup v1", with(v1), memoryReading{freeSpace{512 << 20, 1 << 30}, true}},
		{"cgroup v1 without a limit", with(v1, "/sys/fs/cgroup/memory/memory.limit_in_bytes", "9223372036854771712\n"), host},
		{"cgroup v1 without usage", with(v1, "/sys/fs/cgroup/memory/memory.usage_in_bytes", ""), host},
		{"cgroup v2 before v1", with(both), memoryReading{freeSpace{1 << 30, 2 << 30}, true}},
		{"host", with(nil), host},
	} {
		got, err := memoryFrom(func(path string) (string, bool) { text, ok := c.files[path]; return text, ok })
		if err != nil || got != c.want {
			t.Errorf("%s: got %+v, %v; want %+v", c.name, got, err, c.want)
		}
	}

	for name, files := range map[string]map[string]string{
		"no meminfo":             {},
		"no MemAvailable":        {"/proc/meminfo": "MemTotal:       16384000 kB\n"},
		"no MemTotal":            {"/proc/meminfo": "MemAvailable:    8192000 kB\n"},
		"empty MemTotal":         {"/proc/meminfo": "MemTotal: 0 kB\nMemAvailable: 0 kB\n"},
		"garbled meminfo":        {"/proc/meminfo": "MemTotal: lots kB\nMemAvailable: 1 kB\n"},
		"cgroup without meminfo": v2,
	} {
		if got, err := memoryFrom(func(path string) (string, bool) { text, ok := files[path]; return text, ok }); err == nil {
			t.Errorf("%s: got %+v, want an error", name, got)
		}
	}
}

func TestField(t *testing.T) {
	text := "MemTotal:  16384 kB\ninactive_anon 5\ninactive_file 7\nbroken\nbad x\n"
	for key, want := range map[string]uint64{"MemTotal:": 16384, "inactive_file": 7, "inactive_anon": 5} {
		if got, ok := statNumber(text, key); !ok || got != want {
			t.Errorf("statNumber(%q) = %d, %v; want %d", key, got, ok, want)
		}
	}
	for _, key := range []string{"inactive", "Mem", "broken", "bad", "missing"} {
		if got, ok := statNumber(text, key); ok {
			t.Errorf("statNumber(%q) = %d, want none", key, got)
		}
	}
}

func TestHumanBytes(t *testing.T) {
	for _, c := range []struct {
		n    uint64
		want string
	}{
		{0, "0 B"},
		{1023, "1023 B"},
		{1024, "1 KB"},
		{1536, "1.5 KB"},
		{140 << 20, "140 MB"},
		{3328599654, "3.1 GB"},
		{2 << 30, "2 GB"},
		{50 << 30, "50 GB"},
		{(1 << 30) * 104 / 10, "10 GB"},
		{(1 << 30) * 996 / 100, "10 GB"},
		{(1 << 20) * 10237 / 10, "1 GB"},
		{3 << 40, "3 TB"},
		{1<<64 - 1, "16384 PB"},
	} {
		if got := sizeText(c.n); got != c.want {
			t.Errorf("sizeText(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}

// The percentage shown never reaches the threshold when the rule alerts.
func TestFreeSpaceRoundsDown(t *testing.T) {
	f := freeSpace{free: 9999, total: 100000}
	if !f.below(10) || !strings.HasPrefix(f.String(), "9% (") {
		t.Fatalf("%v below 10 = %v", f, f.below(10))
	}
}

func TestReadDisk(t *testing.T) {
	disk, err := readDisk(t.TempDir())
	if err != nil || disk.total == 0 || disk.free > disk.total {
		t.Fatalf("readDisk = %+v, %v", disk, err)
	}
	if _, err := readDisk(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("a missing path was read")
	}
}
