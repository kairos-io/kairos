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

type guardFile struct {
	Path string `yaml:"path"`
}

type guardStage struct {
	Name               string      `yaml:"name"`
	If                 string      `yaml:"if"`
	OnlyServiceManager string      `yaml:"only_service_manager"`
	Commands           []string    `yaml:"commands"`
	Files              []guardFile `yaml:"files"`
}

// writesFile reports whether the stage lays down the given path. Unmarshalling
// resolves the file list's YAML merge keys, so an aliased entry counts.
func (s guardStage) writesFile(path string) bool {
	for _, f := range s.Files {
		if f.Path == path {
			return true
		}
	}
	return false
}

type guardConfig struct {
	Stages map[string][]guardStage `yaml:"stages"`
}

func readStages(name string) []guardStage {
	return readStage(name, "initramfs")
}

func readStage(name, stage string) []guardStage {
	content, err := os.ReadFile(filepath.Join("..", "bundled", "cloudconfigs", name))
	Expect(err).NotTo(HaveOccurred(), "read cloudconfig %s", name)

	var cfg guardConfig
	Expect(yaml.Unmarshal(content, &cfg)).To(Succeed(), "parse cloudconfig %s", name)
	Expect(cfg.Stages[stage]).NotTo(BeEmpty(), "%s has no %s stage", name, stage)
	return cfg.Stages[stage]
}

// stageEnabling returns the systemd initramfs stage that enables the named
// service, so the specs below identify a stage by what it does rather than by
// its position in the file.
func stageEnabling(stages []guardStage, service string) guardStage {
	return stageRunning(stages, "systemctl enable "+service)
}

// stageRunning returns the systemd stage that runs command verbatim.
func stageRunning(stages []guardStage, command string) guardStage {
	return stageRunningFor(stages, "systemd", command)
}

// stageRunningFor returns the stage for the given service manager that runs
// command verbatim.
func stageRunningFor(stages []guardStage, serviceManager, command string) guardStage {
	for _, s := range stages {
		if s.OnlyServiceManager != serviceManager {
			continue
		}
		for _, c := range s.Commands {
			if c == command {
				return s
			}
		}
	}
	Fail("no " + serviceManager + " stage runs " + command)
	return guardStage{}
}

// evalGuard runs a stage's `if` expression the way yip does, with sh -c, against
// a synthetic /proc/cmdline. liveMode says whether /run/cos/live_mode exists.
// The UKI sentinel is absent, so these specs describe the GRUB live ISO.
// It reports whether the stage would run.
func evalGuard(expr, cmdline string, liveMode bool) bool {
	return evalGuardOn(expr, cmdline, liveMode, false)
}

// evalGuardUki is evalGuard for a Trusted Boot medium: immucore has written
// /run/cos/uki_install_mode and there is no live_mode sentinel.
func evalGuardUki(expr, cmdline string) bool {
	return evalGuardOn(expr, cmdline, false, true)
}

// evalGuardOn runs the guard with each of the two sentinels present or absent
// as asked, so a spec can describe a GRUB boot, a UKI boot, or an installed
// system (neither sentinel).
func evalGuardOn(expr, cmdline string, liveMode, ukiInstallMode bool) bool {
	root := GinkgoT().TempDir()

	cmdlinePath := filepath.Join(root, "cmdline")
	Expect(os.WriteFile(cmdlinePath, []byte(cmdline+"\n"), 0644)).To(Succeed())

	sentinel := func(name string, present bool) string {
		path := filepath.Join(root, name)
		if present {
			Expect(os.WriteFile(path, nil, 0644)).To(Succeed())
		}
		return path
	}

	expr = strings.ReplaceAll(expr, "/proc/cmdline", cmdlinePath)
	expr = strings.ReplaceAll(expr, "/run/cos/live_mode", sentinel("live_mode", liveMode))
	expr = strings.ReplaceAll(expr, "/run/cos/uki_install_mode", sentinel("uki_install_mode", ukiInstallMode))

	err := exec.Command("sh", "-c", expr).Run()
	if err == nil {
		return true
	}
	_, isExit := err.(*exec.ExitError)
	Expect(isExit).To(BeTrue(), "guard failed to run: %v", err)
	return false
}

