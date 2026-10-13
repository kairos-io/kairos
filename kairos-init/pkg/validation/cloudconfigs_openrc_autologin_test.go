package validation_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// alpineStockInittab is the console block Alpine ships, which is what
// 25_autologin.yaml rewrites on a live boot. Six virtual consoles and one
// serial line, none of them logged in.
const alpineStockInittab = `::sysinit:/sbin/openrc sysinit
::sysinit:/sbin/openrc boot
::wait:/sbin/openrc default

tty1::respawn:/sbin/getty 38400 tty1
tty2::respawn:/sbin/getty 38400 tty2
tty3::respawn:/sbin/getty 38400 tty3
tty4::respawn:/sbin/getty 38400 tty4
tty5::respawn:/sbin/getty 38400 tty5
tty6::respawn:/sbin/getty 38400 tty6
ttyS0::respawn:/sbin/getty -L ttyS0 115200 vt100

::shutdown:/sbin/openrc shutdown
`

// runOpenRCAutologin runs the openrc stage of 25_autologin.yaml against a copy
// of the stock inittab and returns the file it produced.
//
// The commands are run the way yip runs them, one `sh -c` each, with the
// inittab path pointed at the copy. Running them beats reading them: the whole
// stage is a sed and a few appends, so a spec that parsed the yaml would prove
// nothing about what the sed matches.
func runOpenRCAutologin() string {
	var commands []string
	for _, s := range readStages("25_autologin.yaml") {
		if s.OnlyServiceManager == "openrc" {
			commands = s.Commands
			break
		}
	}
	Expect(commands).NotTo(BeEmpty(), "25_autologin.yaml has no openrc stage with commands")

	inittab := filepath.Join(GinkgoT().TempDir(), "inittab")
	Expect(os.WriteFile(inittab, []byte(alpineStockInittab), 0644)).To(Succeed())

	for _, c := range commands {
		c = strings.ReplaceAll(c, "/etc/inittab", inittab)
		out, err := exec.Command("sh", "-c", c).CombinedOutput()
		Expect(err).NotTo(HaveOccurred(), "command failed: %s\n%s", c, out)
	}

	content, err := os.ReadFile(inittab)
	Expect(err).NotTo(HaveOccurred())
	return string(content)
}

var _ = Describe("Bundled autologin cloudconfig, openrc", func() {
	// 25_autologin.yaml gives a live boot a root console without a password.
	// Its openrc half rewrote tty1 and ttyS0 only, so Alt+F2 onwards still
	// presented a login prompt. That is worst on an interactive install, which
	// owns tty1 and leaves the other virtual consoles as the only shell an
	// advanced user has.
	//
	// Whether this stage runs at all is pinned by the 25_autologin.yaml specs
	// in cloudconfigs_install_mode_guard_test.go.
	var inittab string

	BeforeEach(func() {
		inittab = runOpenRCAutologin()
	})

	It("autologs in on every virtual console, not only tty1", func() {
		for n := 1; n <= 6; n++ {
			tty := fmt.Sprintf("tty%d", n)
			Expect(inittab).To(MatchRegexp(`(?m)^%s::respawn:.*--autologin root.*%s$`, tty, tty),
				"nothing autologs in on %s:\n%s", tty, inittab)
		}
	})

	It("autologs in on the serial console", func() {
		Expect(inittab).To(MatchRegexp(`(?m)^ttyS0::respawn:.*--autologin root.*ttyS0$`),
			"nothing autologs in on ttyS0:\n%s", inittab)
	})

	It("leaves no stock getty behind on a console it took over", func() {
		// The stage appends, so a line it failed to strip would sit above its
		// replacement and keep respawning a password prompt there.
		Expect(inittab).NotTo(ContainSubstring("/sbin/getty"),
			"a stock getty survived the rewrite:\n%s", inittab)
	})

	It("writes one entry per console", func() {
		for _, tty := range []string{"tty1", "tty2", "tty3", "tty4", "tty5", "tty6", "ttyS0"} {
			matches := regexp.MustCompile(`(?m)^`+tty+`::`).FindAllString(inittab, -1)
			Expect(matches).To(HaveLen(1), "%s has %d entries, want 1:\n%s", tty, len(matches), inittab)
		}
	})

	It("keeps the openrc runlevel lines", func() {
		// The sed deletes from the first match to the end of the line, so a
		// pattern that is too wide takes the sysinit block with it.
		for _, line := range []string{
			"::sysinit:/sbin/openrc sysinit",
			"::sysinit:/sbin/openrc boot",
			"::wait:/sbin/openrc default",
			"::shutdown:/sbin/openrc shutdown",
		} {
			Expect(inittab).To(ContainSubstring(line), "the rewrite ate %q:\n%s", line, inittab)
		}
	})
})
