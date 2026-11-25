// Package check says whether a Linux system can become a Kairos system.
//
// It reads the running system or an unpacked root filesystem and gives a
// verdict for each requirement: pass, warn or fail. It changes nothing, needs
// no network and does not need root.
//
// The rules are the ones the rest of kairos-init applies, and they come from
// the same code: the distribution and its family from system.DetectFromValues,
// the package manager from yip's package step, the kernel packages from
// values.GetKernelPackagesFor, the stages that build the initramfs from
// stages.OSDracutFamilies, the files the agent copies to the EFI partition
// from sdk/utils, and the GRUB modules from the agent constants. What is left
// in this package is what no other code states: which tools and kernel modules
// the Kairos components need, and which releases of a distribution lack
// packages.
package check

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"regexp"
	"runtime"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
	agentconstants "github.com/kairos-io/kairos/v4/agent/pkg/constants"
	"github.com/kairos-io/kairos/v4/kairos-init/pkg/bundled"
	"github.com/kairos-io/kairos/v4/kairos-init/pkg/kernel"
	"github.com/kairos-io/kairos/v4/kairos-init/pkg/stages"
	"github.com/kairos-io/kairos/v4/kairos-init/pkg/system"
	"github.com/kairos-io/kairos/v4/kairos-init/pkg/validation"
	"github.com/kairos-io/kairos/v4/kairos-init/pkg/values"
	"github.com/kairos-io/kairos/v4/sdk/types/logger"
	"github.com/kairos-io/kairos/v4/sdk/utils"
)

var (
	alpineFamily = values.AlpineFamily.String()
	archAMD64    = values.ArchAMD64.String()
	archARM64    = values.ArchARM64.String()
	archRISCV64  = values.ArchRiscV64.String()
)

const (
	levelRequired = "required"
	levelOptional = "optional"
)

// Options say what to check.
type Options struct {
	// Root is an unpacked root filesystem. Empty means the running system.
	Root string
	// Arch is the architecture of Root, as `uname -m` or Go print it. Empty
	// means the one this program runs on.
	Arch string
	// TrustedBoot checks for a Trusted Boot (UKI) image, which has no GRUB and
	// no dracut initramfs.
	TrustedBoot bool
	// Converted says the system already went through kairos-init, so anything
	// missing is missing for good.
	Converted bool
	// Model is the model kairos-init builds for. Empty means generic. The
	// package lists of a board model are not judged: only the generic model is.
	Model string
	// SkipSteps are the steps of kairos-init that will not run (--skip-step). A
	// requirement that only a skipped step would meet is judged on the system
	// as it is.
	SkipSteps []string
	// Extensions is --stage-extensions: stage extensions on disk can bring tools
	// the check cannot see, so a missing tool is a warning.
	Extensions bool
}

// skipped is whether a step will not run: the step itself or the stage it is in.
func (o Options) skipped(step string, stage string) bool {
	for _, s := range o.SkipSteps {
		if s == step || s == stage {
			return true
		}
	}
	return false
}

// ErrNotDirectory is returned when Options.Root is not a directory.
var ErrNotDirectory = errors.New("not a directory")

// systemdVersion asks a systemd binary of the running system for its version.
// It is a variable for the tests.
var systemdVersion = func(path string) (int, error) {
	out, err := exec.Command(path, "--version").Output()
	if err != nil {
		return 0, err
	}
	fields := strings.Fields(string(out))
	if len(fields) < 2 {
		return 0, fmt.Errorf("unexpected output %q", out)
	}
	return strconv.Atoi(strings.TrimRight(fields[1], "abcdefghijklmnopqrstuvwxyz~-"))
}

type checker struct {
	opts   Options
	fs     rootFS
	report *Report
	log    logger.KairosLogger

	section string
	sys     values.System
	// family and distro are what kairos-init makes of the system, empty if it
	// knows neither.
	family string
	distro string
	id     string
	// version is VERSION_ID as it is in os-release.
	version    string
	pretty     string
	initramfs  string
	arch       string
	encryption string
	systemd    string
	openrc     string
	kernelDir  string
	// kernelComes is whether the kernel steps install the kernel.
	kernelComes bool
}

