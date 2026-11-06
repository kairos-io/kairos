package debugbundle_test

import (
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/kairos/v4/installer/internal/debugbundle"
	"github.com/kairos-io/kairos/v4/sdk/diagnostics"
)

// fakeRunner returns canned output and errors keyed by command name.
type fakeRunner struct {
	calls   []string
	outputs map[string][]byte
	errs    map[string]error
}

func (f *fakeRunner) Run(name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, strings.TrimSpace(name+" "+strings.Join(args, " ")))
	return f.outputs[name], f.errs[name]
}

var _ = Describe("CollectExtras", func() {
	It("writes one file per diagnostic group plus the installer context, and records command failures inline", func() {
		dir := GinkgoT().TempDir()
		r := &fakeRunner{
			outputs: map[string][]byte{"lsblk": []byte("NAME sda\n")},
			errs:    map[string]error{"dmesg": os.ErrPermission},
		}
		c := debugbundle.Context{
			AgentBin:            "/usr/bin/kairos-agent",
			AgentArgs:           []string{"manual-install", "/tmp/cc.yaml"},
			Disk:                "/dev/sda",
			Source:              "oci:foo",
			Version:             "v1.2.3",
			CloudConfigRedacted: "#cloud-config\npassword: ***REDACTED***\n",
		}

		paths, err := debugbundle.CollectExtras(r, c, dir)
		Expect(err).ToNot(HaveOccurred())
		// Derived, not restated: a group added to sdk/diagnostics lands here
		// without this spec having to be told about it.
		Expect(paths).To(HaveLen(len(diagnostics.Collect(r, diagnostics.OSFS{})) + 1))

		kernel, _ := os.ReadFile(filepath.Join(dir, "installer-kernel.log"))
		Expect(string(kernel)).To(ContainSubstring("(FAILED:")) // dmesg failed but did not abort

		storage, _ := os.ReadFile(filepath.Join(dir, "installer-storage.log"))
		Expect(string(storage)).To(ContainSubstring("NAME sda"))

		ctx, _ := os.ReadFile(filepath.Join(dir, "installer-context.log"))
		Expect(string(ctx)).To(ContainSubstring("v1.2.3"))
		Expect(string(ctx)).To(ContainSubstring("***REDACTED***"))
		Expect(string(ctx)).To(ContainSubstring("/dev/sda"))
	})

	It("collects the machine state through sdk/diagnostics, so the agent bundle has it too", func() {
		dir := GinkgoT().TempDir()
		r := &fakeRunner{
			outputs: map[string][]byte{"hostname": []byte("node-1\n")},
			errs:    map[string]error{},
		}

		_, err := debugbundle.CollectExtras(r, debugbundle.Context{}, dir)
		Expect(err).ToNot(HaveOccurred())

		system, err := os.ReadFile(filepath.Join(dir, "installer-system.log"))
		Expect(err).ToNot(HaveOccurred())
		Expect(string(system)).To(ContainSubstring("node-1"))
		Expect(string(system)).To(ContainSubstring("===== uname (uname -a) ====="))
	})
})
