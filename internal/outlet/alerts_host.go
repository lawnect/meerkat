package outlet

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/lawnect/meerkat/internal/contract"
	"github.com/lawnect/meerkat/internal/control"
	"github.com/lawnect/meerkat/internal/securefs"
)

const (
	// runMarkerFile sits in the data directory while an Outlet runs. Close
	// removes it, so finding one at start means the last run was cut off: a
	// crash, an OOM kill, power loss.
	runMarkerFile = "running.json"
	// restartNewsAge is how long an unexpected restart stays worth telling.
	restartNewsAge = time.Hour
	// recoveryMargin is how far above its threshold a reading must climb
	// before the alert clears, so a reading that hovers there doesn't flap.
	recoveryMargin = 2
	// memoryRounds is how many rounds in a row memory must be low to alert,
	// so a short spike doesn't.
	memoryRounds = 3
)

// hostAlertState is the Outlet restart, disk and memory rules', kept
// between rounds.
type hostAlertState struct {
	// restart is an unexpected restart not told yet.
	restart *restartNews
	// lowMemory counts the rounds in a row memory was low; the last was at lowAt.
	lowMemory int
	lowAt     time.Time
}

// runMarker is what a running Outlet leaves in runMarkerFile.
type runMarker struct {
	StartedAt time.Time `json:"started_at"`
	Version   string    `json:"version"`
}

// restartNews is a restart the previous run's marker gave away.
type restartNews struct {
	// previous is empty when the marker couldn't be read.
	previous runMarker
	// started is when this run began.
	started time.Time
}

// freeSpace is how much of a disk or of memory is left, in bytes.
type freeSpace struct{ free, total uint64 }

func (f freeSpace) percent() float64 { return float64(f.free) * 100 / float64(f.total) }

func (f freeSpace) below(percent int) bool { return f.percent() < float64(percent) }

// String reads "6% (3.1 GB of 50 GB)". The percentage rounds down, so it is
// under the rule's whenever the rule alerts.
func (f freeSpace) String() string {
	return strconv.Itoa(int(f.percent())) + "% (" + sizeText(f.free) + " of " + sizeText(f.total) + ")"
}

// memoryReading is memory use as the Outlet's container sees it.
type memoryReading struct {
	freeSpace
	// limited means the container's limit applies, not the host's memory.
	limited bool
}

// These read the machine; tests replace them.
var (
	// diskUsage reads the filesystem holding path.
	diskUsage = readDisk
	// memoryUsage fails with errors.ErrUnsupported where memory isn't read.
	memoryUsage = readMemory
)

// markRun notes whether the last run stopped cleanly and marks this one. A
// marker that can't be written never stops the Outlet: it only costs the
// next start its restart alert.
func (s *Store) markRun(dir string) {
	path := filepath.Join(dir, runMarkerFile)
	started := s.now()
	var previous runMarker
	switch _, err := os.Lstat(path); {
	case err == nil:
		if err := securefs.ReadJSON(path, &previous); err != nil {
			slog.Warn("the marker of the last run is unreadable", "error", err)
		}
		slog.Warn("the last run didn't stop cleanly", "started", previous.StartedAt, "version", previous.Version)
		s.alerts.host.restart = &restartNews{previous, started}
	case !errors.Is(err, fs.ErrNotExist):
		slog.Warn("can't tell whether the last run stopped cleanly", "error", err)
	}
	if err := securefs.WriteJSON(path, runMarker{started.UTC(), contract.Version}); err != nil {
		slog.Warn("can't mark this run, so a crash won't be reported at the next start", "error", err)
		return
	}
	s.runMarkerPath = path
}

// unmarkRun is called when the Outlet stops cleanly.
func (s *Store) unmarkRun() {
	if s.runMarkerPath == "" {
		return
	}
	if err := os.Remove(s.runMarkerPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
		slog.Warn("can't remove the marker of this run, so the next start will report a crash", "error", err)
	}
}

