// Package diagnostics collects the state of a machine as labelled text
// groups: the kernel ring buffer, the block devices and mounts, the hardware
// inventory, and the running system.
//
// It lives in the SDK because two commands need the same picture of a broken
// machine. `kairos-agent logs` builds the debug bundle of an installed
// system, and the installer builds one for an install that failed before
// there is a system to log into. Collecting the sections in one place means a
// section added for one of them is in the other one's bundle too.
//
// Collection is best effort by design. A missing tool, a kernel without EFI,
// or a file that is not there on this flavour records what failed inside the
// group rather than failing the collection, because a bundle with one section
// missing is still the bundle somebody is waiting for.
package diagnostics

import (
	"fmt"
	"io/fs"
	"os"
	"strings"
)

// Runner runs an external command and returns its combined output. It is the
// subset of sdk/types/runner.Runner that collection needs, so a caller can
// pass the runner it already has, and a test can pass a fake.
type Runner interface {
	Run(name string, args ...string) ([]byte, error)
}

// FS is the subset of sdk/types/fs.KairosFS that collection needs: the files
// that are read verbatim, the /proc walk behind the process list, and the
// probe for EFI.
type FS interface {
	ReadFile(name string) ([]byte, error)
	Stat(name string) (fs.FileInfo, error)
	ReadDir(name string) ([]fs.DirEntry, error)
}

// OSFS reads the running machine. It is what a command collecting its own
// host passes to Collect.
type OSFS struct{}

// ReadFile reads name from the host filesystem.
func (OSFS) ReadFile(name string) ([]byte, error) { return os.ReadFile(name) }

// ReadDir lists name on the host filesystem.
func (OSFS) ReadDir(name string) ([]fs.DirEntry, error) { return os.ReadDir(name) }

// Stat stats name on the host filesystem.
func (OSFS) Stat(name string) (fs.FileInfo, error) { return os.Stat(name) }

// Group is one collected section file: a short name and the text to write
// under it. The caller decides where it goes and how it is named, because the
// agent puts these in a tarball and the installer writes them next to its own
// logs.
type Group struct {
	Name string
	Body string
}

// Group names, in the order Collect returns them.
const (
	GroupSystem   = "system"
	GroupKernel   = "kernel"
	GroupStorage  = "storage"
	GroupHardware = "hardware"
)

// Collect returns the diagnostic groups for the machine r and fsys describe.
func Collect(r Runner, fsys FS) []Group {
	return []Group{
		{Name: GroupSystem, Body: systemBody(r, fsys)},
		{Name: GroupKernel, Body: kernelBody(r)},
		{Name: GroupStorage, Body: storageBody(r, fsys)},
		{Name: GroupHardware, Body: hardwareBody(r, fsys)},
	}
}

// Section renders one labelled command block. A failing command is recorded
// inline, under the same header as a successful one, so a reader sees that it
// was tried and why it produced nothing.
func Section(r Runner, title, name string, args ...string) string {
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

// FileSection renders the contents of path as a labelled block, or the empty
// string when it cannot be read. A file that does not exist on this flavour
// is not a failure worth a line of its own.
func FileSection(fsys FS, path string) string {
	data, err := fsys.ReadFile(path)
	if err != nil {
		return ""
	}
	return "===== " + path + " =====\n" + string(data) + "\n"
}

// BootMode reports whether the machine booted through EFI or legacy BIOS.
func BootMode(fsys FS) string {
	if _, err := fsys.Stat("/sys/firmware/efi"); err == nil {
		return "EFI"
	}
	return "BIOS (legacy)"
}

// systemBody is who the machine is and what is running on it.
//
// The process list and the uptime are read from /proc rather than run as
// commands, because `ps` and `uptime` are either absent or a different
// program on the minimal flavours. See processSection for why that also
// settles the question of command line arguments in a bundle.
func systemBody(r Runner, fsys FS) string {
	var b strings.Builder
	b.WriteString(Section(r, "hostname", "hostname"))
	b.WriteString(Section(r, "uname", "uname", "-a"))
	b.WriteString(uptimeSection(fsys))
	b.WriteString(processSection(fsys))
	b.WriteString(Section(r, "ip addr", "ip", "a"))
	b.WriteString(FileSection(fsys, "/etc/resolv.conf"))
	return b.String()
}

func kernelBody(r Runner) string {
	var b strings.Builder
	b.WriteString(Section(r, "dmesg", "dmesg"))
	b.WriteString(Section(r, "kernel journal", "journalctl", "-k", "--no-pager"))
	b.WriteString(Section(r, "udevadm", "udevadm", "info", "--export-db"))
	return b.String()
}

func storageBody(r Runner, fsys FS) string {
	var b strings.Builder
	b.WriteString(Section(r, "lsblk", "lsblk", "-O"))
	b.WriteString(Section(r, "blkid", "blkid"))
	b.WriteString(Section(r, "parted", "parted", "-l"))
	b.WriteString(Section(r, "mount", "mount"))
	b.WriteString(Section(r, "df", "df", "-h"))
	b.WriteString(FileSection(fsys, "/proc/mounts"))
	return b.String()
}

func hardwareBody(r Runner, fsys FS) string {
	var b strings.Builder
	fmt.Fprintf(&b, "boot-mode: %s\n\n", BootMode(fsys))
	b.WriteString(Section(r, "efibootmgr", "efibootmgr", "-v"))
	b.WriteString(Section(r, "lspci", "lspci"))
	b.WriteString(Section(r, "lscpu", "lscpu"))
	b.WriteString(Section(r, "dmidecode", "dmidecode", "-t", "system", "-t", "bios"))
	b.WriteString(FileSection(fsys, "/proc/meminfo"))
	return b.String()
}
