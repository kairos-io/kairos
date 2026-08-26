package debugbundle

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Runner runs an external command and returns its combined output. It is an
// interface so tests can inject a fake.
type Runner interface {
	Run(name string, args ...string) ([]byte, error)
}

// ExecRunner runs real commands via os/exec.
type ExecRunner struct{}

// Run executes name with args and returns combined stdout+stderr.
func (ExecRunner) Run(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).CombinedOutput()
}

// Context carries install metadata included verbatim in installer-context.log.
type Context struct {
	AgentBin            string
	AgentArgs           []string
	Disk                string
	Source              string
	Version             string
	CloudConfigRedacted string
}

// section renders one labeled command block. A failing command is recorded
// inline and never aborts collection.
func section(r Runner, title, name string, args ...string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "===== %s (%s %s) =====\n", title, name, strings.Join(args, " "))
	out, err := r.Run(name, args...)
	if err != nil {
		fmt.Fprintf(&b, "(FAILED: %v)\n", err)
	}
	b.Write(out)
	if len(out) == 0 || out[len(out)-1] != '\n' {
		b.WriteByte('\n')
	}
	b.WriteByte('\n')
	return b.String()
}

// bootMode reports EFI or BIOS based on /sys/firmware/efi presence.
func bootMode() string {
	if _, err := os.Stat("/sys/firmware/efi"); err == nil {
		return "EFI"
	}
	return "BIOS (legacy)"
}

type group struct {
	filename string
	body     string
}

func kernelGroup(r Runner) group {
	var b strings.Builder
	b.WriteString(section(r, "dmesg", "dmesg"))
	b.WriteString(section(r, "kernel journal", "journalctl", "-k", "--no-pager"))
	b.WriteString(section(r, "udevadm", "udevadm", "info", "--export-db"))
	return group{filename: "installer-kernel.log", body: b.String()}
}

func storageGroup(r Runner) group {
	var b strings.Builder
	b.WriteString(section(r, "lsblk", "lsblk", "-O"))
	b.WriteString(section(r, "blkid", "blkid"))
	b.WriteString(section(r, "parted", "parted", "-l"))
	b.WriteString(section(r, "mount", "mount"))
	b.WriteString(section(r, "df", "df", "-h"))
	if data, err := os.ReadFile("/proc/mounts"); err == nil {
		b.WriteString("===== /proc/mounts =====\n" + string(data) + "\n")
	}
	return group{filename: "installer-storage.log", body: b.String()}
}

func hardwareGroup(r Runner) group {
	var b strings.Builder
	fmt.Fprintf(&b, "boot-mode: %s\n\n", bootMode())
	b.WriteString(section(r, "efibootmgr", "efibootmgr", "-v"))
	b.WriteString(section(r, "lspci", "lspci"))
	b.WriteString(section(r, "lscpu", "lscpu"))
	b.WriteString(section(r, "dmidecode", "dmidecode", "-t", "system", "-t", "bios"))
	if data, err := os.ReadFile("/proc/meminfo"); err == nil {
		b.WriteString("===== /proc/meminfo =====\n" + string(data) + "\n")
	}
	return group{filename: "installer-hardware.log", body: b.String()}
}

func contextGroup(r Runner, c Context) group {
	var b strings.Builder
	fmt.Fprintf(&b, "installer-version: %s\n", c.Version)
	fmt.Fprintf(&b, "boot-mode: %s\n", bootMode())
	fmt.Fprintf(&b, "selected-disk: %s\n", c.Disk)
	fmt.Fprintf(&b, "install-source: %s\n", c.Source)
	fmt.Fprintf(&b, "agent-command: %s %s\n\n", c.AgentBin, strings.Join(c.AgentArgs, " "))

	b.WriteString("===== KAIROS_* environment =====\n")
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "KAIROS_") {
			b.WriteString(e + "\n")
		}
	}
	b.WriteString("\n===== rendered cloud-config (password redacted) =====\n")
	b.WriteString(c.CloudConfigRedacted + "\n\n")

	b.WriteString(section(r, "ip addr", "ip", "a"))
	if data, err := os.ReadFile("/etc/resolv.conf"); err == nil {
		b.WriteString("===== /etc/resolv.conf =====\n" + string(data) + "\n")
	}
	return group{filename: "installer-context.log", body: b.String()}
}

// CollectExtras writes the four grouped .log files into dir and returns the
// written file paths. Best-effort: a write error for one file does not stop the
// others; the first error (if any) is returned after attempting all of them.
func CollectExtras(r Runner, c Context, dir string) ([]string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	groups := []group{kernelGroup(r), storageGroup(r), hardwareGroup(r), contextGroup(r, c)}
	var written []string
	var firstErr error
	for _, g := range groups {
		p := filepath.Join(dir, g.filename)
		if err := os.WriteFile(p, []byte(g.body), 0o644); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		written = append(written, p)
	}
	return written, firstErr
}
