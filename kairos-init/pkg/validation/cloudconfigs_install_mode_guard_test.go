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

// evalGuardUki is evalGuard for a Trusted Boot install medium. immucore
// writes BOTH sentinels on that boot: uki_install_mode because the UKI did not
// boot from an installed EFI entry, and live_mode because the same condition
// makes getUKIBootState report LiveCD (see sdk/state/state.go and
// immucore/pkg/state/steps_shared.go). A guard that wants to exclude Trusted
// Boot therefore has to name uki_install_mode; requiring live_mode does not
// narrow anything.
func evalGuardUki(expr, cmdline string) bool {
	return evalGuardOn(expr, cmdline, true, true)
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

		// On Trusted Boot every UKI entry AuroraBoot builds carries
		// install-mode, because they all extend the same base cmdline, and the
		// installer copies norole.efi verbatim into each installed role. The
		// cmdline is signed, so no entry can carry a keyword of its own and
		// there is nothing there to read. A UKI install medium therefore gets
		// the interactive installer, which is the dispatcher: it runs
		// AutoInstall first, so install.auto: true still installs unattended.
		It("starts the interactive installer on a UKI medium whatever the cmdline says", func() {
			for _, cmdline := range []string{
				"BOOT_IMAGE=/boot/kernel install-mode",
				"BOOT_IMAGE=/boot/kernel install-mode-interactive",
				"BOOT_IMAGE=/boot/kernel",
			} {
				Expect(evalGuardUki(interactive, cmdline)).To(BeTrue(), "cmdline %q", cmdline)
				Expect(evalGuardUki(plain, cmdline)).To(BeFalse(), "cmdline %q", cmdline)
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
				{"BOOT_IMAGE=/boot/kernel install-mode-interactive", true, false},
				{"BOOT_IMAGE=/boot/kernel install-mode", true, true},
				{"BOOT_IMAGE=/boot/kernel", true, true},
				{"BOOT_IMAGE=/boot/kernel install-mode", false, false},
			} {
				Expect(evalGuardOn(openrc, tc.cmdline, tc.live, tc.ukiMedia)).
					To(Equal(evalGuardOn(plain, tc.cmdline, tc.live, tc.ukiMedia)),
						"cmdline %q live=%v uki=%v", tc.cmdline, tc.live, tc.ukiMedia)
			}
		})

		// The GRUB live ISO keeps deciding by keyword: its cmdline is per menu
		// entry and unsigned, so the entry can still say which installer it
		// wants, and a live boot that asks for neither gets neither.
		It("still decides by keyword on the GRUB live ISO", func() {
			Expect(evalGuard(plain, "BOOT_IMAGE=/boot/kernel install-mode", true)).To(BeTrue())
			Expect(evalGuard(interactive, "BOOT_IMAGE=/boot/kernel install-mode", true)).To(BeFalse())
			Expect(evalGuard(interactive, "BOOT_IMAGE=/boot/kernel install-mode-interactive", true)).To(BeTrue())
			Expect(evalGuard(plain, "BOOT_IMAGE=/boot/kernel", true)).To(BeFalse())
			Expect(evalGuard(interactive, "BOOT_IMAGE=/boot/kernel", true)).To(BeFalse())
		})

		// The openrc interactive stage is a separate `if`, same as the plain
		// pair below. Pin it to the same answers rather than the same text.
		It("guards the openrc interactive installer exactly as the systemd one", func() {
			openrc := stageRunningFor(readStages("52_installer.yaml"), "openrc",
				`echo "tty1::respawn:/usr/bin/kairos-agent interactive-install --shell tty1" >> /etc/inittab`).If

			for _, tc := range []struct {
				cmdline        string
				live, ukiMedia bool
			}{
				{"BOOT_IMAGE=/boot/kernel install-mode-interactive", true, false},
				{"BOOT_IMAGE=/boot/kernel interactive-install", true, false},
				{"BOOT_IMAGE=/boot/kernel install-mode", true, false},
				{"BOOT_IMAGE=/boot/kernel", true, false},
				{"BOOT_IMAGE=/boot/kernel install-mode", true, true},
				{"BOOT_IMAGE=/boot/kernel", true, true},
				{"BOOT_IMAGE=/boot/kernel install-mode-interactive", false, false},
			} {
				Expect(evalGuardOn(openrc, tc.cmdline, tc.live, tc.ukiMedia)).
					To(Equal(evalGuardOn(interactive, tc.cmdline, tc.live, tc.ukiMedia)),
						"cmdline %q live=%v uki=%v", tc.cmdline, tc.live, tc.ukiMedia)
			}
		})

		It("starts nothing on a plain boot, or outside live mode", func() {
			Expect(evalGuard(plain, "BOOT_IMAGE=/boot/kernel root=LABEL=COS_STATE", true)).To(BeFalse())
			Expect(evalGuard(interactive, "BOOT_IMAGE=/boot/kernel root=LABEL=COS_STATE", true)).To(BeFalse())
			Expect(evalGuard(plain, "BOOT_IMAGE=/boot/kernel install-mode", false)).To(BeFalse())
		})

		// nodepair.enable used to start the plain installer, back when the
		// live medium's job was to show the pairing QR code. The medium boots
		// the front-facing installer now, and install-mode is the one keyword
		// that asks for the plain one, so the legacy keyword decides nothing.
		It("no longer starts an installer on the legacy nodepair.enable keyword", func() {
			const cmdline = "BOOT_IMAGE=/boot/kernel nodepair.enable"
			Expect(evalGuard(plain, cmdline, true)).To(BeFalse())
			Expect(evalGuard(interactive, cmdline, true)).To(BeFalse())
		})
	})

	// The web UI is a frontend of the installer, served in the installer's own
	// process on every boot that runs it. A kairos-webui service beside it is
	// a second process going for the same listen address, which is why the
	// guards keeping the two apart kept growing terms. There is no service any
	// more: no unit, no init script, and no stage that starts or enables one.
	Describe("the retired kairos-webui service", func() {
		It("is not written or started by any bundled cloudconfig", func() {
			entries, err := os.ReadDir(filepath.Join("..", "bundled", "cloudconfigs"))
			Expect(err).NotTo(HaveOccurred())

			for _, e := range entries {
				if e.IsDir() || filepath.Ext(e.Name()) != ".yaml" {
					continue
				}

				content, err := os.ReadFile(filepath.Join("..", "bundled", "cloudconfigs", e.Name()))
				Expect(err).NotTo(HaveOccurred(), "read cloudconfig %s", e.Name())

				var cfg guardConfig
				Expect(yaml.Unmarshal(content, &cfg)).To(Succeed(), "parse cloudconfig %s", e.Name())

				for stage, stages := range cfg.Stages {
					for _, s := range stages {
						for _, f := range s.Files {
							Expect(f.Path).NotTo(ContainSubstring("kairos-webui"),
								"%s stage %q writes %s", e.Name(), stage, f.Path)
						}
						for _, c := range s.Commands {
							Expect(c).NotTo(ContainSubstring("kairos-webui"),
								"%s stage %q runs %q", e.Name(), stage, c)
						}
					}
				}
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
