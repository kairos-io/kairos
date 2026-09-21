package validation_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// runInittabCommands runs a stage's shell commands against a fixture
// /etc/inittab the way yip's CommandStep does (sh -c, one command at a time),
// substituting inittabPath for the literal /etc/inittab the bundled configs
// hardcode.
func runInittabCommands(commands []string, inittabPath string) {
	for _, c := range commands {
		cmd := strings.ReplaceAll(c, "/etc/inittab", inittabPath)
		out, err := exec.Command("sh", "-c", cmd).CombinedOutput()
		Expect(err).NotTo(HaveOccurred(), "command %q: %s", c, out)
	}
}

// inittabLineFor returns the single line in content that starts with prefix,
// or "" if there is none.
func inittabLineFor(content, prefix string) string {
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, prefix) {
			return line
		}
	}
	return ""
}

// runstage.go always runs initramfs to completion, for every cloud-init file,
// before initramfs.after starts for any of them. 25_autologin.yaml writes the
// ttyS0 autologin line in the initramfs stage; 10_accounting.yaml
// unconditionally overwrites ttyS0 again in initramfs.after with a plain
// getty, with no guard telling it to stay off the line the live medium
// already claimed. 10_accounting.yaml never touches tty1 at all, so tty1
// keeps its autologin while ttyS0 does not.
//
// This spec pins today's asymmetric, wrong outcome: ttyS0 loses its live-mode
// autologin to the plain getty line, and root has no known password there
// (10_accounting.yaml also runs passwd -l root in the same stage), while
// tty1 keeps the autologin. It must start failing once the fix (a
// complementary live_mode guard on 10_accounting.yaml's serial-login step)
// lands, and get flipped to assert the fixed behaviour then.
var _ = Describe("OpenRC live-mode serial autologin vs 10_accounting.yaml (kairos-io/kairos#4741)", func() {
	It("loses the ttyS0 autologin to the plain getty line, but keeps it on tty1", func() {
		root := GinkgoT().TempDir()
		inittabPath := filepath.Join(root, "inittab")

		// A representative Alpine /etc/inittab before either stage touches it.
		Expect(os.WriteFile(inittabPath, []byte(strings.Join([]string{
			"::sysinit:/sbin/openrc sysinit",
			"::wait:/sbin/openrc boot",
			"::wait:/sbin/openrc default",
			"tty1::respawn:/sbin/getty 38400 tty1",
			"ttyS0::respawn:/sbin/getty -L ttyS0 115200 vt100",
			"::shutdown:/sbin/openrc shutdown",
			"",
		}, "\n")), 0644)).To(Succeed())

		autologinStages := readStages("25_autologin.yaml")
		autologin := stageRunningFor(autologinStages, "openrc", `sed -i -e 's/tty1.*//g' /etc/inittab`)

		accountingAfter := readStage("10_accounting.yaml", "initramfs.after")
		serialLogin := stageRunningFor(accountingAfter, "openrc", `sed -i -e 's/ttyS0.*//g' /etc/inittab`)

		// Stage order from agent/pkg/utils/runstage.go: initramfs completes for
		// every cloud-init file before initramfs.after starts for any of them.
		runInittabCommands(autologin.Commands, inittabPath)
		runInittabCommands(serialLogin.Commands, inittabPath)

		content, err := os.ReadFile(inittabPath)
		Expect(err).NotTo(HaveOccurred())

		ttyS0Line := inittabLineFor(string(content), "ttyS0")
		tty1Line := inittabLineFor(string(content), "tty1")

		// The bug: 10_accounting.yaml's unconditional serial-login step wins,
		// so ttyS0 gets a plain login prompt with no known root password.
		Expect(ttyS0Line).To(Equal("ttyS0::respawn:/sbin/getty -L ttyS0 115200 vt100"),
			"ttyS0 should still be overwritten by 10_accounting.yaml's unguarded step")
		Expect(ttyS0Line).NotTo(ContainSubstring("agetty --autologin root"),
			"ttyS0 lost its live-mode autologin to 10_accounting.yaml")

		// tty1 is untouched by 10_accounting.yaml, so it keeps the autologin
		// 25_autologin.yaml wrote. This is the asymmetry the issue describes.
		Expect(tty1Line).To(ContainSubstring("agetty --autologin root"),
			"tty1 should still have kept its live-mode autologin")
	})
})