var _ = Describe("Bundled cloudconfigs install-mode guards", func() {
	// install-mode is a prefix of install-mode-interactive. A guard matching it
	// as a plain substring fires on an interactive boot too, so both the plain
	// and the interactive installer get enabled and only the file order decides
	// which one ends up owning tty1.
	Describe("52_installer.yaml", func() {
		var plain, interactive string

		BeforeEach(func() {
			stages := readStages("52_installer.yaml")
			plain = stageEnabling(stages, "kairos-installer").If
			interactive = stageEnabling(stages, "kairos-interactive").If
		})

		It("starts the plain installer on install-mode", func() {
			Expect(evalGuard(plain, "BOOT_IMAGE=/boot/kernel install-mode", true)).To(BeTrue())
			Expect(evalGuard(interactive, "BOOT_IMAGE=/boot/kernel install-mode", true)).To(BeFalse())
		})

		It("starts only the interactive installer on install-mode-interactive", func() {
			const cmdline = "BOOT_IMAGE=/boot/kernel install-mode-interactive rd.debug"
			Expect(evalGuard(interactive, cmdline, true)).To(BeTrue())
			Expect(evalGuard(plain, cmdline, true)).To(BeFalse())
		})

		It("starts only the interactive installer on the legacy interactive-install", func() {
			const cmdline = "BOOT_IMAGE=/boot/kernel interactive-install"
			Expect(evalGuard(interactive, cmdline, true)).To(BeTrue())
			Expect(evalGuard(plain, cmdline, true)).To(BeFalse())
		})

		It("still honours the legacy nodepair.enable keyword", func() {
			Expect(evalGuard(plain, "BOOT_IMAGE=/boot/kernel nodepair.enable", true)).To(BeTrue())
		})

		// On Trusted Boot every UKI entry AuroraBoot builds carries
		// install-mode, because they all extend the same base cmdline, and the
		// installer copies norole.efi verbatim into each installed role. So
		// the keyword is true on a live UKI boot and on an installed one
		// alike, and only /run/cos/uki_install_mode separates them. The plain
		// installer has to key off the sentinel there, or it depends on a
		// keyword that says nothing.
		It("starts the plain installer on a UKI medium whatever the cmdline says", func() {
			for _, cmdline := range []string{
				"BOOT_IMAGE=/boot/kernel install-mode",
				"BOOT_IMAGE=/boot/kernel",
			} {
				Expect(evalGuardUki(plain, cmdline)).To(BeTrue(), "cmdline %q", cmdline)
			}
		})

		// The point of kairos-io/kairos#5000: an installed Trusted Boot system
		// boots with install-mode on its cmdline for the life of the machine.
		// Neither sentinel exists there, and no installer stage may fire.
		It("starts nothing on an installed system that inherited install-mode", func() {
			const cmdline = "BOOT_IMAGE=/boot/kernel install-mode rd.immucore.uki"
			Expect(evalGuardOn(plain, cmdline, false, false)).To(BeFalse())
			Expect(evalGuardOn(interactive, cmdline, false, false)).To(BeFalse())
		})

		// The openrc stage starts the same installer from /etc/inittab. It is
		// a separate `if` in the same file, so it can be narrowed or widened
		// on its own; pin the two to the same answer rather than the same text.
		It("guards the openrc installer exactly as the systemd one", func() {
			openrc := stageRunningFor(readStages("52_installer.yaml"), "openrc",
				`echo "tty1::respawn:/usr/bin/kairos-agent install tty1" >> /etc/inittab`).If

			for _, tc := range []struct {
				cmdline        string
				live, ukiMedia bool
			}{
				{"BOOT_IMAGE=/boot/kernel install-mode", true, false},
				{"BOOT_IMAGE=/boot/kernel", true, false},
				{"BOOT_IMAGE=/boot/kernel nodepair.enable", true, false},
				{"BOOT_IMAGE=/boot/kernel install-mode", false, true},
				{"BOOT_IMAGE=/boot/kernel", false, true},
				{"BOOT_IMAGE=/boot/kernel install-mode", false, false},
			} {
				Expect(evalGuardOn(openrc, tc.cmdline, tc.live, tc.ukiMedia)).
					To(Equal(evalGuardOn(plain, tc.cmdline, tc.live, tc.ukiMedia)),
						"cmdline %q live=%v uki=%v", tc.cmdline, tc.live, tc.ukiMedia)
			}
		})

		// install-mode-interactive is a per-entry extra rather than part of the
		// base cmdline, so on UKI it still discriminates and the interactive
		// stage keeps requiring it.
		It("keeps the interactive stage keyed on its own keyword on a UKI medium", func() {
			Expect(evalGuardUki(interactive, "BOOT_IMAGE=/boot/kernel install-mode")).To(BeFalse())
			Expect(evalGuardUki(interactive, "BOOT_IMAGE=/boot/kernel install-mode install-mode-interactive")).To(BeTrue())
		})

		It("starts nothing on a plain boot, or outside live mode", func() {
			Expect(evalGuard(plain, "BOOT_IMAGE=/boot/kernel root=LABEL=COS_STATE", true)).To(BeFalse())
			Expect(evalGuard(interactive, "BOOT_IMAGE=/boot/kernel root=LABEL=COS_STATE", true)).To(BeFalse())
			Expect(evalGuard(plain, "BOOT_IMAGE=/boot/kernel install-mode", false)).To(BeFalse())
		})

		// Narrowing the install-mode guard took the WebUI's `systemctl enable`
		// off the interactive path, since that enable sits in the install-mode
		// stage. The boot stage covers the rest: any live boot, install-mode or
		// not, gets the WebUI as its own service and gets it enabled rather
		// than only started, so it comes back after a restart.
		It("enables the webui on a live boot with no install keyword at all", func() {
			boot := stageRunning(readStage("52_installer.yaml", "boot"), "systemctl enable --now kairos-webui")

			for _, cmdline := range []string{
				"BOOT_IMAGE=/boot/kernel install-mode",
				"BOOT_IMAGE=/boot/kernel",
			} {
				Expect(evalGuard(boot.If, cmdline, true)).To(BeTrue(), "cmdline %q", cmdline)
			}
			Expect(evalGuard(boot.If, "BOOT_IMAGE=/boot/kernel install-mode", false)).To(BeFalse())
		})

		// On an interactive boot the installer binary serves the WebUI itself,
		// in the same process as its terminal UI. Starting kairos-webui too
		// would put a second process on the same listen address, so the boot
		// stage has to stay off that path. Every cmdline the interactive stage
		// claims has to be one the boot stage refuses, or both fire.
		It("leaves the webui service alone on an interactive boot", func() {
			boot := stageRunning(readStage("52_installer.yaml", "boot"), "systemctl enable --now kairos-webui")
			openrc := stageRunningFor(readStage("52_installer.yaml", "boot"), "openrc", "rc-service kairos-webui start")

			for _, cmdline := range []string{
				"BOOT_IMAGE=/boot/kernel install-mode-interactive",
				"BOOT_IMAGE=/boot/kernel interactive-install",
				"BOOT_IMAGE=/boot/kernel interactive-install install-mode-interactive",
			} {
				Expect(evalGuard(interactive, cmdline, true)).To(BeTrue(),
					"interactive stage should claim %q", cmdline)
				Expect(evalGuard(boot.If, cmdline, true)).To(BeFalse(),
					"boot stage should refuse %q", cmdline)
				Expect(evalGuard(openrc.If, cmdline, true)).To(BeFalse(),
					"openrc boot stage should refuse %q", cmdline)
			}
		})

		// Quitting the terminal UI ends the in-process WebUI with it, and
		// nothing re-execs the installer for that boot. openrc can bring it
		// back because /etc/init.d/kairos-webui is written unconditionally;
		// systemd could not, because the only stages writing the unit are the
		// install-mode one (whose guard excludes install-mode-interactive) and
		// the boot one (which refuses it outright), so `systemctl start
		// kairos-webui` failed with "Unit kairos-webui.service not found".
		It("installs the webui unit on an interactive systemd boot without enabling it", func() {
			stages := readStages("52_installer.yaml")
			stage := stageEnabling(stages, "kairos-interactive")

			Expect(stage.writesFile("/etc/systemd/system/kairos-webui.service")).To(BeTrue(),
				"interactive stage must write the unit so the WebUI can be restarted by hand")

			// Enabling or starting it here would put a second process on the
			// listen address the installer already holds.
			for _, c := range stage.Commands {
				Expect(c).NotTo(ContainSubstring("kairos-webui"),
					"interactive stage must not touch the kairos-webui service")
			}
		})

		// install-mode and install-mode-interactive are not mutually
		// exclusive on the cmdline, and the space-delimited match means a
		// cmdline carrying both fires the plain stage as well as the
		// interactive one. That is deliberate for tty1: the interactive stage
		// runs later and disables kairos-installer. It is not deliberate for
		// the WebUI, so no stage that fires on this cmdline may enable or
		// start kairos-webui, or the service and the installer's in-process
		// WebUI both go for the same listen address.
		It("leaves the webui service alone when the cmdline carries both keywords", func() {
			const cmdline = "BOOT_IMAGE=/boot/kernel install-mode install-mode-interactive"

			plainStage := stageEnabling(readStages("52_installer.yaml"), "kairos-installer")
			boot := stageRunning(readStage("52_installer.yaml", "boot"), "systemctl enable --now kairos-webui")
			openrc := stageRunningFor(readStage("52_installer.yaml", "boot"), "openrc", "rc-service kairos-webui start")

			Expect(evalGuard(plain, cmdline, true)).To(BeTrue(),
				"install-mode stage should still claim %q", cmdline)
			Expect(evalGuard(interactive, cmdline, true)).To(BeTrue(),
				"interactive stage should claim %q", cmdline)

			Expect(evalGuard(boot.If, cmdline, true)).To(BeFalse(),
				"boot stage should refuse %q", cmdline)
			Expect(evalGuard(openrc.If, cmdline, true)).To(BeFalse(),
				"openrc boot stage should refuse %q", cmdline)

			for _, c := range plainStage.Commands {
				Expect(c).NotTo(ContainSubstring("kairos-webui"),
					"install-mode stage must not touch the kairos-webui service")
			}
		})
	})

	// The autologin guard used to be two OR'd `grep -qv`, which is true for
	// every single-line cmdline. Autologin is unconditional on a live boot now,
	// and these specs pin that so it cannot silently become conditional again.
	Describe("25_autologin.yaml", func() {
		var stages []guardStage

		BeforeEach(func() {
			stages = readStages("25_autologin.yaml")
		})

		It("autologins on every live boot, interactive or not", func() {
			for _, s := range stages {
				for _, cmdline := range []string{
					"BOOT_IMAGE=/boot/kernel install-mode",
					"BOOT_IMAGE=/boot/kernel install-mode-interactive",
					"BOOT_IMAGE=/boot/kernel interactive-install",
				} {
					Expect(evalGuard(s.If, cmdline, true)).To(BeTrue(),
						"stage %q should autologin on %q", s.Name, cmdline)
				}
			}
		})

		// The one cmdline the old guard did refuse: with both keywords present
		// neither `grep -qv` succeeds, so autologin was skipped. Nothing about
		// carrying both keywords should turn autologin off.
		It("autologins when the cmdline carries both interactive keywords", func() {
			const cmdline = "BOOT_IMAGE=/boot/kernel interactive-install install-mode-interactive"
			for _, s := range stages {
				Expect(evalGuard(s.If, cmdline, true)).To(BeTrue(), "stage %q", s.Name)
			}
		})

		It("does not autologin outside live mode", func() {
			for _, s := range stages {
				Expect(evalGuard(s.If, "BOOT_IMAGE=/boot/kernel root=LABEL=COS_STATE", false)).To(BeFalse(),
					"stage %q should not autologin off the live medium", s.Name)
			}
		})
	})
})
