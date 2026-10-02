package validation_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"
)

// inittabStep is the subset of a bundled cloud-config step this spec needs: the
// guard, the service-manager filter and the commands.
type inittabStep struct {
	Name               string   `yaml:"name"`
	If                 string   `yaml:"if"`
	OnlyServiceManager string   `yaml:"only_service_manager"`
	Commands           []string `yaml:"commands"`
}

type inittabConfig struct {
	Stages map[string][]inittabStep `yaml:"stages"`
}

// alpineInittab is the /etc/inittab of
// quay.io/kairos/alpine:3.18-core-amd64-generic-v2.4.3, read off the registry.
// The serial line ships commented out, which is why something has to add one.
const alpineInittab = `# /etc/inittab

::sysinit:/sbin/openrc sysinit
::sysinit:/sbin/openrc boot
::wait:/sbin/openrc default

# Set up a couple of getty's
tty1::respawn:/sbin/getty 38400 tty1
tty2::respawn:/sbin/getty 38400 tty2

# Put a getty on the serial port
#ttyS0::respawn:/sbin/getty -L ttyS0 115200 vt100

# Stuff to do for the 3-finger salute
::ctrlaltdel:/sbin/reboot

# Stuff to do before rebooting
::shutdown:/sbin/openrc shutdown
`

// inittabStages is the order agent/pkg/utils/runstage.go applies a stage in:
// every file's <stage>.before, then every file's <stage>, then every file's
// <stage>.after. So a step in initramfs.after runs after every initramfs step
// of every other file, not just of its own.
var inittabStages = []string{"initramfs.before", "initramfs", "initramfs.after"}

// replayOpenRCInittab runs, in the order a real OpenRC boot runs them, every
// bundled cloud-config step that touches /etc/inittab, and returns the
// resulting file. liveMode and cmdline describe the boot being simulated.
//
// Only the steps whose commands mention /etc/inittab are replayed. The rest of
// the same files create users and mount things, which is not what is under
// test here and would need root.
func replayOpenRCInittab(liveMode bool, cmdline string) string {
	root := GinkgoT().TempDir()
	inittab := filepath.Join(root, "inittab")
	cmdlinePath := filepath.Join(root, "cmdline")
	runCos := filepath.Join(root, "run-cos")

	Expect(os.WriteFile(inittab, []byte(alpineInittab), 0o600)).To(Succeed())
	Expect(os.WriteFile(cmdlinePath, []byte(cmdline+"\n"), 0o600)).To(Succeed())
	Expect(os.MkdirAll(runCos, 0o700)).To(Succeed())
	if liveMode {
		Expect(os.WriteFile(filepath.Join(runCos, "live_mode"), nil, 0o600)).To(Succeed())
	}

	// The steps name absolute paths that cannot be written here, so point each
	// one at the fake root. Same substitution approach as
	// cloudconfigs_vm_test.go.
	localize := func(s string) string {
		s = strings.ReplaceAll(s, "/etc/inittab", inittab)
		s = strings.ReplaceAll(s, "/proc/cmdline", cmdlinePath)
		s = strings.ReplaceAll(s, "/run/cos/", runCos+"/")
		return s
	}

	dir := filepath.Join("..", "bundled", "cloudconfigs")
	entries, err := os.ReadDir(dir)
	Expect(err).NotTo(HaveOccurred(), "read bundled cloudconfigs directory")

	replayed := 0
	for _, stage := range inittabStages {
		// os.ReadDir sorts by filename, which is the order yip's dirOps walks
		// a cloud-config directory in.
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}

			content, err := os.ReadFile(filepath.Join(dir, entry.Name()))
			Expect(err).NotTo(HaveOccurred(), "read cloudconfig %s", entry.Name())

			var config inittabConfig
			Expect(yaml.Unmarshal(content, &config)).To(Succeed(), "parse %s", entry.Name())

			for _, step := range config.Stages[stage] {
				if step.OnlyServiceManager != "" && step.OnlyServiceManager != "openrc" {
					continue
				}
				if !strings.Contains(strings.Join(step.Commands, "\n"), "/etc/inittab") {
					continue
				}

				if step.If != "" {
					if err := exec.Command("sh", "-c", localize(step.If)).Run(); err != nil {
						continue
					}
				}

				for _, command := range step.Commands {
					out, err := exec.Command("sh", "-c", localize(command)).CombinedOutput()
					Expect(err).NotTo(HaveOccurred(),
						"%s %s: %q: %s", entry.Name(), stage, command, out)
				}
				replayed++
			}
		}
	}

	Expect(replayed).To(BeNumerically(">", 0), "no inittab step was replayed, the parser stopped matching")

	result, err := os.ReadFile(inittab)
	Expect(err).NotTo(HaveOccurred())
	return string(result)
}

// inittabEntry returns the line of an inittab that configures the given tty.
func inittabEntry(inittab, tty string) string {
	for _, line := range strings.Split(inittab, "\n") {
		if strings.HasPrefix(line, tty+":") {
			return line
		}
	}
	return ""
}

var _ = Describe("Bundled OpenRC inittab steps", func() {
	// 25_autologin.yaml installs a root autologin on tty1 and ttyS0 for a live
	// boot, and 10_accounting.yaml's initramfs.after step used to sed the ttyS0
	// one straight back out and replace it with a plain getty, because
	// runstage.go runs every initramfs step before any initramfs.after step.
	// Serial was then the one console a live medium could not be reached on,
	// which is the whole point of the entry. See kairos-io/kairos#4741.
	It("keeps the live-media autologin on both consoles", func() {
		inittab := replayOpenRCInittab(true, "root=LABEL=COS_LIVE console=ttyS0")

		Expect(inittabEntry(inittab, "ttyS0")).To(
			HavePrefix("ttyS0::respawn:/sbin/agetty --autologin root"),
			"the serial console lost its autologin")
		Expect(inittabEntry(inittab, "tty1")).To(
			HavePrefix("tty1::respawn:/sbin/agetty --autologin root"),
			"tty1 lost its autologin")
	})

	// The complement: on an installed system nothing sets up a serial console
	// but 10_accounting.yaml, so its guard must not lock it out of a normal
	// boot.
	It("gives an installed system a plain serial getty", func() {
		inittab := replayOpenRCInittab(false, "root=LABEL=COS_STATE")

		Expect(inittabEntry(inittab, "ttyS0")).To(
			Equal("ttyS0::respawn:/sbin/getty -L ttyS0 115200 vt100"))
		Expect(inittab).NotTo(ContainSubstring("--autologin"),
			"an installed system must not autologin root")
	})

	// 52_installer.yaml puts the installer on tty1 for an install-mode live
	// boot, and it does that from the initramfs stage, after 25_autologin.yaml
	// in the same stage. So the fix for #4741 has to stay in 10_accounting.yaml:
	// moving 25_autologin.yaml's step into initramfs.after would fix serial by
	// taking tty1 away from the installer.
	It("leaves tty1 to the installer on an install-mode boot, and keeps serial autologin", func() {
		inittab := replayOpenRCInittab(true, "root=LABEL=COS_LIVE install-mode console=ttyS0")

		Expect(inittabEntry(inittab, "tty1")).To(
			HavePrefix("tty1::respawn:/usr/bin/kairos-agent install"),
			"the installer must own tty1 on an install-mode boot")
		Expect(inittabEntry(inittab, "ttyS0")).To(
			HavePrefix("ttyS0::respawn:/sbin/agetty --autologin root"),
			"the serial console lost its autologin")
	})
})
