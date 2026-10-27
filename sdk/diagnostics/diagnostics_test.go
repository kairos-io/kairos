package diagnostics_test

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strconv"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/kairos/v4/sdk/diagnostics"
)

// fakeRunner answers by command name and records what it was asked to run.
type fakeRunner struct {
	calls   []string
	outputs map[string][]byte
	errs    map[string]error
}

func (f *fakeRunner) Run(name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, strings.TrimSpace(name+" "+strings.Join(args, " ")))
	return f.outputs[name], f.errs[name]
}

// fakeFS serves a fixed set of files and nothing else.
type fakeFS struct {
	files map[string]string
	reads *[]string
}

func (f fakeFS) ReadFile(name string) ([]byte, error) {
	if f.reads != nil {
		*f.reads = append(*f.reads, name)
	}
	data, ok := f.files[name]
	if !ok {
		return nil, os.ErrNotExist
	}
	return []byte(data), nil
}

func (f fakeFS) Stat(name string) (fs.FileInfo, error) {
	if _, ok := f.files[name]; !ok {
		return nil, os.ErrNotExist
	}
	// Only the error matters to BootMode, which is the one caller.
	return nil, nil
}

// ReadDir lists the immediate children of dirname that this filesystem has a
// file for. It is enough for the /proc walk, which only reads the names.
func (f fakeFS) ReadDir(dirname string) ([]fs.DirEntry, error) {
	prefix := strings.TrimSuffix(dirname, "/") + "/"
	seen := map[string]bool{}
	var entries []fs.DirEntry
	for name := range f.files {
		rest, ok := strings.CutPrefix(name, prefix)
		if !ok {
			continue
		}
		child, _, _ := strings.Cut(rest, "/")
		if child == "" || seen[child] {
			continue
		}
		seen[child] = true
		entries = append(entries, dirEntry(child))
	}
	if len(entries) == 0 {
		return nil, os.ErrNotExist
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	return entries, nil
}

// dirEntry is a name and nothing else, which is all the /proc walk looks at.
type dirEntry string

func (d dirEntry) Name() string               { return string(d) }
func (d dirEntry) IsDir() bool                { return true }
func (d dirEntry) Type() fs.FileMode          { return fs.ModeDir }
func (d dirEntry) Info() (fs.FileInfo, error) { return nil, os.ErrNotExist }

// procFS builds a filesystem with a /proc the process walk can read. Each
// entry of procs is a pid mapped to the (comm, state, ppid, start-ticks) its
// /proc/<pid>/stat should report.
func procFS(uptime string, procs map[int][4]string) fakeFS {
	files := map[string]string{"/proc/uptime": uptime + " 0.00\n"}
	for pid, p := range procs {
		comm, state, ppid, start := p[0], p[1], p[2], p[3]
		// proc(5): pid, (comm), state, ppid, then 17 fields before starttime.
		fields := []string{strconv.Itoa(pid), "(" + comm + ")", state, ppid}
		for i := 5; i <= 21; i++ {
			fields = append(fields, "0")
		}
		fields = append(fields, start)
		files[fmt.Sprintf("/proc/%d/stat", pid)] = strings.Join(fields, " ") + "\n"
		files[fmt.Sprintf("/proc/%d/comm", pid)] = comm + "\n"
		files[fmt.Sprintf("/proc/%d/cmdline", pid)] = comm + "\x00--token\x00SECRET-JOIN-TOKEN\x00"
	}
	return fakeFS{files: files}
}

// bodyOf returns the body of the named group, failing the spec if Collect did
// not produce it.
func bodyOf(groups []diagnostics.Group, name string) string {
	for _, g := range groups {
		if g.Name == name {
			return g.Body
		}
	}
	Fail("no group named " + name)
	return ""
}

var _ = Describe("Collect", func() {
	var r *fakeRunner

	BeforeEach(func() {
		r = &fakeRunner{outputs: map[string][]byte{}, errs: map[string]error{}}
	})

	It("returns the four groups in a stable order", func() {
		groups := diagnostics.Collect(r, fakeFS{})
		names := []string{}
		for _, g := range groups {
			names = append(names, g.Name)
		}
		Expect(names).To(Equal([]string{
			diagnostics.GroupSystem,
			diagnostics.GroupKernel,
			diagnostics.GroupStorage,
			diagnostics.GroupHardware,
		}))
	})

	It("collects the sections this issue asked for that nothing collected before", func() {
		// Each command answers with a string only it produces, so dropping one
		// section cannot be masked by another section's output.
		r.outputs["hostname"] = []byte("the-hostname\n")
		r.outputs["uname"] = []byte("the-uname\n")

		fsys := procFS("90061.00", map[int][4]string{1: {"the-init-process", "S", "0", "0"}})
		system := bodyOf(diagnostics.Collect(r, fsys), diagnostics.GroupSystem)
		Expect(system).To(ContainSubstring("===== hostname (hostname ) =====\nthe-hostname"))
		Expect(system).To(ContainSubstring("===== uname (uname -a) =====\nthe-uname"))
		Expect(system).To(ContainSubstring("the-init-process"))
	})

	It("reads the process list from /proc, so a BusyBox image gets one too", func() {
		// BusyBox ps rejects the POSIX -eo column selection, which is what
		// this section used to ask for, so on the minimal flavours the list
		// was an error message. Running no command at all cannot regress that
		// way, and the spec asserts both halves: the table, and that nothing
		// was executed to produce it.
		fsys := procFS("90061.50", map[int][4]string{
			1:    {"systemd", "S", "0", "0"},
			1000: {"k3s-agent", "R", "342", "9000150"},
			342:  {"udevd", "S", "1", "9000150"},
		})

		system := bodyOf(diagnostics.Collect(r, fsys), diagnostics.GroupSystem)

		Expect(system).To(ContainSubstring("===== processes (/proc) ====="))
		Expect(system).To(ContainSubstring("PID    PPID STAT       ELAPSED COMMAND"))
		// 90061s of uptime, started at tick 0: 1 day, 1 hour, 1 minute, 1s.
		Expect(system).To(MatchRegexp(`\n +1 +0 S +1-01:01:01 systemd\n`))
		// Started at 90001.5s of a 90061.5s uptime, so one minute ago, and listed
		// after pid 1 whatever order /proc hands the directory back in.
		Expect(system).To(MatchRegexp(`\n +1000 +342 R +01:00 k3s-agent\n`))
		// Ordered by pid as a number: "1000" sorts before "342" as a name.
		Expect(strings.Index(system, "udevd")).To(BeNumerically("<", strings.Index(system, "k3s-agent")))

		Expect(r.calls).ToNot(ContainElement(ContainSubstring("ps")))
	})

	It("never opens the command line of a process", func() {
		// A node runs its kubernetes agent with the cluster join token as an
		// argument, and a debug bundle is made to be sent to somebody else.
		// The guarantee is that /proc/<pid>/cmdline is not read, which holds
		// whatever ps on this image would have printed.
		var reads []string
		fsys := procFS("120.00", map[int][4]string{4242: {"k3s-agent", "S", "1", "0"}})
		fsys.reads = &reads

		groups := diagnostics.Collect(r, fsys)

		Expect(reads).To(ContainElement("/proc/4242/stat"))
		Expect(reads).ToNot(ContainElement(ContainSubstring("cmdline")))
		for _, g := range groups {
			Expect(g.Body).ToNot(ContainSubstring("SECRET-JOIN-TOKEN"))
		}
	})

	It("reports the uptime from /proc, not from the uptime binary", func() {
		fsys := procFS("3725.00", nil)

		system := bodyOf(diagnostics.Collect(r, fsys), diagnostics.GroupSystem)

		Expect(system).To(ContainSubstring("===== uptime (/proc/uptime) =====\nup 1:02:05\n"))
		Expect(r.calls).ToNot(ContainElement("uptime"))
	})

	It("records a /proc it cannot read instead of dropping the section", func() {
		system := bodyOf(diagnostics.Collect(r, fakeFS{}), diagnostics.GroupSystem)

		Expect(system).To(ContainSubstring("===== processes (/proc) =====\n(FAILED: "))
		Expect(system).To(ContainSubstring("===== uptime (/proc/uptime) =====\n(FAILED: "))
		// The sections after the failing ones still ran.
		Expect(system).To(ContainSubstring("===== ip addr"))
	})

	It("skips a process that exits while /proc is being walked", func() {
		fsys := procFS("120.00", map[int][4]string{
			1:   {"systemd", "S", "0", "0"},
			999: {"gone", "S", "1", "0"},
		})
		delete(fsys.files, "/proc/999/stat")
		// /proc holds plenty that is not a process directory.
		fsys.files["/proc/cmdline"] = "root=LABEL=COS_STATE\n"

		system := bodyOf(diagnostics.Collect(r, fsys), diagnostics.GroupSystem)

		Expect(system).To(ContainSubstring("systemd"))
		Expect(system).ToNot(ContainSubstring("gone"))
		Expect(system).ToNot(ContainSubstring("FAILED"))
	})

	It("takes the name from the last parenthesis, so a spaced comm stays whole", func() {
		// proc(5) field 2 is parenthesised and may itself contain spaces and
		// parentheses, so the fields after it are not at a fixed offset.
		fsys := procFS("120.00", map[int][4]string{7: {"a (odd) name", "S", "1", "0"}})
		delete(fsys.files, "/proc/7/comm") // force the /proc/<pid>/stat fallback

		system := bodyOf(diagnostics.Collect(r, fsys), diagnostics.GroupSystem)

		Expect(system).To(MatchRegexp(`\n +7 +1 S +02:00 a \(odd\) name\n`))
	})

	It("records a command that failed instead of dropping the section", func() {
		r.errs["dmesg"] = errors.New("operation not permitted")

		kernel := bodyOf(diagnostics.Collect(r, fakeFS{}), diagnostics.GroupKernel)
		Expect(kernel).To(ContainSubstring("===== dmesg (dmesg ) ====="))
		Expect(kernel).To(ContainSubstring("(FAILED: operation not permitted)"))
		// The sections after the failing one still ran.
		Expect(kernel).To(ContainSubstring("===== udevadm"))
	})

	It("reads the files it is given and leaves out the ones it is not", func() {
		fsys := fakeFS{files: map[string]string{
			"/proc/mounts":      "rootfs / rootfs rw 0 0\n",
			"/etc/resolv.conf":  "nameserver 1.1.1.1\n",
			"/sys/firmware/efi": "",
		}}
		groups := diagnostics.Collect(r, fsys)

		Expect(bodyOf(groups, diagnostics.GroupStorage)).To(ContainSubstring("rootfs / rootfs rw"))
		Expect(bodyOf(groups, diagnostics.GroupSystem)).To(ContainSubstring("nameserver 1.1.1.1"))
		// /proc/meminfo is not in this filesystem, so it contributes nothing.
		Expect(bodyOf(groups, diagnostics.GroupHardware)).ToNot(ContainSubstring("/proc/meminfo"))
	})

	It("reports the boot mode from the EFI firmware directory", func() {
		efi := fakeFS{files: map[string]string{"/sys/firmware/efi": ""}}
		Expect(diagnostics.BootMode(efi)).To(Equal("EFI"))
		Expect(diagnostics.BootMode(fakeFS{})).To(Equal("BIOS (legacy)"))

		Expect(bodyOf(diagnostics.Collect(r, efi), diagnostics.GroupHardware)).
			To(HavePrefix("boot-mode: EFI\n"))
	})

	It("ends every command section with a blank line so sections stay apart", func() {
		r.outputs["hostname"] = []byte("node-1") // no trailing newline

		out := diagnostics.Section(r, "hostname", "hostname")
		Expect(out).To(Equal("===== hostname (hostname ) =====\nnode-1\n\n"))
	})
})
