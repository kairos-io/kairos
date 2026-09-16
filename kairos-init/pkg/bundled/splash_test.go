package bundled_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/kairos-io/kairos/v4/kairos-init/pkg/bundled"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// section returns the body of one ini section of a unit file, so an assertion
// about Conflicts= cannot be satisfied by the word appearing in a comment or
// in the wrong section.
func section(unit, name string) string {
	start := strings.Index(unit, "["+name+"]")
	if start < 0 {
		return ""
	}
	rest := unit[start+len(name)+2:]
	if next := strings.Index(rest, "\n["); next >= 0 {
		return rest[:next]
	}
	return rest
}

var _ = Describe("SplashServiceDracut", func() {
	unitSection := func() string { return section(bundled.SplashServiceDracut, "Unit") }

	// The animation owns tty1 and repaints every frame. Anything that needs
	// the console after the initramfs has to be able to take it back, and
	// both of these are cases where leaving an animation up hides the only
	// thing worth reading.
	It("gives the console back at switch-root and on an emergency", func() {
		Expect(unitSection()).To(ContainSubstring("Conflicts=initrd-switch-root.target"))
		Expect(unitSection()).To(ContainSubstring("Conflicts=emergency.target"))
	})

	// The splash is cosmetic. A boot must not be able to stop because of it,
	// which rules out Requires= anywhere in the unit and is why the module
	// links it into initrd.target.wants.
	It("never hard-depends on anything", func() {
		Expect(bundled.SplashServiceDracut).ToNot(ContainSubstring("Requires="))
	})

	It("paints before the initramfs target is up", func() {
		Expect(unitSection()).To(ContainSubstring("Before=initrd.target"))
	})

	// /dev/tty1 only exists once udev has run. Earlier than this and the unit
	// fails to open its console on every boot.
	It("waits for udev to have made the console", func() {
		Expect(unitSection()).To(ContainSubstring("After=systemd-udev-trigger.service"))
	})

	It("is inert unless the command line asks for a splash", func() {
		Expect(unitSection()).To(ContainSubstring("ConditionKernelCommandLine=splash"))
		Expect(unitSection()).To(ContainSubstring("ConditionPathExists=/usr/bin/kairos"))
	})

	It("draws on tty1 with the multi-call binary", func() {
		svc := section(bundled.SplashServiceDracut, "Service")
		Expect(svc).To(ContainSubstring("TTYPath=/dev/tty1"))
		Expect(svc).To(ContainSubstring("ExecStart=/usr/bin/kairos splash\n"))
	})

	// Zero duration means "until SIGTERM", which is what the initramfs wants:
	// the animation lasts exactly as long as immucore takes to mount, however
	// long that is. A --duration here would end it early and leave systemd
	// output scrolling on a blank screen for the rest of the initramfs.
	It("does not bound the animation", func() {
		Expect(bundled.SplashServiceDracut).ToNot(ContainSubstring("--duration"))
	})

	It("bounds how long the stop can take", func() {
		Expect(section(bundled.SplashServiceDracut, "Service")).
			To(ContainSubstring("TimeoutStopSec="))
	})
})

var _ = Describe("SplashService", func() {
	unit := func() string { return fmt.Sprintf(bundled.SplashService, bundled.SplashDuration) }

	It("substitutes a duration Go itself can parse", func() {
		d, err := time.ParseDuration(bundled.SplashDuration)
		Expect(err).ToNot(HaveOccurred())
		Expect(d).To(BeNumerically(">", 0))
		Expect(unit()).To(ContainSubstring("--duration=" + bundled.SplashDuration))
		Expect(unit()).ToNot(ContainSubstring("%s"))
	})

	// A oneshot ordered before getty.target means getty waits for it, so the
	// animation is never half-overwritten by a login prompt. It also means the
	// unit must end on its own, twice over: the binary's own --duration, and
	// TimeoutStartSec for the case where the console misbehaves and it does
	// not. Without both, a cosmetic unit can hold the boot open forever.
	It("always hands the console to getty", func() {
		Expect(section(unit(), "Unit")).To(ContainSubstring("Before=getty.target"))
		svc := section(unit(), "Service")
		Expect(svc).To(ContainSubstring("Type=oneshot"))
		Expect(svc).To(MatchRegexp(`--duration=\S`))
		Expect(svc).To(ContainSubstring("TimeoutStartSec="))
	})

	It("is a no-op when started a second time", func() {
		Expect(section(unit(), "Service")).To(ContainSubstring("RemainAfterExit=yes"))
	})

	// The live ISO runs the interactive installer on tty1 and an automatic
	// reset prints its own progress there. Drawing a logo over either is worse
	// than showing nothing.
	It("stays off the boots that own tty1 themselves", func() {
		u := section(unit(), "Unit")
		Expect(u).To(ContainSubstring("ConditionPathExists=!/run/cos/live_mode"))
		Expect(u).To(ContainSubstring("ConditionPathExists=!/run/cos/autoreset_mode"))
		Expect(u).To(ContainSubstring("ConditionKernelCommandLine=splash"))
	})

	It("is installable into multi-user.target", func() {
		Expect(section(unit(), "Install")).To(ContainSubstring("WantedBy=multi-user.target"))
	})
})