// Run checks the system.
func Run(opts Options) (*Report, error) {
	if opts.Root != "" {
		info, err := os.Stat(opts.Root)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil, fmt.Errorf("%s: %w", opts.Root, ErrNotDirectory)
			}
			return nil, err
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("%s: %w", opts.Root, ErrNotDirectory)
		}
	}
	if opts.Model == "" {
		opts.Model = values.Generic.String()
	}
	c := &checker{
		opts:       opts,
		fs:         newRootFS(opts.Root),
		report:     &Report{converted: opts.Converted},
		log:        logger.NewNullLogger(),
		encryption: levelOptional,
	}
	if opts.TrustedBoot {
		c.encryption = levelRequired
	}
	c.identity()
	c.platform()
	c.kernelSection()
	c.boot()
	c.tools()
	return c.report, nil
}

// --- reporting ---------------------------------------------------------------

func (c *checker) add(status Status, format string, args ...any) {
	switch status {
	case Warn:
		c.report.warns++
	case Fail:
		c.report.fails++
	}
	c.report.Findings = append(c.report.Findings, Finding{Section: c.section, Status: status, Message: fmt.Sprintf(format, args...)})
}

func (c *checker) pass(format string, args ...any) { c.add(Pass, format, args...) }
func (c *checker) warn(format string, args ...any) { c.add(Warn, format, args...) }
func (c *checker) fail(format string, args ...any) { c.add(Fail, format, args...) }

// failIdentity is a fail about what the tools make of the name and the version,
// and not about what is in the system.
func (c *checker) failIdentity(format string, args ...any) {
	c.report.identityFails++
	c.fail(format, args...)
}

// missing reports something that is not there. A base container image has next
// to nothing installed, so what the package list of the family brings counts as
// there.
func (c *checker) missing(level, what, why string) {
	brought := c.report.installs
	// The kernel and its modules come from the kernel step.
	if what == "/lib/modules" || what == "a kernel under /lib/modules" || strings.HasPrefix(what, "module ") {
		brought = c.kernelComes
	}
	// What the package lists leave out.
	switch what {
	case "systemd-cryptenroll", "systemd-sysext":
		if c.sys.Family == values.RedHatFamily && c.majorVersion() == 8 {
			brought = false
		}
		if c.sys.Distro == values.Ubuntu && c.version == "20.04" {
			brought = false
		}
	case "mksquashfs":
		if c.opts.TrustedBoot {
			brought = false
		}
	}
	switch {
	case brought:
		c.pass("%s (%s): not installed yet, left to the packages for the %s family", what, why, c.family)
	case level == levelRequired && c.opts.Extensions:
		c.warn("%s is missing (%s), a stage extension may bring it", what, why)
	case level == levelRequired:
		c.fail("%s is missing (%s)", what, why)
	default:
		c.warn("%s is missing (%s)", what, why)
	}
}

// component says that one of the commands has to be there.
func (c *checker) component(level, why string, names ...string) {
	if path, ok := c.fs.findBin(names...); ok {
		c.pass("%s (%s): %s", names[0], why, path)
		return
	}
	c.missing(level, strings.Join(names, " or "), why)
}

func (c *checker) majorVersion() int {
	v, err := c.sys.ParsedVersion()
	if err != nil {
		return -1
	}
	return v.Segments()[0]
}

// --- identity ----------------------------------------------------------------

func (c *checker) identity() {
	c.section = "Identity: how the Kairos tools see this distribution"

	// The Kairos tools read /etc/os-release only, not /usr/lib/os-release.
	vals := map[string]string{}
	var parseErr error
	if file, err := c.fs.open("/etc/os-release"); err == nil {
		vals, parseErr = godotenv.Parse(file)
		_ = file.Close()
		if parseErr != nil {
			vals = map[string]string{}
		}
	}
	c.id = vals["ID"]
	c.version = vals["VERSION_ID"]
	// The stages match on PRETTY_NAME alone, with no fallback to NAME.
	c.pretty = vals["PRETTY_NAME"]
	c.report.ID = c.id

	c.arch = normalizeArch(c.opts.Arch)
	c.sys = system.DetectFromValues(c.log, vals, c.arch)

	if c.id == "" {
		if parseErr != nil {
			c.failIdentity("/etc/os-release cannot be read (%v), so no Kairos tool can tell what this system is", parseErr)
		} else {
			c.failIdentity("/etc/os-release has no ID, so no Kairos tool can tell what this system is")
		}
	} else {
		c.pass("os-release: %s (ID=%s, VERSION_ID=%s)", orNone(c.pretty, "no PRETTY_NAME"), c.id, orNone(c.version, "none"))
		switch {
		case c.sys.Distro == values.Unknown:
			c.failIdentity("family: kairos-init knows neither ID=%s nor ID_LIKE=%s", c.id, orNone(vals["ID_LIKE"], "none"))
		case c.sys.Distro.String() == c.id:
			c.family, c.distro = c.sys.Family.String(), c.sys.Distro.String()
			c.pass("family: %s (ID=%s)", c.family, c.id)
		default:
			c.family, c.distro = c.sys.Family.String(), c.sys.Distro.String()
			c.pass("family: %s, as a derivative of %s (ID_LIKE=%s)", c.family, c.distro, vals["ID_LIKE"])
		}
	}
	c.report.Family, c.report.Distro = c.family, c.distro

	c.packages()
	c.versions()
	c.initramfsStages()
}

