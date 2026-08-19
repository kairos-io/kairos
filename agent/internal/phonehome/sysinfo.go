package phonehome

import (
	"bufio"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
)

var (
	cachedInfo     map[string]string
	cachedInfoOnce sync.Once
)

// gatherSystemInfo collects OS, kernel, CPU, and memory info.
// Results are cached since system info doesn't change between heartbeats.
func gatherSystemInfo() map[string]string {
	cachedInfoOnce.Do(func() {
		info := make(map[string]string)

		mergeEnvFile(info, "/etc/os-release")
		mergeEnvFile(info, "/etc/kairos-release")

		if out, err := exec.Command("uname", "-r").Output(); err == nil {
			info["KERNEL"] = strings.TrimSpace(string(out))
		}
		if out, err := exec.Command("uname", "-m").Output(); err == nil {
			info["ARCH"] = strings.TrimSpace(string(out))
		}

		if f, err := os.Open("/proc/meminfo"); err == nil {
			scanner := bufio.NewScanner(f)
			for scanner.Scan() {
				if strings.HasPrefix(scanner.Text(), "MemTotal:") {
					info["MEM_TOTAL"] = strings.TrimSpace(
						strings.TrimPrefix(scanner.Text(), "MemTotal:"))
					break
				}
			}
			_ = f.Close()
		}

		if f, err := os.Open("/proc/cpuinfo"); err == nil {
			count := 0
			scanner := bufio.NewScanner(f)
			for scanner.Scan() {
				if strings.HasPrefix(scanner.Text(), "processor") {
					count++
				}
			}
			_ = f.Close()
			if count > 0 {
				info["CPU_COUNT"] = strconv.Itoa(count)
			}
		}

		cachedInfo = info
	})
	return cachedInfo
}

// mergeEnvFile parses KEY=VALUE lines (optional quotes) and merges into dst.
// path is always a compile-time constant in this package (see gatherSystemInfo).
func mergeEnvFile(dst map[string]string, path string) {
	f, err := os.Open(path) //nosec G304 -- callers pass only fixed /etc/*-release paths
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		v = strings.Trim(v, `"'`)
		dst[k] = v
	}
}