var _ = Describe("SplashImmucoreQuietDracut", func() {
	// Mount progress is what would print over the animation, so it goes to
	// the journal. A mount FAILURE is the reason an emergency shell is on
	// screen, so it must still reach the console: hiding it would turn a
	// cosmetic change into an undebuggable boot.
	It("hides immucore's progress but not its failures", func() {
		Expect(bundled.SplashImmucoreQuietDracut).To(ContainSubstring("StandardOutput=journal\n"))
		Expect(bundled.SplashImmucoreQuietDracut).ToNot(ContainSubstring("StandardOutput=journal+console"))
		Expect(bundled.SplashImmucoreQuietDracut).To(ContainSubstring("StandardError=journal+console"))
	})
})

var _ = Describe("SplashDracutConfig", func() {
	// add_dracutmodules leaves the module's own check() in charge, so an image
	// built with every binary pinned through --version-overrides (no
	// /usr/bin/kairos written at all) gets no module rather than a unit whose
	// ExecStart does not exist. force_add_dracutmodules would skip the check.
	It("adds the module without overriding its check", func() {
		Expect(bundled.SplashDracutConfig).To(ContainSubstring(`add_dracutmodules+=" kairos-splash "`))
		Expect(bundled.SplashDracutConfig).ToNot(ContainSubstring("force_add_dracutmodules"))
	})

	It("names the module the module-setup directory provides", func() {
		Expect(bundled.DracutSplashModuleSetupPath).
			To(Equal("/usr/lib/dracut/modules.d/50kairos-splash/module-setup.sh"))
		Expect(bundled.DracutSplashServicePath).
			To(HavePrefix(filepath.Dir(bundled.DracutSplashModuleSetupPath) + "/"))
		Expect(bundled.DracutSplashImmucoreQuietPath).
			To(HavePrefix(filepath.Dir(bundled.DracutSplashModuleSetupPath) + "/"))
	})
})

