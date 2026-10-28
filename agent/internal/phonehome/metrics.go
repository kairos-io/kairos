package phonehome

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

// NodeMetrics is the optional live-usage block of a heartbeat. AuroraBoot keeps
// the last samples per node and draws them as gauges and sparklines; a server
// that does not know the field ignores it. Every field is optional: a source
// that cannot be read is left out, it never fails the heartbeat.
type NodeMetrics struct {
	SampledAt     time.Time      `json:"sampledAt"`
	UptimeSeconds uint64         `json:"uptimeSeconds,omitempty"`
	Load          []float64      `json:"load,omitempty"`
	CPU           *CPUMetrics    `json:"cpu,omitempty"`
	Memory        *MemoryMetrics `json:"memory,omitempty"`
	Disks         []DiskMetrics  `json:"disks,omitempty"`
	TemperatureC  *float64       `json:"temperatureC,omitempty"`
}

// CPUMetrics is the share of CPU time spent busy since the previous heartbeat.
type CPUMetrics struct {
	UsedPercent float64 `json:"usedPercent"`
}

// MemoryMetrics uses MemAvailable, not MemFree, so page cache does not read as
// used memory.
type MemoryMetrics struct {
	TotalBytes     uint64 `json:"totalBytes"`
	AvailableBytes uint64 `json:"availableBytes"`
}

// DiskMetrics is the usage of one Kairos partition.
type DiskMetrics struct {
	Label      string `json:"label,omitempty"`
	Mount      string `json:"mount"`
	TotalBytes uint64 `json:"totalBytes"`
	UsedBytes  uint64 `json:"usedBytes"`
}

// kairosMounts are the partitions an operator cares about on a Kairos node.
// A mount that is not present on this node (e.g. no OEM partition) is skipped.
var kairosMounts = []struct{ label, mount string }{
	{"COS_PERSISTENT", "/usr/local"},
	{"COS_STATE", "/run/initramfs/cos-state"},
	{"COS_OEM", "/oem"},
}

// Seams over the host, swapped by specs through setMetricsSources.
var (
	procRoot = "/proc"
	sysRoot  = "/sys"
	statfsFn = statfsUsage
)

func statfsUsage(path string) (total, used uint64, err error) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return 0, 0, err
	}
	bsize := uint64(st.Bsize) //nolint:gosec // block size is never negative
	return st.Blocks * bsize, (st.Blocks - st.Bfree) * bsize, nil
}

// cpuSample is one reading of the aggregate cpu line of /proc/stat.
type cpuSample struct {
	total, idle uint64
}

// parseProcStat reads the aggregate "cpu" line. idle counts idle + iowait;
// total counts user..steal (guest time is already included in user and nice).
func parseProcStat(r io.Reader) (cpuSample, error) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 5 || fields[0] != "cpu" {
			continue
		}
		var vals []uint64
		for _, f := range fields[1:] {
			v, err := strconv.ParseUint(f, 10, 64)
			if err != nil {
				return cpuSample{}, fmt.Errorf("parsing /proc/stat: %w", err)
			}
			vals = append(vals, v)
		}
		var s cpuSample
		for i, v := range vals {
			if i >= 8 { // guest, guest_nice
				break
			}
			s.total += v
		}
		s.idle = vals[3]
		if len(vals) > 4 {
			s.idle += vals[4]
		}
		return s, nil
	}
	return cpuSample{}, errors.New("no aggregate cpu line in /proc/stat")
}

// cpuUsedPercent returns the busy share between two samples, or -1 when the
// counters did not advance (or were reset), so the caller reports nothing.
func cpuUsedPercent(prev, cur cpuSample) float64 {
	if cur.total <= prev.total || cur.idle < prev.idle {
		return -1
	}
	dt := float64(cur.total - prev.total)
	di := float64(cur.idle - prev.idle)
	if di > dt {
		return -1
	}
	return (dt - di) / dt * 100
}

