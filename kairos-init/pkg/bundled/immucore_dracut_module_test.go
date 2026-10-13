package bundled_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/kairos-io/kairos/v4/kairos-init/pkg/bundled"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// The module setup script is shell that dracut sources, so the only honest way
// to ask what it installs is to run it with the dracut helpers replaced by
// stubs that record their arguments. Reading the constant for a substring
// would pass on a line sitting in a comment or in a branch that never runs.
var _ = Describe("ImmucoreModuleSetupDracut", func() {
	var calls string

	BeforeEach(func() {
		dir := GinkgoT().TempDir()
		module := filepath.Join(dir, "module-setup.sh")
		Expect(os.WriteFile(module, []byte(bundled.ImmucoreModuleSetupDracut), 0755)).To(Succeed())

		log := filepath.Join(dir, "calls")
		harness := `
record() { echo "$@" >> ` + log + `; }
inst_multiple() { record inst_multiple "$@"; }
inst_simple() { record inst_simple "$@"; }
inst_script() { record inst_script "$@"; }
inst_libdir_file() { record inst_libdir_file "$@"; }
instmods() { record instmods "$@"; }
ln_r() { record ln_r "$@"; }
derror() { record derror "$@"; }
dracut_need_initqueue() { record dracut_need_initqueue; }
moddir=` + dir + `
initdir=` + dir + `/initdir
systemdutildir=/usr/lib/systemd
systemdsystemunitdir=/usr/lib/systemd/system
. ` + module + `
# The module defines inst_check_multiple itself, and that definition probes
# this host for every binary. Replace it after sourcing: what it installs is
# the question here, not what this box happens to have.
inst_check_multiple() { record inst_check_multiple "$@"; }
install
`
		out, err := exec.Command("/bin/bash", "-c", harness).CombinedOutput()
		Expect(err).ToNot(HaveOccurred(), string(out))

		recorded, err := os.ReadFile(log)
		Expect(err).ToNot(HaveOccurred())
		calls = string(recorded)
	})

	// immucore validates a system extension image against the image policy the
	// systemd-sysext drop-in enforces before linking it into /run/extensions,
	// and systemd-dissect is the only tool that evaluates an image policy. The
	// 11systemd-sysext dracut module installs systemd-sysext and
	// systemd-confext and stops there, so nothing else puts systemd-dissect in
	// the initramfs. Without it the check has no tool to run and every image
	// is enabled unvalidated, which is what one image that fails the policy
	// needs to stop the whole refresh.
	It("installs systemd-dissect so the extension policy can be evaluated", func() {
		Expect(installed(calls)).To(ContainElement("systemd-dissect"))
	})

	// Flavors on systemd older than 254 have no systemd-dissect at all, and a
	// mandatory install there makes the initramfs unbuildable. immucore falls
	// back to not validating on those, so the binary has to be optional.
	It("asks for systemd-dissect optionally", func() {
		Expect(calls).To(ContainSubstring("inst_multiple -o systemd-dissect"))
		Expect(mandatory(calls)).ToNot(ContainElement("systemd-dissect"))
	})

	It("still installs immucore itself and the yip stage utilities", func() {
		Expect(installed(calls)).To(ContainElements("immucore", "blkid", "cryptsetup", "mount"))
	})
})

// installed returns every binary the script asked dracut for, whether the ask
// was mandatory or optional.
func installed(calls string) []string {
	var names []string
	for _, line := range strings.Split(calls, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if fields[0] != "inst_multiple" && fields[0] != "inst_check_multiple" {
			continue
		}
		for _, field := range fields[1:] {
			if strings.HasPrefix(field, "-") || strings.Contains(field, "/") {
				continue
			}
			names = append(names, field)
		}
	}
	return names
}

// mandatory returns the binaries whose absence fails the image build, which is
// what inst_check_multiple in this module means.
func mandatory(calls string) []string {
	var names []string
	for _, line := range strings.Split(calls, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] != "inst_check_multiple" {
			continue
		}
		names = append(names, fields[1:]...)
	}
	return names
}