// The module-setup script is shell that only ever runs inside a dracut build,
// so nothing in CI would catch a syntax error or a renamed variable. Run it
// against stubs for the dracut API instead and assert on the calls it makes.
var _ = Describe("SplashModuleSetupDracut", func() {
	var dir, calls string

	// run sources the module with the dracut helpers stubbed out, calls one
	// of its functions and returns the exit status plus the recorded calls.
	run := func(fn string, requireBinaries int, env ...string) (int, string) {
		harness := fmt.Sprintf(`
set -u
declare initdir=%[1]s/initdir
declare moddir=%[1]s/moddir
declare systemdsystemunitdir=/usr/lib/systemd/system
require_binaries() { for b in "$@"; do echo "require_binaries $b" >> %[2]s; done; return %[3]d; }
inst_multiple() { for f in "$@"; do echo "inst_multiple $f" >> %[2]s; done; }
inst_simple() { echo "inst_simple $1 ${2:-}" >> %[2]s; }
ln_r() { echo "ln_r $1 $2" >> %[2]s; }
derror() { echo "derror $*" >> %[2]s; }
source %[1]s/module-setup.sh
%[4]s
`, dir, calls, requireBinaries, fn)
		cmd := exec.Command("bash", "-c", harness)
		cmd.Env = append(os.Environ(), env...)
		out, err := cmd.CombinedOutput()
		status := 0
		if ee, ok := err.(*exec.ExitError); ok {
			status = ee.ExitCode()
		} else {
			Expect(err).ToNot(HaveOccurred(), string(out))
		}
		recorded, rerr := os.ReadFile(calls)
		if rerr != nil {
			recorded = nil
		}
		return status, string(recorded) + string(out)
	}

	BeforeEach(func() {
		dir = GinkgoT().TempDir()
		calls = filepath.Join(dir, "calls")
		Expect(os.MkdirAll(filepath.Join(dir, "moddir"), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(dir, "module-setup.sh"),
			[]byte(bundled.SplashModuleSetupDracut), 0o755)).To(Succeed())
	})

	It("is valid bash", func() {
		out, err := exec.Command("bash", "-n", filepath.Join(dir, "module-setup.sh")).CombinedOutput()
		Expect(err).ToNot(HaveOccurred(), string(out))
	})

	// An image whose binaries were all pinned through --version-overrides has
	// no /usr/bin/kairos. check() failing there is what keeps the initramfs
	// from getting a unit whose ExecStart does not exist.
	It("excludes itself when the multi-call binary is missing", func() {
		status, recorded := run("check", 1)
		Expect(status).ToNot(Equal(0))
		Expect(recorded).To(ContainSubstring("require_binaries /usr/bin/kairos"))
	})

	It("includes itself when the binary is there", func() {
		status, _ := run("check", 0)
		Expect(status).To(Equal(0))
	})

	// inst_simple would copy the ELF without the shared libraries it needs
	// and the unit would fail to exec inside the initramfs.
	It("installs the binary with its libraries", func() {
		_, recorded := run("install", 0)
		Expect(recorded).To(ContainSubstring("inst_multiple /usr/bin/kairos"))
	})

	// .wants, not .requires: a splash that cannot open /dev/tty1 (a
	// serial-only console, a kernel with no CONFIG_VT) must degrade to "no
	// animation" rather than leave initrd.target unactivatable, which stalls
	// the boot in the initramfs.
	It("wires the unit in as wanted, never as required", func() {
		_, recorded := run("install", 0)
		Expect(recorded).To(ContainSubstring(
			"ln_r ../kairos-splash.service /usr/lib/systemd/system/initrd.target.wants/kairos-splash.service"))
		Expect(recorded).ToNot(ContainSubstring("initrd.target.requires"))
		Expect(recorded).To(ContainSubstring(
			"inst_simple " + filepath.Join(dir, "moddir", "kairos-splash.service") +
				" /usr/lib/systemd/system/kairos-splash.service"))
	})

	// A drop-in, so it does not matter whether 28immucore ran before or after
	// this module: it never rewrites immucore.service itself.
	It("quiets immucore with a drop-in", func() {
		_, recorded := run("install", 0)
		Expect(recorded).To(ContainSubstring(
			"inst_simple " + filepath.Join(dir, "moddir", "immucore-quiet.conf") +
				" /usr/lib/systemd/system/immucore.service.d/10-quiet.conf"))
		Expect(recorded).ToNot(MatchRegexp(`inst_simple \S+immucore\.service `))
	})

	It("creates the wants and drop-in directories under initdir", func() {
		_, _ = run("install", 0)
		for _, d := range []string{
			"initdir/usr/lib/systemd/system/initrd.target.wants",
			"initdir/usr/lib/systemd/system/immucore.service.d",
		} {
			st, err := os.Stat(filepath.Join(dir, d))
			Expect(err).ToNot(HaveOccurred(), d)
			Expect(st.IsDir()).To(BeTrue(), d)
		}
	})

	// Branding is data: a downstream drops its own wordmark in
	// /etc/kairos/branding/splash and gets it in the initramfs too, without
	// forking this module.
	It("carries downstream branding into the initramfs", func() {
		art := filepath.Join(dir, "branding")
		Expect(os.MkdirAll(art, 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(art, "wordmark"), []byte("X\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(art, "palette"), []byte("94\n"), 0o644)).To(Succeed())
		_, recorded := run("install", 0, "splash_branding_dir="+art)
		Expect(recorded).To(ContainSubstring("inst_simple " + filepath.Join(art, "wordmark")))
		Expect(recorded).To(ContainSubstring("inst_simple " + filepath.Join(art, "palette")))
	})

	// An unbranded image is the normal case and the built-in artwork covers
	// it. The glob must not expand to a literal path and hand dracut a file
	// that does not exist.
	It("installs no artwork when the branding directory is absent", func() {
		_, recorded := run("install", 0,
			"splash_branding_dir="+filepath.Join(dir, "absent"))
		Expect(recorded).ToNot(ContainSubstring("absent"))
	})
})

var _ = Describe("BootArgsCfg", func() {
	// The units are gated on ConditionKernelCommandLine=splash, so without
	// this token nothing animates on an installed machine and the whole
	// feature is dead code.
	It("asks for the splash on the shared part of the command line", func() {
		Expect(bundled.BootArgsCfg).To(MatchRegexp(`set baseCmd="[^"]*\bsplash\b`))
	})
})