// parseMeminfo reads MemTotal and MemAvailable (reported in kB).
func parseMeminfo(r io.Reader) (*MemoryMetrics, error) {
	var m MemoryMetrics
	var haveTotal bool
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		key, rest, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			continue
		}
		v, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			continue
		}
		switch key {
		case "MemTotal":
			m.TotalBytes, haveTotal = v*1024, true
		case "MemAvailable":
			m.AvailableBytes = v * 1024
		}
	}
	if !haveTotal {
		return nil, errors.New("no MemTotal in /proc/meminfo")
	}
	return &m, nil
}

// parseLoadavg reads the 1, 5 and 15 minute load averages.
func parseLoadavg(s string) ([]float64, error) {
	fields := strings.Fields(s)
	if len(fields) < 3 {
		return nil, errors.New("short /proc/loadavg")
	}
	out := make([]float64, 0, 3)
	for _, f := range fields[:3] {
		v, err := strconv.ParseFloat(f, 64)
		if err != nil {
			return nil, fmt.Errorf("parsing /proc/loadavg: %w", err)
		}
		out = append(out, v)
	}
	return out, nil
}

// parseUptime reads the first field of /proc/uptime as whole seconds.
func parseUptime(s string) (uint64, error) {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return 0, errors.New("empty /proc/uptime")
	}
	v, err := strconv.ParseFloat(fields[0], 64)
	if err != nil || v < 0 {
		return 0, fmt.Errorf("parsing /proc/uptime: %v", err)
	}
	return uint64(v), nil
}

// maxThermalC returns the hottest thermal zone in °C, or nil when the node
// exposes none (VMs usually do not).
func maxThermalC() *float64 {
	paths, _ := filepath.Glob(filepath.Join(sysRoot, "class/thermal/thermal_zone*/temp"))
	var best *float64
	for _, p := range paths {
		b, err := os.ReadFile(p) //nolint:gosec // fixed sysfs glob
		if err != nil {
			continue
		}
		milli, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
		if err != nil || milli <= 0 {
			continue
		}
		c := float64(milli) / 1000
		if best == nil || c > *best {
			best = &c
		}
	}
	return best
}

// cpuTracker keeps the previous /proc/stat sample, because CPU usage only
// exists as a delta between two readings. The first heartbeat of a process has
// no previous sample and reports no cpu block.
type cpuTracker struct {
	mu   sync.Mutex
	prev *cpuSample
}

// gather reads every source. Unlike gatherSystemInfo it is not cached: it runs
// on each heartbeat.
func (t *cpuTracker) gather() *NodeMetrics {
	m := &NodeMetrics{SampledAt: time.Now().UTC()}

	if f, err := os.Open(filepath.Join(procRoot, "stat")); err == nil {
		s, perr := parseProcStat(f)
		_ = f.Close()
		if perr == nil {
			t.mu.Lock()
			if t.prev != nil {
				if p := cpuUsedPercent(*t.prev, s); p >= 0 {
					m.CPU = &CPUMetrics{UsedPercent: p}
				}
			}
			t.prev = &s
			t.mu.Unlock()
		}
	}

	if f, err := os.Open(filepath.Join(procRoot, "meminfo")); err == nil {
		if mem, perr := parseMeminfo(f); perr == nil {
			m.Memory = mem
		}
		_ = f.Close()
	}

	if b, err := os.ReadFile(filepath.Join(procRoot, "loadavg")); err == nil {
		if l, perr := parseLoadavg(string(b)); perr == nil {
			m.Load = l
		}
	}

	if b, err := os.ReadFile(filepath.Join(procRoot, "uptime")); err == nil {
		if u, perr := parseUptime(string(b)); perr == nil {
			m.UptimeSeconds = u
		}
	}

	for _, km := range kairosMounts {
		total, used, err := statfsFn(km.mount)
		if err != nil || total == 0 {
			continue
		}
		m.Disks = append(m.Disks, DiskMetrics{Label: km.label, Mount: km.mount, TotalBytes: total, UsedBytes: used})
	}

	m.TemperatureC = maxThermalC()
	return m
}