// hasPackageList is whether kairos-init has a package list for the system.
func hasPackageList(s values.System) bool {
	return values.BasePackages[s.Distro] != nil || values.BasePackages[s.Family] != nil
}

func (c *checker) packages() {
	switch {
	case c.family == "":
		return
	case c.sys.Family == values.HadronFamily:
		c.pass("packages: Hadron ships everything, kairos-init installs no package on it")
		return
	}
	// What the install steps bring. A step that does not run brings nothing.
	c.report.installs = !c.opts.Converted && !c.opts.skipped(values.InstallPackagesStep, values.InstallStage)
	c.kernelComes = !c.opts.Converted && !c.opts.skipped(values.InstallKernelStep, values.InstallStage)
	if !c.report.installs && !c.kernelComes {
		if !c.opts.Converted {
			c.pass("packages: kairos-init is told not to install any, the system is judged as it is")
		}
		return
	}
	if !hasPackageList(c.sys) {
		c.fail("packages: kairos-init has no package list for the %s family", titleCase(c.family))
		return
	}
	installer := packageManager(c.fs)
	switch {
	case installer == "":
		c.failIdentity("packages: yip cannot tell the package manager from ID=%s, kairos-init stops with \"unknown package manager\"", c.id)
	case !c.hasBin(installer):
		c.fail("packages: %s is missing, kairos-init cannot install packages", installer)
	default:
		c.pass("packages: %s", installer)
	}
	switch c.sys.Distro {
	case values.RedHat, values.SLES, values.SLEMicroRancher:
		// CentOS Stream is a derivative of rhel with public repositories.
		if c.sys.Distro.String() == c.id {
			c.warn("packages: the kernel and other packages are in repositories that need a subscription")
		}
	}
}

func (c *checker) hasBin(name string) bool {
	_, ok := c.fs.findBin(name)
	return ok
}

// versions checks that the version of the system fits the package lists.
func (c *checker) versions() {
	// The version filters the package lists, and one that does not parse gets no
	// package from them. Hadron installs no package, so its version is not parsed.
	// The init stage parses it for Ubuntu and the Red Hat family as well.
	installs := c.report.installs || c.kernelComes
	versionOK := true
	if c.family != "" && c.sys.Family != values.HadronFamily {
		if _, err := c.sys.ParsedVersion(); err != nil {
			versionOK = false
			if installs || c.sys.Distro == values.Ubuntu || c.sys.Family == values.RedHatFamily {
				c.failIdentity("version: VERSION_ID=%s is not a version, kairos-init stops on it", orNone(c.version, "none"))
			}
		} else {
			c.pass("version: %s", c.version)
		}
	}
	if !versionOK || c.opts.Converted || c.family == "" {
		return
	}

	if c.kernelComes && c.sys.Arch != "" && c.kernelPackageNeeded() {
		c.kernelPackage()
	}
	if !c.report.installs {
		return
	}
	// Releases the package lists do not fit. A derivative counts its own
	// versions, so only the distribution itself can be judged by its number.
	major := c.majorVersion()
	switch {
	case c.sys.Distro == values.Debian && c.id == "debian" && major < 12:
		c.fail("packages: Debian %s does not have all of them, Debian 12 or newer does", c.version)
	case c.sys.Distro == values.OpenSUSELeap && c.id == "opensuse-leap" && major < 16:
		c.fail("packages: openSUSE Leap %s does not have all of them, Leap 16 or newer does", c.version)
	case c.sys.Distro == values.SLES && c.id == "sles" && major < 16:
		c.warn("packages: SLES %s does not have all of them in its public repositories, a registered system with its subscription repositories may", c.version)
	}
}