// restartAlerts tells of an unexpected restart once, in the first round that
// can. News that waits too long for alerts to be set up is stale, and a rule
// that is off at that round never hears it.
func (s *Store) restartAlerts(ctx context.Context, env alertEnv, now time.Time) error {
	news := s.alerts.host.restart
	if news == nil {
		return nil
	}
	if !env.rules.OutletRestart.Enabled || now.Sub(news.started) > restartNewsAge {
		s.alerts.host.restart = nil
		return nil
	}
	n := restartNotice(news)
	// A crash loop pages once an hour, like any flapping alert.
	e, opened, err := s.control.RecordAlert(ctx, control.AlertEvent{Key: "outlet_restart", Kind: "outlet_restart", Subject: "Outlet", Message: n.summary, Ref: n.ref}, alertCooldown, now)
	if err != nil {
		return err
	}
	s.alerts.host.restart = nil
	if opened {
		s.deliver(ctx, env, e.ID, n)
	}
	return nil
}

func restartNotice(news *restartNews) notice {
	n := notice{icon: "🔴", title: "Outlet restarted unexpectedly", ref: "/", summary: "Outlet restarted unexpectedly"}
	if !news.previous.StartedAt.IsZero() {
		n = n.line("Previous start", utc(news.previous.StartedAt))
		n.summary += " (previous start " + string(utc(news.previous.StartedAt)) + ")"
	}
	return n.line("Version", code(contract.Version)).line("Running since", utc(news.started))
}

// diskAlerts watches the filesystem holding the Outlet's data. It alerts below
// the rule's free space and clears two points above it; in between the alert
// stays as it is.
func (s *Store) diskAlerts(ctx context.Context, env alertEnv, now time.Time) error {
	rule := env.rules.DiskLow
	if !rule.Enabled {
		return s.syncAlerts(ctx, env, "disk_low", nil, nil, nil, now)
	}
	path := filepath.Dir(s.path)
	disk, err := diskUsage(path)
	if err != nil {
		return err
	}
	var active []condition
	healthy, held := map[string]bool{}, map[string]bool{}
	switch {
	case disk.below(rule.MinFreePercent):
		active = append(active, condition{"disk_low", path, diskLowNotice(path, disk)})
	case disk.below(rule.MinFreePercent + recoveryMargin):
		held["disk_low"] = true
	default:
		healthy["disk_low"] = true
	}
	return s.syncAlerts(ctx, env, "disk_low", active, healthy, held, now)
}

func diskLowNotice(path string, disk freeSpace) notice {
	return notice{icon: "🔴", title: "Disk space low", ref: "/",
		summary: "Disk space low on " + path + ": " + disk.String() + " free"}.
		line("Free", esc(disk.String())).line("Path", code(path))
}

// memoryAlerts is the disk rule's with a wait: a spike has to last
// memoryRounds rounds.
func (s *Store) memoryAlerts(ctx context.Context, env alertEnv, now time.Time) error {
	rule := env.rules.MemoryLow
	state := &s.alerts.host
	if !rule.Enabled {
		state.lowMemory = 0
		return s.syncAlerts(ctx, env, "memory_low", nil, nil, nil, now)
	}
	memory, err := memoryUsage()
	if errors.Is(err, errors.ErrUnsupported) {
		return s.syncAlerts(ctx, env, "memory_low", nil, nil, nil, now)
	}
	if err != nil {
		return err
	}
	var active []condition
	healthy, held := map[string]bool{}, map[string]bool{}
	switch {
	case memory.below(rule.MinFreePercent):
		// Rounds skipped while alerts were off don't count as in a row.
		if now.Sub(state.lowAt) > 2*alertInterval {
			state.lowMemory = 0
		}
		state.lowMemory++
		state.lowAt = now
		if state.lowMemory >= memoryRounds {
			active = append(active, condition{"memory_low", memory.limit(), memoryLowNotice(memory)})
		} else {
			// Not yet long enough to open an alert, nor to close one.
			held["memory_low"] = true
		}
	case memory.below(rule.MinFreePercent + recoveryMargin):
		state.lowMemory = 0
		held["memory_low"] = true
	default:
		state.lowMemory = 0
		healthy["memory_low"] = true
	}
	return s.syncAlerts(ctx, env, "memory_low", active, healthy, held, now)
}

func (m memoryReading) limit() string {
	if m.limited {
		return "Container limit"
	}
	return "Host memory"
}

