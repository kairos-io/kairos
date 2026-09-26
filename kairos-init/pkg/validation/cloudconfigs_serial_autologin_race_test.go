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

// runStage replays one stage the way yip does: evaluate the `if` expression
// first, and run the commands only when it succeeds.
func runStage(s guardStage, inittabPath, cmdline string, liveMode bool) {
	if s.If != "" && !evalGuard(s.If, cmdline, liveMode) {
		return
	}
	runInittabCommands(s.Commands, inittabPath)
}

// inittabLinesFor returns every line in content that starts with prefix.
func inittabLinesFor(content, prefix string) []string {
	var out []string
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, prefix) {
			out = append(out, line)
		}
	}
	return out
}

// inittabLineFor returns the single line in content that starts with prefix,
// and fails if there is not exactly one.
func inittabLineFor(content, prefix string) string {
	lines := inittabLinesFor(content, prefix)
	ExpectWithOffset(1, lines).To(HaveLen(1), "expected one %s line, got %v", prefix, lines)
	return lines[0]
}

// replayInittab writes a representative Alpine /etc/inittab, replays the two
// openrc stages that edit it in the order runstage.go runs them, and returns
// the result. runstage.go runs initramfs to completion, for every cloud-init
// file, before initramfs.after starts for any of them, so 25_autologin.yaml's
// initramfs stage always goes first and 10_accounting.yaml's initramfs.after
// stage always goes second, whatever the file names suggest.
func replayInittab(liveMode bool) string {
	inittabPath := filepath.Join(GinkgoT().TempDir(), "inittab")

	Expect(os.WriteFile(inittabPath, []byte(strings.Join([]string{
		"::sysinit:/sbin/openrc sysinit",
		"::wait:/sbin/openrc boot",
		"::wait:/sbin/openrc default",
		"tty1::respawn:/sbin/getty 38400 tty1",
		"ttyS0::respawn:/sbin/getty -L ttyS0 115200 vt100",
		"::shutdown:/sbin/openrc shutdown",
		"",
	}, "\n")), 0644)).To(Succeed())

	cmdline := "BOOT_IMAGE=/boot/kernel root=LABEL=COS_STATE"
	if liveMode {
		cmdline = "BOOT_IMAGE=/boot/kernel install-mode"
	}

	autologin := stageRunningFor(readStages("25_autologin.yaml"), "openrc",
		`sed -i -e 's/tty1.*//g' /etc/inittab`)
	serialLogin := stageRunningFor(readStage("10_accounting.yaml", "initramfs.after"), "openrc",
		`sed -i -e 's/ttyS0.*//g' /etc/inittab`)

	runStage(autologin, inittabPath, cmdline, liveMode)
	runStage(serialLogin, inittabPath, cmdline, liveMode)

	content, err := os.ReadFile(inittabPath)
	Expect(err).NotTo(HaveOccurred())
	return string(content)
}

// 25_autologin.yaml claims both tty1 and ttyS0 for a root autologin on an
// openrc live boot, because a machine whose only console is serial is
// reachable no other way and root's password is locked in the same boot.
// 10_accounting.yaml's serial login step rewrites ttyS0 as a plain getty and
// runs in a later stage, so the two only stay out of each other's way while
// their guards remain complements: live mode for one, everything else for the
// other.
var _ = Describe("OpenRC live-mode serial autologin vs 10_accounting.yaml (kairos-io/kairos#4741)", func() {
	It("autologins root on ttyS0 as well as tty1 on a live boot", func() {
		content := replayInittab(true)

		Expect(inittabLineFor(content, "ttyS0")).
			To(Equal("ttyS0::respawn:/sbin/agetty --autologin root -i --noclear ttyS0"))
		Expect(inittabLineFor(content, "tty1")).
			To(Equal("tty1::respawn:/sbin/agetty --autologin root -i --noclear tty1"))
	})

	It("leaves the plain serial getty alone off the live medium", func() {
		content := replayInittab(false)

		Expect(inittabLineFor(content, "ttyS0")).
			To(Equal("ttyS0::respawn:/sbin/getty -L ttyS0 115200 vt100"))
		Expect(inittabLineFor(content, "ttyS0")).
			NotTo(ContainSubstring("autologin"))
		Expect(inittabLineFor(content, "tty1")).
			To(Equal("tty1::respawn:/sbin/getty 38400 tty1"))
	})
})