// kernelPackageNeeded is whether the kernel comes from a package list that can
// be judged: not for a board model, whose kernel is in lists this check does not
// read, and not when the kernel step will not run.
func (c *checker) kernelPackageNeeded() bool {
	if c.opts.Model != values.Generic.String() {
		return false
	}
	if c.opts.skipped(values.InstallKernelStep, values.InstallStage) {
		return false
	}
	return true
}

// newestKernel is the in-root directory of the newest kernel, as kairos-init
// chooses it.
func (c *checker) newestKernel() (string, bool) {
	dir, ok := c.fs.resolve("/lib/modules")
	if !ok {
		return "", false
	}
	name, err := kernel.GetLatestFromPath(dir, c.opts.Model, c.log)
	if err != nil || name == "" {
		return "", false
	}
	return "/lib/modules/" + name, true
}

var plainRelease = regexp.MustCompile(`^\d+\.\d+$`)

var hweKernel = regexp.MustCompile(`-hwe-(\d+\.\d+)$`)

// kernelPackage checks that kairos-init has a kernel package for the system.
func (c *checker) kernelPackage() {
	packages, _ := values.GetKernelPackagesFor(c.sys, c.opts.TrustedBoot, c.opts.Model, c.log)
	known := len(packages) > 0
	if !known {
		// No package, so the step installs nothing. A kernel the system has is
		// then the one kairos-init finds.
		if _, found := c.newestKernel(); found {
			c.pass("kernel package: kairos-init has none for %s %s, the kernel of the system is used", titleCase(c.distro), c.version)
			return
		}
	}
	// A kernel that names the release it comes from does not exist before it.
	// The Trusted Boot list names the 24.04 kernel for every Ubuntu release.
	if v, err := c.sys.ParsedVersion(); err == nil {
		for _, pkg := range packages {
			// A package name that holds the version of the system needs one of the
			// form 24.04 to name a package.
			if strings.Contains(pkg, "{{") && !plainRelease.MatchString(c.version) {
				known = false
			}
			m := hweKernel.FindStringSubmatch(pkg)
			if m == nil {
				continue
			}
			floor := values.System{Version: m[1]}
			if need, err := floor.ParsedVersion(); err == nil && v.LessThan(need) {
				known = false
			}
		}
	}
	if known {
		c.pass("kernel package: kairos-init has one for %s %s", titleCase(c.distro), c.version)
		return
	}
	c.failIdentity("kernel package: kairos-init has none for %s %s, VERSION_ID has to be a release it knows", titleCase(c.distro), c.version)
}

// initramfsStages checks that the stages that build the initramfs run on this
// system. They match on PRETTY_NAME.
func (c *checker) initramfsStages() {
	if c.opts.skipped(values.InitrdStep, values.InitStage) {
		c.pass("build steps: the initramfs is not built, so PRETTY_NAME is not looked at")
		return
	}
	dracut := regexp.MustCompile(stages.OSDracutFamilies).MatchString(c.pretty)
	initrd := regexp.MustCompile(stages.InitrdDracutOS).MatchString(c.pretty)
	alpine := regexp.MustCompile(values.AlpineRegex).MatchString(c.pretty)
	switch {
	case !dracut && initrd:
		c.failIdentity("build steps: PRETTY_NAME=%s matches the stage that builds the initramfs, but not the stages that put immucore into it", c.pretty)
	case !dracut && !alpine:
		c.failIdentity("build steps: no kairos-init stage matches PRETTY_NAME=%s, so it builds no initramfs and does not enable the network and ssh services", orNone(c.pretty, "none"))
	case c.opts.TrustedBoot:
		c.pass("build steps: PRETTY_NAME matches")
		c.initramfs = "uki"
	case alpine && !dracut && c.family != alpineFamily:
		c.fail("initramfs: PRETTY_NAME says Alpine, but the family is %s", orNone(c.family, "unknown"))
	case dracut && c.family == alpineFamily:
		c.fail("initramfs: PRETTY_NAME says dracut, but the family is alpine")
	case dracut:
		c.initramfs = "dracut"
		c.pass("initramfs: dracut (PRETTY_NAME matches)")
	default:
		c.initramfs = "mkinitfs"
		c.pass("initramfs: mkinitfs (PRETTY_NAME matches)")
	}
}

