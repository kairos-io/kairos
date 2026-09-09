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

type guardStage struct {
	Name               string   `yaml:"name"`
	If                 string   `yaml:"if"`
	OnlyServiceManager string   `yaml:"only_service_manager"`
	Commands           []string `yaml:"commands"`
}

type guardConfig struct {
	Stages map[string][]guardStage `yaml:"stages"`
}

func readStages(name string) []guardStage {
	content, err := os.ReadFile(filepath.Join("..", "bundled", "cloudconfigs", name))
	Expect(err).NotTo(HaveOccurred(), "read cloudconfig %s", name)

	var cfg guardConfig
	Expect(yaml.Unmarshal(content, &cfg)).To(Succeed(), "parse cloudconfig %s", name)
	Expect(cfg.Stages["initramfs"]).NotTo(BeEmpty(), "%s has no initramfs stage", name)
	return cfg.Stages["initramfs"]
}

// stageEnabling returns the systemd initramfs stage that enables the named
// service, so the specs below identify a stage by what it does rather than by
// its position in the file.
func stageEnabling(stages []guardStage, service string) guardStage {
	want := "systemctl enable " + service
	for _, s := range stages {
		if s.OnlyServiceManager != "systemd" {
			continue
		}
		for _, c := range s.Commands {
			if c == want {
				return s
			}
		}
	}
	Fail("no systemd stage enables " + service)
	return guardStage{}
}

// evalGuard runs a stage's `if` expression the way yip does, with sh -c, against
// a synthetic /proc/cmdline. liveMode says whether /run/cos/live_mode exists.
// It reports whether the stage would run.
func evalGuard(expr, cmdline string, liveMode bool) bool {
	root := GinkgoT().TempDir()

	cmdlinePath := filepath.Join(root, "cmdline")
	Expect(os.WriteFile(cmdlinePath, []byte(cmdline+"\n"), 0644)).To(Succeed())

	livePath := filepath.Join(root, "live_mode")
	if liveMode {
		Expect(os.WriteFile(livePath, nil, 0644)).To(Succeed())
	}

	expr = strings.ReplaceAll(expr, "/proc/cmdline", cmdlinePath)
	expr = strings.ReplaceAll(expr, "/run/cos/live_mode", livePath)
	// Never present: these specs cover the live ISO, not a UKI install medium.
	expr = strings.ReplaceAll(expr, "/run/cos/uki_install_mode", filepath.Join(root, "absent"))

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

		It("starts nothing on a plain boot, or outside live mode", func() {
			Expect(evalGuard(plain, "BOOT_IMAGE=/boot/kernel root=LABEL=COS_STATE", true)).To(BeFalse())
			Expect(evalGuard(interactive, "BOOT_IMAGE=/boot/kernel root=LABEL=COS_STATE", true)).To(BeFalse())
			Expect(evalGuard(plain, "BOOT_IMAGE=/boot/kernel install-mode", false)).To(BeFalse())
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
