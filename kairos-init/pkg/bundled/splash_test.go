package bundled_test

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/kairos-io/kairos/v4/kairos-init/pkg/bundled"
	"github.com/kairos-io/kairos/v4/sdk/state"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/twpayne/go-vfs/v4/vfst"
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

// menuentry returns the body of one grub menuentry block, selected by its
// --id, so an assertion about what the recovery entry boots cannot be
// satisfied by a line belonging to the entry above it.
func menuentry(cfg, id string) string {
	start := strings.Index(cfg, "--id "+id+" {")
	if start < 0 {
		return ""
	}
	rest := cfg[start:]
	if end := strings.Index(rest, "\n}"); end >= 0 {
		return rest[:end]
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
		Expect(unitSection()).To(ContainSubstring(
			"ConditionPathExists=" + bundled.SplashBinaryPath + "\n"))
	})

	// The unit execs the replaceable path, never /usr/bin/kairos: that is what
	// lets a downstream swap the animation by dropping its own executable
	// there. `ExecStart=/usr/bin/kairos splash` would work identically on a
	// stock image and ignore the replacement on a rebuilt one.
	It("draws on tty1 through the replaceable splash path", func() {
		svc := section(bundled.SplashServiceDracut, "Service")
		Expect(svc).To(ContainSubstring("TTYPath=/dev/tty1"))
		Expect(svc).To(ContainSubstring("ExecStart=" + bundled.SplashBinaryPath + "\n"))
		Expect(svc).ToNot(ContainSubstring("/usr/bin/kairos "))
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

	// getty@.service ships Before=getty.target itself, so ordering the splash
	// before the target only makes the two siblings of it: nothing keeps
	// getty@tty1.service from starting while the splash still animates, and
	// the login prompt is then drawn over it. The unit has to be ordered
	// before the getty instance that owns the console it writes to.
	It("is ordered before the getty on the console it writes to", func() {
		u := section(unit(), "Unit")
		Expect(section(unit(), "Service")).To(ContainSubstring("TTYPath=/dev/tty1"))
		Expect(u).To(ContainSubstring("Before=getty@tty1.service"))
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

	// Recovery is the boot a human takes to read what went wrong, so it is the
	// one boot that must never have its console covered. The carve-out cannot
	// be left to the command line: `splash` lives in BootArgsCfg's baseCmd,
	// which is the part every menuentry shares, so the recovery entry asks for
	// an animation just as the active entry does.
	It("stays off a recovery boot", func() {
		Expect(section(unit(), "Unit")).
			To(ContainSubstring("ConditionPathExists=!/run/cos/recovery_mode"))
	})

	It("is installable into multi-user.target", func() {
		Expect(section(unit(), "Install")).To(ContainSubstring("WantedBy=multi-user.target"))
	})

	// Same reason as the initramfs unit: this is the path a downstream
	// replaces, and it has to be the path the unit execs and gates on.
	It("execs and gates on the replaceable splash path", func() {
		Expect(section(unit(), "Service")).
			To(ContainSubstring("ExecStart=" + bundled.SplashBinaryPath + " "))
		Expect(section(unit(), "Unit")).
			To(ContainSubstring("ConditionPathExists=" + bundled.SplashBinaryPath + "\n"))
		Expect(unit()).ToNot(ContainSubstring("/usr/bin/kairos "))
	})
})

// The override is only an override if every place the feature names an
// executable names the same one. A single leftover /usr/bin/kairos would make
// one half of the boot ignore a downstream's splash, which is worse than not
// supporting the override at all: the animation would change at switch-root.
var _ = Describe("the splash binary path", func() {
	It("is the only executable the splash wiring names", func() {
		for name, text := range map[string]string{
			"SplashServiceDracut":     bundled.SplashServiceDracut,
			"SplashService":           fmt.Sprintf(bundled.SplashService, bundled.SplashDuration),
			"SplashModuleSetupDracut": bundled.SplashModuleSetupDracut,
		} {
			Expect(text).To(ContainSubstring(bundled.SplashBinaryPath), name)
			// Comment lines in the module-setup script explain the symlink,
			// so they name /usr/bin/kairos legitimately. Only the directives
			// matter here. Then strip every mention of the splash path, so
			// what is left is a bare /usr/bin/kairos only if one is really
			// being exec'd or installed.
			var directives []string
			for _, line := range strings.Split(text, "\n") {
				if !strings.HasPrefix(strings.TrimSpace(line), "#") {
					directives = append(directives, line)
				}
			}
			bare := strings.ReplaceAll(strings.Join(directives, "\n"),
				bundled.SplashBinaryPath, "")
			Expect(bare).ToNot(MatchRegexp(`/usr/bin/kairos\b`), name)
		}
	})

	// The units exec this path directly, with no sub-tool argument, so the
	// multi-call binary has to pick the splash from argv[0] alone. That alias
	// lives in cmd/kairos/register_splash.go and is asserted there; here we
	// only pin the basename the alias has to match.
	It("is named after the sub-tool the multi-call binary dispatches on", func() {
		Expect(filepath.Base(bundled.SplashBinaryPath)).To(Equal("kairos-splash"))
		Expect(filepath.Dir(bundled.SplashBinaryPath)).To(Equal("/usr/bin"))
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

	// plymouth activates on the same `splash` token, so leaving it in would
	// give /dev/tty1 two owners on a base image that ships it.
	It("keeps plymouth out of the initramfs", func() {
		Expect(bundled.SplashDracutConfig).To(ContainSubstring(`omit_dracutmodules+=" plymouth "`))
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
	// no /usr/bin/kairos, so kairos-init installs no splash symlink either.
	// check() failing there is what keeps the initramfs from getting a unit
	// whose ExecStart does not exist.
	//
	// The check is on the replaceable path and not on the multi-call binary,
	// so an image whose splash is a downstream executable rather than a
	// symlink to kairos is still included.
	It("excludes itself when the splash binary is missing", func() {
		status, recorded := run("check", 1)
		Expect(status).ToNot(Equal(0))
		Expect(recorded).To(ContainSubstring("require_binaries " + bundled.SplashBinaryPath + "\n"))
		Expect(recorded).ToNot(ContainSubstring("require_binaries /usr/bin/kairos\n"))
	})

	It("includes itself when the binary is there", func() {
		status, _ := run("check", 0)
		Expect(status).To(Equal(0))
	})

	// inst_simple would copy the ELF without the shared libraries it needs
	// and the unit would fail to exec inside the initramfs. inst_multiple on
	// the splash path also carries the symlink plus its target, so the
	// initramfs half execs the same replaceable name the booted system does.
	It("installs the splash binary with its libraries", func() {
		_, recorded := run("install", 0)
		Expect(recorded).To(ContainSubstring("inst_multiple " + bundled.SplashBinaryPath + "\n"))
		Expect(recorded).ToNot(ContainSubstring("inst_multiple /usr/bin/kairos\n"))
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

	// This is why SplashService needs a recovery_mode carve-out rather than a
	// command line that simply leaves the token out. `splash` sits in baseCmd,
	// documented as shared between all entries, and every menuentry sources
	// bootargs.cfg and boots the one $kernelcmd it builds. So the recovery and
	// state reset entries ask for an animation in exactly the same words the
	// active entry uses, and no per-entry cmdline distinguishes them.
	It("gives every menuentry the same splash token, recovery included", func() {
		Expect(bundled.BootArgsCfg).To(MatchRegexp(`set baseCmd="[^"]*\bsplash\b`))

		for _, id := range []string{"cos", "fallback", "recovery", "statereset"} {
			entry := menuentry(bundled.GrubCfg, id)
			Expect(entry).ToNot(BeEmpty(), "no menuentry --id %s", id)
			Expect(entry).To(ContainSubstring("source (loop0)/etc/cos/bootargs.cfg"), id)
			Expect(entry).To(ContainSubstring("$kernel $kernelcmd"), id)
			Expect(entry).ToNot(ContainSubstring("kairos.splash=0"), id)
		}
	})
})

// kernelCmdlineAllows evaluates every ConditionKernelCommandLine= of a unit
// section against a command line the way systemd does: a value with a "="
// must equal a whole word, a value without one matches the bare word or any
// word that assigns it, and a leading "!" negates. The conditions are ANDed.
func kernelCmdlineAllows(unitSection, cmdline string) bool {
	words := strings.Fields(cmdline)
	for _, line := range strings.Split(unitSection, "\n") {
		value, ok := strings.CutPrefix(strings.TrimSpace(line), "ConditionKernelCommandLine=")
		if !ok {
			continue
		}
		// A triggering condition ("|") is ORed rather than ANDed. None of the
		// units use one, and this evaluator would get it wrong.
		Expect(value).ToNot(HavePrefix("|"), line)
		value, negate := strings.CutPrefix(value, "!")
		found := false
		for _, w := range words {
			if strings.Contains(value, "=") {
				found = found || w == value
			} else {
				found = found || w == value || strings.HasPrefix(w, value+"=")
			}
		}
		if found == negate {
			return false
		}
	}
	return true
}

// grubCmdline builds the kernel command line a GrubCfg menuentry boots, out
// of the same pieces GRUB uses: the entry's own label and image, BootArgsCfg's
// baseCmd and the baseRootCmd for that image, and whatever the entry appends
// after the extra_*_cmdline variables. squashfs picks the recovery image
// shipped as a squashfs (root=live:...) over the .img file.
func grubCmdline(id string, squashfs bool) string {
	entry := menuentry(bundled.GrubCfg, id)
	Expect(entry).ToNot(BeEmpty(), "no menuentry --id %s", id)

	vars := map[string]string{
		"label": regexp.MustCompile(`set label=(\S+)`).FindStringSubmatch(entry)[1],
	}
	imgs := regexp.MustCompile(`set img=(\S+)`).FindAllStringSubmatch(entry, -1)
	Expect(imgs).ToNot(BeEmpty(), id)
	vars["img"] = imgs[len(imgs)-1][1]
	rootCmd := regexp.MustCompile(`set baseRootCmd="(root=LABEL[^"]*)"`)
	if squashfs {
		Expect(entry).To(ContainSubstring("set recoverylabel="), id)
		vars["img"] = imgs[0][1]
		vars["recoverylabel"] = regexp.MustCompile(`set recoverylabel=(\S+)`).FindStringSubmatch(entry)[1]
		rootCmd = regexp.MustCompile(`set baseRootCmd="(root=live:[^"]*)"`)
	}

	base := regexp.MustCompile(`set baseCmd="([^"]*)"`).FindStringSubmatch(bundled.BootArgsCfg)
	Expect(base).ToNot(BeNil())
	root := rootCmd.FindStringSubmatch(bundled.BootArgsCfg)
	Expect(root).ToNot(BeNil())
	tail := regexp.MustCompile(`\$kernelcmd \$\{extra_cmdline\} \$\{extra_\w+_cmdline\}(.*)`).
		FindStringSubmatch(entry)
	Expect(tail).ToNot(BeNil(), id)

	cmdline := base[1] + " " + root[1] + tail[1]
	for k, v := range vars {
		cmdline = strings.ReplaceAll(cmdline, "$"+k, v)
	}
	Expect(cmdline).ToNot(ContainSubstring("$"), id)
	return cmdline
}

// immucore decides what kind of boot this is from the command line and
// writes /run/cos/recovery_mode or autoreset_mode, which is what keeps the
// booted-system unit off those boots. The initramfs unit starts before that
// file exists, so it has to reach the same answer from the command line on
// its own. These specs boot every GRUB entry through both and require them to
// agree: the splash runs exactly on the boots immucore calls active or
// passive.
var _ = Describe("SplashServiceDracut on each boot entry", func() {
	unitSection := func() string { return section(bundled.SplashServiceDracut, "Unit") }

	bootState := func(cmdline string) state.Boot {
		fs, cleanup, err := vfst.NewTestFS(map[string]interface{}{"/proc/cmdline": cmdline})
		Expect(err).ToNot(HaveOccurred())
		defer cleanup()
		b, err := state.DetectBootWithVFS(fs)
		Expect(err).ToNot(HaveOccurred())
		return b
	}

	DescribeTable("animates only where immucore will not write recovery_mode or autoreset_mode",
		func(id string, squashfs bool, extra string, want state.Boot) {
			cmdline := grubCmdline(id, squashfs) + extra
			Expect(bootState(cmdline)).To(Equal(want), cmdline)
			Expect(kernelCmdlineAllows(unitSection(), cmdline)).To(
				Equal(want == state.Active || want == state.Passive), cmdline)
		},
		Entry("active", "cos", false, "", state.Active),
		Entry("fallback", "fallback", false, "", state.Passive),
		Entry("recovery from recovery.img", "recovery", false, "", state.Recovery),
		Entry("recovery from recovery.squashfs", "recovery", true, "", state.Recovery),
		Entry("state reset from recovery.img", "statereset", false, "", state.AutoReset),
		Entry("state reset from recovery.squashfs", "statereset", true, "", state.AutoReset),
		// No shipped entry adds kairos.reset to an active root, but immucore
		// reads it ahead of every other marker, so a custom grubmenu entry that
		// does must not get an animation either.
		Entry("an active root with kairos.reset", "cos", false, " kairos.reset", state.AutoReset),
	)

	It("still needs the splash word on an active boot", func() {
		cmdline := strings.Replace(grubCmdline("cos", false), " splash ", " ", 1)
		Expect(cmdline).ToNot(MatchRegexp(`\bsplash\b`))
		Expect(kernelCmdlineAllows(unitSection(), cmdline)).To(BeFalse())
	})
})

// grubToBash rewrites the subset of GRUB script BootArgsCfg uses into bash, so
// that a test can run the real thing and read the command line it builds
// rather than pattern-match its source.
//
// Three rewrites, and no others. Each one is a syntax difference, never a
// behaviour one:
//
//  1. "(loop0)" is GRUB's name for the mounted system image. It becomes
//     $LOOP0, a directory the test fills with os-release and kairos-release,
//     so the "source" lines and the [ -f ] guard run for real.
//  2. GRUB spells assignment "set var=value"; bash spells it "var=value".
//  3. GRUB's test is a token-stream parser, so the "-o test" in setSelinux
//     parses as "-o" followed by a one-argument test of the word "test",
//     which is a non-empty string and therefore true, ANDed with the
//     comparison that follows. bash's test rejects the extra word outright.
//     Dropping it keeps GRUB's meaning.
//
// Anything else has to run as written. The caller asserts on an empty stderr,
// which is what stops a construct this does not model from passing silently.
func grubToBash(script string) string {
	script = strings.ReplaceAll(script, "(loop0)", "${LOOP0}")
	script = strings.ReplaceAll(script, " -o test ", " -o ")
	script = strings.ReplaceAll(script, " -a test ", " -a ")

	lines := strings.Split(script, "\n")
	for i, line := range lines {
		trimmed := strings.TrimLeft(line, " \t")
		indent := line[:len(line)-len(trimmed)]
		if rest, ok := strings.CutPrefix(trimmed, "set "); ok && strings.Contains(rest, "=") {
			lines[i] = indent + rest
		}
	}
	return strings.Join(lines, "\n")
}

// entryLabel reads the "set label=" a menuentry assigns before it sources
// bootargs.cfg. The quiet carve-out keys off that value, so the test takes it
// from GrubCfg instead of restating it: moving an entry to a different label
// has to show up here.
func entryLabel(id string) string {
	for _, line := range strings.Split(menuentry(bundled.GrubCfg, id), "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "set label="); ok {
			return rest
		}
	}
	return ""
}

var _ = Describe("BootArgsCfg quiet", func() {
	var dir string

	BeforeEach(func() {
		dir = GinkgoT().TempDir()
		Expect(os.MkdirAll(filepath.Join(dir, "loop0", "etc"), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(dir, "loop0", "etc", "os-release"),
			[]byte("ID=alpine\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(dir, "loop0", "etc", "kairos-release"),
			[]byte("KAIROS_FAMILY=alpine\nKAIROS_MODEL=generic\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(dir, "bootargs.sh"),
			[]byte(grubToBash(bundled.BootArgsCfg)), 0o644)).To(Succeed())
	})

	// cmdline runs bootargs.cfg with the variables a menuentry has set by the
	// time it sources the file, and returns the $kernelcmd it builds.
	cmdline := func(label, recoverylabel, img string) string {
		script := filepath.Join(dir, "bootargs.sh")
		cmd := exec.Command("bash", "-c", "source "+script+"; printf '%s' \"$kernelcmd\"")
		cmd.Env = append(os.Environ(),
			"LOOP0="+filepath.Join(dir, "loop0"),
			"label="+label,
			"recoverylabel="+recoverylabel,
			"img="+img,
			"selinux_enabled=false",
			"selinux_mode=permissive",
		)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		Expect(err).ToNot(HaveOccurred(), stderr.String())
		// An untranslated GRUB construct shows up here as a bash diagnostic,
		// and a silently mis-evaluated branch is exactly the failure this
		// whole helper would otherwise hide.
		Expect(stderr.String()).To(BeEmpty())
		Expect(string(out)).ToNot(BeEmpty())
		return string(out)
	}

	// The splash cannot cover the text that is printed before it can draw:
	// the kernel starts logging at its own loglevel long before PID 1, and
	// the initramfs unit has to wait for udev to create /dev/tty1. quiet on
	// the command line is what closes that window.
	It("asks for quiet on the entries that animate", func() {
		for _, id := range []string{"cos", "fallback"} {
			label := entryLabel(id)
			Expect(label).ToNot(BeEmpty(), "no label in menuentry %s", id)
			Expect(cmdline(label, "", "/cOS/"+id+".img")).To(
				MatchRegexp(`(^|\s)quiet(\s|$)`), id)
		}
	})

	// Recovery and the state reset are where someone goes to read a boot that
	// went wrong, and the booted-system animation is already refused there by
	// SplashService's /run/cos sentinels. Both entries boot the recovery
	// image, as a squashfs when there is one and as a .img when there is not,
	// and only the squashfs case sets recoverylabel. The carve-out has to
	// cover both.
	It("leaves recovery and the state reset verbose", func() {
		for _, id := range []string{"recovery", "statereset"} {
			Expect(entryLabel(id)).To(Equal("COS_SYSTEM"), id)
		}
		for _, recoverylabel := range []string{"COS_RECOVERY", ""} {
			Expect(cmdline("COS_SYSTEM", recoverylabel, "/cOS/recovery.img")).ToNot(
				MatchRegexp(`(^|\s)quiet(\s|$)`), recoverylabel)
		}
	})

	// quiet would be pointless on an entry that does not ask for the splash,
	// and misleading on one that does not get it, so the two tokens have to
	// keep travelling together on the animated entries.
	It("keeps quiet and splash on the same entries", func() {
		Expect(cmdline(entryLabel("cos"), "", "/cOS/active.img")).To(
			MatchRegexp(`(^|\s)splash(\s|$)`))
	})
})