// --- platform ----------------------------------------------------------------

func (c *checker) platform() {
	c.section = "Platform"

	given := c.opts.Arch
	if given == "" {
		given = hostArch()
	}
	switch c.arch {
	case archAMD64, archARM64, archRISCV64:
		c.report.efiArch = c.arch
		c.pass("architecture: %s", given)
	default:
		c.fail("architecture: %s, Kairos is built for amd64, arm64 and riscv64", given)
	}

	// yip skips every stage that is meant for one init system when it finds both.
	if path, ok := c.fs.findBin("systemctl"); ok {
		c.systemd = path
	}
	if path, ok := c.fs.findBin("openrc"); ok {
		c.openrc = path
	}
	switch {
	case c.systemd != "" && c.openrc != "":
		c.fail("init system: systemd and OpenRC are both installed, and yip cannot choose")
	case c.systemd != "":
		c.pass("init system: systemd")
	case c.openrc != "" && c.opts.TrustedBoot:
		c.fail("init system: OpenRC, Trusted Boot needs systemd")
	case c.openrc != "":
		c.pass("init system: OpenRC")
	case c.family == alpineFamily && c.opts.TrustedBoot:
		c.fail("init system: OpenRC, Trusted Boot needs systemd")
	case c.family != alpineFamily && (c.fs.exists("/sbin/init") || c.fs.exists("/usr/sbin/init") || c.fs.exists("/etc/inittab")):
		// The Alpine base image has busybox as init and no OpenRC yet.
		c.fail("init system: neither systemd nor OpenRC, and something else is installed as init")
	default:
		c.missing(levelRequired, "systemd or OpenRC", "init system")
	}

	// The version can only be asked of a binary that runs here.
	switch {
	case c.systemd != "" && c.fs.host():
		c.systemdVersion()
	case c.systemd != "" && c.opts.TrustedBoot:
		c.warn("systemd version: not read from another root, Trusted Boot needs 252 or newer")
	}
}

func (c *checker) systemdVersion() {
	version, err := systemdVersion(c.systemd)
	switch {
	case err != nil:
		c.warn("systemd version: cannot be read")
	case version >= 252:
		c.pass("systemd version: %d", version)
	case c.opts.TrustedBoot:
		c.fail("systemd version: %d, Trusted Boot needs 252 or newer", version)
	default:
		c.warn("systemd version: %d, encryption bound to TPM measurements needs 252 or newer", version)
	}
}

// --- kernel ------------------------------------------------------------------

func (c *checker) kernelSection() {
	c.section = "Kernel"

	// The newest kernel, as kairos-init chooses it.
	if dir, found := c.newestKernel(); found {
		c.kernelDir = dir
	}
	if c.kernelDir == "" {
		if c.fs.exists("/lib/modules") {
			c.missing(levelRequired, "a kernel under /lib/modules", "kernel")
		} else {
			c.missing(levelRequired, "/lib/modules", "kernel")
		}
		return
	}
	c.pass("kernel: %s", path.Base(c.kernelDir))
	c.module(levelRequired, "read-only system, writable paths", "overlay")
	c.module(levelRequired, "install media and recovery image", "squashfs")
	c.module(levelRequired, "system images", "loop")
	c.module(levelRequired, "state, OEM and persistent partitions", "ext4")
	c.module(levelRequired, "EFI system partition", "vfat")
	c.module(levelRequired, "install media", "isofs")
	c.module(levelRequired, "LVM and encryption", "dm_mod")
	c.module(c.encryption, "encrypted partitions", "dm_crypt")
	c.module(levelOptional, "boot from write-protected media", "dm_snapshot")
	c.module(levelOptional, "TPM 2.0", "tpm_crb")
	c.module(levelOptional, "TPM", "tpm_tis")
	c.module(levelOptional, "multipath storage", "dm_multipath")
	if c.opts.Converted {
		c.convertedFile("/boot/vmlinuz")
		if !c.opts.TrustedBoot {
			c.convertedFile("/boot/initrd")
		}
	}
}

func (c *checker) convertedFile(path string) {
	if c.fs.exists(path) {
		c.pass("%s", path)
	} else {
		c.fail("%s is missing", path)
	}
}

