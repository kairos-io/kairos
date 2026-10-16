package diagnostics_test

import (
	"errors"
	"io/fs"
	"os"
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
}

func (f fakeFS) ReadFile(name string) ([]byte, error) {
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
		r.outputs["ps"] = []byte("the-process-list\n")

		system := bodyOf(diagnostics.Collect(r, fakeFS{}), diagnostics.GroupSystem)
		Expect(system).To(ContainSubstring("===== hostname (hostname ) =====\nthe-hostname"))
		Expect(system).To(ContainSubstring("===== uname (uname -a) =====\nthe-uname"))
		Expect(system).To(ContainSubstring("the-process-list"))
	})

	It("asks ps for the columns only, never for the command line", func() {
		diagnostics.Collect(r, fakeFS{})

		var psCalls []string
		for _, c := range r.calls {
			if strings.HasPrefix(c, "ps") {
				psCalls = append(psCalls, c)
			}
		}
		// A node runs its kubernetes agent with the cluster join token as an
		// argument, and a debug bundle is made to be sent to somebody else.
		Expect(psCalls).To(HaveLen(1))
		Expect(psCalls[0]).To(Equal("ps -eo pid,ppid,stat,etime,comm"))
		Expect(psCalls[0]).ToNot(ContainSubstring("args"))
		Expect(psCalls[0]).ToNot(ContainSubstring("ps aux"))
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
