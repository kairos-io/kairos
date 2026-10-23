package diagnostics

import (
	"bytes"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// procHZ is the unit of the time fields in /proc. It is USER_HZ, which the
// kernel fixes at 100 for userspace on every architecture Kairos builds for,
// independently of the CONFIG_HZ the kernel itself ticks at.
const procHZ = 100

// Offsets into the /proc/<pid>/stat fields that follow the executable name.
// proc(5) numbers the state field 3, the parent pid 4 and the start time 22,
// and the name is field 2, so field N sits at offset N-3.
const (
	offState     = 0
	offPPID      = 1
	offStartTime = 19
)

type process struct {
	pid     int
	ppid    int
	state   string
	elapsed time.Duration
	comm    string
}

// processSection renders the running processes, read from /proc with no
// external tool.
//
// `ps` is not the same program on every flavour. A BusyBox image rejects the
// POSIX `-eo` column selection, so on exactly the minimal images where a debug
// bundle matters most, the section used to hold an error message instead of a
// process list.
//
// Reading /proc also makes the promise this section is built around structural
// rather than a matter of which columns were asked for. The command line of a
// process lives in /proc/<pid>/cmdline, which is never opened here, so the
// cluster join token a Kubernetes agent is started with cannot reach a bundle
// that is made to be sent to somebody else.
func processSection(fsys FS) string {
	var b strings.Builder
	b.WriteString("===== processes (/proc) =====\n")
	procs, err := readProcesses(fsys)
	if err != nil {
		fmt.Fprintf(&b, "(FAILED: %v)\n\n", err)
		return b.String()
	}
	fmt.Fprintf(&b, "%7s %7s %-5s %12s %s\n", "PID", "PPID", "STAT", "ELAPSED", "COMMAND")
	for _, p := range procs {
		fmt.Fprintf(&b, "%7d %7d %-5s %12s %s\n", p.pid, p.ppid, p.state, elapsedString(p.elapsed), p.comm)
	}
	b.WriteByte('\n')
	return b.String()
}

// uptimeSection reports how long the machine has been up. It reads
// /proc/uptime for the same reason processSection reads /proc: the `uptime`
// binary is not installed on the minimal flavours.
func uptimeSection(fsys FS) string {
	var b strings.Builder
	b.WriteString("===== uptime (/proc/uptime) =====\n")
	up, err := readUptime(fsys)
	if err != nil {
		fmt.Fprintf(&b, "(FAILED: %v)\n", err)
	} else {
		fmt.Fprintf(&b, "up %s\n", elapsedString(up))
	}
	b.WriteByte('\n')
	return b.String()
}

// readProcesses walks /proc and returns one entry per process, ordered by pid.
func readProcesses(fsys FS) ([]process, error) {
	uptime, err := readUptime(fsys)
	if err != nil {
		return nil, err
	}
	entries, err := fsys.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	var procs []process
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			// /proc holds plenty that is not a process.
			continue
		}
		p, err := readProcess(fsys, pid, uptime)
		if err != nil {
			// A process that exited while the directory was being walked is
			// not a collection failure.
			continue
		}
		procs = append(procs, p)
	}
	sort.Slice(procs, func(i, j int) bool { return procs[i].pid < procs[j].pid })
	return procs, nil
}

func readProcess(fsys FS, pid int, uptime time.Duration) (process, error) {
	raw, err := fsys.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return process{}, err
	}
	// The executable name is field 2, in parentheses, and may itself contain
	// spaces and parentheses. The fields after it therefore start past the
	// LAST ')' in the line, not at a fixed offset.
	end := bytes.LastIndexByte(raw, ')')
	if end < 0 {
		return process{}, fmt.Errorf("/proc/%d/stat: no comm field", pid)
	}
	// fields[0] is stat field 3, so stat field N is fields[N-statState].
	fields := strings.Fields(string(raw[end+1:]))
	if len(fields) <= offStartTime {
		return process{}, fmt.Errorf("/proc/%d/stat: %d fields after comm", pid, len(fields))
	}
	ppid, err := strconv.Atoi(fields[offPPID])
	if err != nil {
		return process{}, fmt.Errorf("/proc/%d/stat: ppid: %w", pid, err)
	}
	ticks, err := strconv.ParseInt(fields[offStartTime], 10, 64)
	if err != nil {
		return process{}, fmt.Errorf("/proc/%d/stat: starttime: %w", pid, err)
	}
	elapsed := uptime - time.Duration(ticks)*time.Second/procHZ
	if elapsed < 0 {
		elapsed = 0
	}
	return process{
		pid:     pid,
		ppid:    ppid,
		state:   fields[offState],
		elapsed: elapsed,
		comm:    comm(fsys, pid, raw, end),
	}, nil
}

// comm is the executable name. /proc/<pid>/comm holds it on its own; the copy
// in /proc/<pid>/stat, between the first '(' and the last ')', is the fallback
// for a kernel too old to have it.
func comm(fsys FS, pid int, stat []byte, end int) string {
	if c, err := fsys.ReadFile(fmt.Sprintf("/proc/%d/comm", pid)); err == nil {
		if name := strings.TrimSpace(string(c)); name != "" {
			return name
		}
	}
	if start := bytes.IndexByte(stat, '('); start >= 0 && start < end {
		return string(stat[start+1 : end])
	}
	return ""
}

func readUptime(fsys FS) (time.Duration, error) {
	raw, err := fsys.ReadFile("/proc/uptime")
	if err != nil {
		return 0, err
	}
	fields := strings.Fields(string(raw))
	if len(fields) == 0 {
		return 0, errors.New("/proc/uptime: empty")
	}
	secs, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, fmt.Errorf("/proc/uptime: %w", err)
	}
	return time.Duration(secs * float64(time.Second)), nil
}

// elapsedString renders a duration the way ps renders its ELAPSED column:
// [[DD-]HH:]MM:SS.
func elapsedString(d time.Duration) string {
	total := int64(d / time.Second)
	secs, mins, hours, days := total%60, total/60%60, total/3600%24, total/86400
	switch {
	case days > 0:
		return fmt.Sprintf("%d-%02d:%02d:%02d", days, hours, mins, secs)
	case hours > 0:
		return fmt.Sprintf("%d:%02d:%02d", hours, mins, secs)
	default:
		return fmt.Sprintf("%02d:%02d", mins, secs)
	}
}