// module checks that the driver is a module or is built into the kernel. File
// names use dashes and underscores alike.
func (c *checker) module(level, why, name string) {
	// The kernel of a board is built for that board and leaves out drivers the
	// generic images need, so its modules are not required.
	if c.opts.Model != values.Generic.String() && level == levelRequired {
		level = levelOptional
	}
	pattern := regexp.MustCompile("/" + strings.ReplaceAll(regexp.QuoteMeta(name), "_", "[-_]") + `\.ko`)
	for _, file := range []string{"modules.builtin", "modules.dep"} {
		if data, err := c.fs.readFile(c.kernelDir + "/" + file); err == nil && pattern.Match(data) {
			c.pass("module %s (%s)", name, why)
			return
		}
	}
	c.missing(level, "module "+name, why)
}

// --- boot --------------------------------------------------------------------

// grubFile is whether the file is in the GRUB directory for that platform,
// wherever the distribution keeps it.
func (c *checker) grubFile(name, platform string) bool {
	for _, dir := range []string{"/usr/lib/grub", "/usr/share/grub2", "/usr/lib/grub2", "/usr/share/grub"} {
		if c.fs.exists(dir + "/" + platform + "/" + name) {
			return true
		}
	}
	return false
}

func (c *checker) boot() {
	c.section = "Boot"

	if c.opts.TrustedBoot {
		c.pass("Trusted Boot: AuroraBoot builds the UKI and brings systemd-boot")
	} else if c.report.efiArch != "" {
		c.component(levelRequired, "boot entries", "grub2-editenv", "grub-editenv")
		if c.report.efiArch == archAMD64 {
			c.component(levelOptional, "install on a BIOS machine", "grub2-install", "grub-install")
			if c.grubFile("modinfo.sh", "i386-pc") {
				c.pass("GRUB for BIOS")
			} else {
				c.missing(levelOptional, "GRUB i386-pc", "BIOS boot")
			}
		}
		platform := map[string]string{archAMD64: "x86_64-efi", archARM64: "arm64-efi", archRISCV64: "riscv64-efi"}[c.report.efiArch]
		var absent []string
		for _, module := range agentconstants.GetGrubModules() {
			if !c.grubFile(module, platform) {
				absent = append(absent, module)
			}
		}
		if len(absent) == 0 {
			c.pass("GRUB modules for UEFI")
		} else {
			c.missing(levelOptional, "GRUB modules: "+strings.Join(absent, " "), "UEFI boot")
		}
		// riscv64 firmware loads GRUB without a shim.
		if shim := utils.GetEfiShimFiles(c.report.efiArch); len(shim) > 0 {
			c.uefi("shim for UEFI", shim)
		}
		c.uefi("GRUB for UEFI", utils.GetEfiGrubFiles(c.report.efiArch))
	}

	// The commands the immucore dracut module puts in the initramfs.
	switch c.initramfs {
	case "dracut":
		c.component(levelRequired, "builds the initramfs", "dracut")
		c.component(levelRequired, "dracut modules, systemd units", "bash")
		c.component(levelRequired, "initramfs", "lvm")
		for _, command := range dracutModuleCommands() {
			if !isTool(command) {
				c.component(levelRequired, "initramfs", command)
			}
		}
		c.component(levelOptional, "check of the initramfs content", "lsinitrd")
	case "mkinitfs":
		c.component(levelRequired, "builds the initramfs", "mkinitfs")
		c.component(levelRequired, "machine id", "dbus-uuidgen")
	}
}

// uefi checks a file the agent needs to make the disk bootable on UEFI.
// kairos-init does not look at it, so a missing one is never a fail.
func (c *checker) uefi(what string, paths []string) {
	if path, ok := c.fs.firstOf(paths); ok {
		c.pass("%s: %s", what, path)
		return
	}
	if c.report.installs {
		// The packages of these put the files in a directory the agent knows.
		switch {
		case c.sys.Family == values.DebianFamily, c.sys.Family == values.SUSEFamily,
			c.sys.Family == values.RedHatFamily && agentKnowsRedHat(c.id):
			c.pass("%s: not installed yet, left to the packages for the %s family", what, c.family)
			return
		}
		c.report.uefiMissing = true
		c.warn("%s: the agent looks in no directory of ID=%s, so a UEFI install fails", what, c.id)
		return
	}
	c.report.uefiMissing = true
	c.warn("%s: not in any path the agent looks in, so a UEFI install fails", what)
}