func memoryLowNotice(memory memoryReading) notice {
	return notice{icon: "🔴", title: "Memory low", ref: "/",
		summary: "Memory low: " + memory.String() + " free (" + strings.ToLower(memory.limit()) + ")"}.
		line("Free", esc(memory.String())).line("Limit", esc(memory.limit()))
}

var byteUnits = []string{"B", "KB", "MB", "GB", "TB", "PB"}

// sizeText reads "140 MB" or "3.1 GB": powers of 1024, one decimal below 10.
func sizeText(n uint64) string {
	if n < 1024 {
		return strconv.FormatUint(n, 10) + " B"
	}
	value := float64(n)
	for unit := 1; ; unit++ {
		value /= 1024
		shown := float64(int64(value + 0.5))
		if value < 10 {
			shown = float64(int64(value*10+0.5)) / 10
		}
		// 1023.7 MB would show as 1024 MB: it is 1 GB.
		if shown < 1024 || unit == len(byteUnits)-1 {
			return strconv.FormatFloat(shown, 'f', -1, 64) + " " + byteUnits[unit]
		}
	}
}

// Linux has memory in /proc and in the cgroup of the container, whose limit
// is what Docker enforces; reading them takes files, so memoryFrom works on
// whatever read returns for a path, and says false for a missing file.
const (
	procMeminfo = "/proc/meminfo"
	cgroupV2    = "/sys/fs/cgroup/"
	cgroupV1    = "/sys/fs/cgroup/memory/"
)

// memoryFrom reads the container's limit when there is one, else the host's
// memory. A limit of the host's memory or more isn't one: the host runs out first.
func memoryFrom(read func(path string) (string, bool)) (memoryReading, error) {
	text, _ := read(procMeminfo)
	available, haveAvailable := statNumber(text, "MemAvailable:")
	total, haveTotal := statNumber(text, "MemTotal:")
	if !haveAvailable || !haveTotal || total == 0 {
		return memoryReading{}, errors.New("host memory can't be read")
	}
	host := freeSpace{available * 1024, total * 1024}
	if limit, usage, ok := cgroupMemory(read); ok && limit < host.total {
		return memoryReading{freeSpace{limit - min(usage, limit), limit}, true}, nil
	}
	return memoryReading{freeSpace: host}, nil
}

// cgroupMemory returns the container's memory limit and what counts as used
// of it, as `docker stats` does: the cache the kernel can drop isn't use.
// It's false without a limit.
func cgroupMemory(read func(path string) (string, bool)) (limit, usage uint64, ok bool) {
	// cgroup v2 keeps one directory for the container; v1 one per controller.
	limitFile, usageFile, statFile, inactiveKey := cgroupV2+"memory.max", cgroupV2+"memory.current", cgroupV2+"memory.stat", "inactive_file"
	if _, found := read(limitFile); !found {
		limitFile, usageFile, statFile, inactiveKey = cgroupV1+"memory.limit_in_bytes", cgroupV1+"memory.usage_in_bytes", cgroupV1+"memory.stat", "total_inactive_file"
	}
	readCount := func(path string) (uint64, bool) {
		text, found := read(path)
		n, err := strconv.ParseUint(strings.TrimSpace(text), 10, 64)
		return n, found && err == nil
	}
	// v2 says "max" for no limit, which isn't a number.
	limit, haveLimit := readCount(limitFile)
	current, haveCurrent := readCount(usageFile)
	stat, _ := read(statFile)
	inactive, _ := statNumber(stat, inactiveKey)
	return limit, workingSet(current, inactive), haveLimit && haveCurrent && limit > 0
}

func workingSet(usage, inactive uint64) uint64 {
	if inactive < usage {
		return usage - inactive
	}
	return usage
}

// statNumber finds the number after key in lines like "MemTotal:  16384 kB" or
// "inactive_file 4096".
func statNumber(text, key string) (uint64, bool) {
	for line := range strings.Lines(text) {
		if f := strings.Fields(line); len(f) >= 2 && f[0] == key {
			n, err := strconv.ParseUint(f[1], 10, 64)
			return n, err == nil
		}
	}
	return 0, false
}
