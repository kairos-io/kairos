package netboot

import (
	"strings"
	"testing"

	"github.com/kairos-io/AuroraBoot/pkg/constants"
)

// netbootBase is the cmdline StartPixiecore serves when the user set no
// override, templates and all.
const netbootBase = `rd.live.overlay.overlayfs rd.neednet=1 ip=dhcp rd.cos.disable root=live:{{ ID "%s" }} netboot install-mode-interactive config_url={{ ID "%s" }} console=tty1 console=ttyS0 console=tty0`

func options(cmdline string) []string {
	return strings.Fields(pixiecoreTemplate.ReplaceAllString(cmdline, "URL"))
}

func has(cmdline, option string) bool {
	for _, o := range options(cmdline) {
		if o == option {
			return true
		}
	}

	return false
}

// The bug in kairos-io/kairos#2573: the ISO boots with selinux=0 and the
// netbooted node did not, because the netboot cmdline was hardcoded.
func TestLiveCmdlineAddsTheLivecdOptions(t *testing.T) {
	got := LiveCmdline(netbootBase, isoGrubCfg(t, "", ""))

	for _, want := range []string{"selinux=0", "net.ifnames=1", "vga=795", "nomodeset"} {
		if !has(got, want) {
			t.Errorf("%q is in the livecd cmdline but missing from %q", want, got)
		}
	}
}

// Options describing the medium the rootfs sits on, or selecting a boot mode,
// would either do nothing or fight with netboot on a netbooted node.
func TestLiveCmdlineDropsMediumAndModeOptions(t *testing.T) {
	got := LiveCmdline(netbootBase, isoGrubCfg(t, "", ""))

	for _, unwanted := range []string{"cdroot", "rd.live.dir=/", "rd.live.squashimg=rootfs.squashfs", "install-mode"} {
		if has(got, unwanted) {
			t.Errorf("%q only applies to a physical medium but ended up in %q", unwanted, got)
		}
	}
}

// --extend-live-cmdline is meant to reach the booted system whichever way it
// boots, and until now it stopped at the ISO.
func TestLiveCmdlineCarriesTheExtendedCmdline(t *testing.T) {
	got := LiveCmdline(netbootBase, isoGrubCfg(t, "", " foo=bar"))

	if !has(got, "foo=bar") {
		t.Errorf("--extend-live-cmdline was dropped: %q", got)
	}
}

// root= points at the squashfs pixiecore serves, so the livecd's CDLABEL root
// must not survive, and neither must any other key netboot already sets.
func TestLiveCmdlineKeepsTheNetbootOptions(t *testing.T) {
	got := LiveCmdline(netbootBase, "menuentry x {\n linux ($root)/boot/kernel root=live:CDLABEL=COS_LIVE ip=static rd.cos.disable\n}")

	if strings.Contains(got, "CDLABEL") {
		t.Errorf("the livecd root= overrode the served one: %q", got)
	}
	if has(got, "ip=static") {
		t.Errorf("the livecd ip= overrode the netboot one: %q", got)
	}
	if strings.Count(got, "rd.cos.disable") != 1 {
		t.Errorf("rd.cos.disable was appended a second time: %q", got)
	}
}

// $thor_options is resolved by grub at boot and {{NOMODESET}} at ISO build
// time. Either would reach the kernel as literal text.
func TestLiveCmdlineDropsUnexpandedPlaceholders(t *testing.T) {
	got := LiveCmdline(netbootBase, "menuentry x {\n linux ($root)/boot/kernel selinux=0 $thor_options {{NOMODESET}}\n}")

	if added := strings.TrimPrefix(got, netbootBase); strings.ContainsAny(added, "${}") {
		t.Errorf("an unexpanded placeholder reached the cmdline: %q", got)
	}
	if !has(got, "selinux=0") {
		t.Errorf("the real options around the placeholder were dropped too: %q", got)
	}
}

// --live-console renders into the livecd cmdline as the console= the user wants
// to be the primary one, and the kernel gives that to the last console= it
// sees, so it has to be kept even though base already sets three.
func TestLiveCmdlineKeepsTheLiveConsole(t *testing.T) {
	got := LiveCmdline(netbootBase, isoGrubCfg(t, "console=ttyS1,115200", ""))

	if !has(got, "console=ttyS1,115200") {
		t.Errorf("--live-console was dropped: %q", got)
	}
	if lastConsole(got) != "console=ttyS1,115200" {
		t.Errorf("--live-console is not the last console=, so it is not the primary one: %q", got)
	}
}

// Not every ISO carries a livecd grub config, and AuroraBoot must keep
// netbooting the ones that do not.
func TestLiveCmdlineWithoutAKernelLine(t *testing.T) {
	for name, cfg := range map[string]string{
		"empty":             "",
		"no menuentry":      "set default=0\nset timeout=10\n",
		"kernel only":       "menuentry x {\n linux\n}",
		"no kernel options": "menuentry x {\n linux ($root)/boot/kernel\n}",
	} {
		if got := LiveCmdline(netbootBase, cfg); got != netbootBase {
			t.Errorf("%s: cmdline changed to %q, want it untouched", name, got)
		}
	}
}

func TestDefaultEntryOptionsTakesTheFirstEntry(t *testing.T) {
	cfg := `menuentry "Kairos" {
    linux ($root)/boot/kernel install-mode selinux=0
}
menuentry "Kairos (debug)" {
    linuxefi ($root)/boot/kernel rd.debug rd.shell
}`

	got := strings.Join(defaultEntryOptions(cfg), " ")
	if got != "install-mode selinux=0" {
		t.Errorf("options = %q, want the first entry's", got)
	}
}

// isoGrubCfg renders the livecd config AuroraBoot ships the way the ISO build
// does, on amd64. It renders the go:embed'd template itself, so the test
// follows the template when it changes and a move of it breaks the compile
// rather than the test run. Kept in step with applyGrubTemplate in
// pkg/ops/iso.go, which cannot be imported from here: pkg/ops already imports
// this package. pkg/constants can, it imports nothing from AuroraBoot.
func isoGrubCfg(t *testing.T, liveConsole, extendCmdline string) string {
	t.Helper()

	if liveConsole == "" {
		liveConsole = "console=ttyS0 console=tty1"
	}
	out := strings.ReplaceAll(string(constants.GrubLiveBiosCfg), "{{NOMODESET}}", " nomodeset")
	out = strings.ReplaceAll(out, "{{EXTEND_CMDLINE}}", extendCmdline)

	return strings.ReplaceAll(out, "{{LIVE_CONSOLE}}", liveConsole)
}

// lastConsole returns the console= the kernel will make the primary one.
func lastConsole(cmdline string) string {
	last := ""
	for _, o := range options(cmdline) {
		if optionKey(o) == "console" {
			last = o
		}
	}

	return last
}

// The livecd config defaults to console=ttyS0 console=tty1, both of which the
// netboot cmdline already sets. Appending them again would make tty1 the
// primary console instead of tty0, which is not a change this fix should make.
func TestLiveCmdlineDoesNotMoveThePrimaryConsole(t *testing.T) {
	before := lastConsole(netbootBase)

	got := LiveCmdline(netbootBase, isoGrubCfg(t, "", ""))

	if after := lastConsole(got); after != before {
		t.Errorf("primary console moved from %q to %q: %q", before, after, got)
	}
	for _, o := range []string{"console=ttyS0", "console=tty1"} {
		if n := strings.Count(" "+got+" ", " "+o+" "); n != 1 {
			t.Errorf("%q appears %d times, want 1: %q", o, n, got)
		}
	}
}