// agentKnowsRedHat is whether the agent looks in the EFI directory of the
// distribution, in sdk/utils.
func agentKnowsRedHat(id string) bool {
	for _, known := range []string{"fedora", "rocky", "rhel", "ol", "almalinux"} {
		if id == known {
			return true
		}
	}
	return false
}

var (
	instCheck   = regexp.MustCompile(`(?m)^\s+inst_check_multiple ([^|;&\\\n]+)`)
	commandName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]*$`)
)

// dracutModuleCommands are the commands the immucore dracut module insists on,
// from the module itself.
func dracutModuleCommands() []string {
	var out []string
	for _, m := range instCheck.FindAllStringSubmatch(bundled.ImmucoreModuleSetupDracut, -1) {
		for _, command := range strings.Fields(m[1]) {
			if command != "immucore" && commandName.MatchString(command) {
				out = append(out, command)
			}
		}
	}
	return out
}

// --- tools -------------------------------------------------------------------

type tool struct{ level, why, name string }

// tools are the commands the agent and immucore run.
var tools = []tool{
	{levelRequired, "mounts", "mount"},
	{levelRequired, "mounts", "umount"},
	{levelRequired, "finds partitions by label", "blkid"},
	{levelRequired, "finds partitions", "lsblk"},
	{levelRequired, "finds mounts", "findmnt"},
	{levelRequired, "device events", "udevadm"},
	{levelRequired, "kernel modules", "depmod"},
	{levelRequired, "kernel modules", "modprobe"},
	{levelRequired, "copies the system", "rsync"},
	{levelRequired, "unpacks archives", "tar"},
	{levelRequired, "encryption", "cryptsetup"},
	{levelRequired, "filesystem check", "e2fsck"},
	{levelRequired, "filesystem labels", "tune2fs"},
	{levelRequired, "system images", "mkfs.ext2"},
	{levelRequired, "initramfs", "mkfs.ext3"},
	{levelRequired, "partitions", "mkfs.ext4"},
	{levelRequired, "EFI system partition", "mkfs.vfat"},
	{levelOptional, "squashfs recovery image", "mksquashfs"},
	{levelOptional, "state of encrypted partitions", "dmsetup"},
}

func isTool(name string) bool {
	for _, t := range tools {
		if t.name == name {
			return true
		}
	}
	return false
}

func (c *checker) tools() {
	c.section = "Disk and filesystem tools"
	for _, t := range tools {
		c.component(t.level, t.why, t.name)
	}

	c.section = "Userspace"
	// kairos-init validate asks for these on the packages of the distribution.
	for _, name := range validation.DistroBinaries(c.sys.Family) {
		why := map[string]string{"sudo": "users", "less": "console", "mount.nfs": "NFS"}[name]
		c.component(levelRequired, why, name)
	}
	c.component(levelOptional, "remote access", "sshd")
	c.component(levelOptional, "network boot", "curl")
	if c.openrc != "" || c.family == alpineFamily {
		c.component(levelOptional, "network", "connmand")
		return
	}
	if c.fs.exists("/usr/lib/systemd/systemd-networkd") || c.fs.exists("/lib/systemd/systemd-networkd") || c.hasBin("NetworkManager") {
		c.pass("network: systemd-networkd or NetworkManager")
	} else {
		c.missing(levelOptional, "systemd-networkd or NetworkManager", "network")
	}
	c.component(c.encryption, "encryption bound to the TPM", "systemd-cryptenroll")
	if c.fs.exists("/usr/lib/systemd/systemd-cryptsetup") {
		c.pass("systemd-cryptsetup (encryption bound to the TPM)")
	} else {
		c.missing(c.encryption, "systemd-cryptsetup", "encryption bound to the TPM")
	}
	c.component(levelOptional, "system extensions", "systemd-sysext")
}

// --- helpers -----------------------------------------------------------------

// normalizeArch gives the Go name of an architecture, from the Go or the
// `uname -m` spelling, and the empty string for one Kairos is not built for.
func normalizeArch(arch string) string {
	if arch == "" {
		arch = hostArch()
	}
	switch arch {
	case "x86_64", archAMD64:
		return archAMD64
	case "aarch64", archARM64:
		return archARM64
	case archRISCV64:
		return archRISCV64
	}
	return ""
}

func orNone(s, none string) string {
	if s == "" {
		return none
	}
	return s
}

func titleCase(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func hostArch() string { return runtime.GOARCH }
