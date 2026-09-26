package validation_test

import (
	"os"
	"path/filepath"

	"github.com/kairos-io/kairos/v4/kairos-init/pkg/stages"
	"github.com/kairos-io/kairos/v4/kairos-init/pkg/values"
	"github.com/kairos-io/kairos/v4/sdk/types/logger"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"
)

// unitsEveryImageHas lists the units 09_systemd_services.yaml may enable
// without a guard. A unit belongs here only when every systemd image Kairos
// builds has it, and the binding case is Hadron: it is the base for
// amd64-core, amd64-standard, amd64-core-fips, both uki flavors and the three
// arm64 flavors, and kairos-init installs no package on it at all, so nothing
// this repository declares can put a unit there. Each entry below is either
// systemd's own or was read out of the published Hadron rootfs:
//
//	multi-user.target, getty@tty1   systemd ships both
//	iscsid                          usr/lib/systemd/system/iscsid.service in
//	                                ghcr.io/kairos-io/hadron:v0.5.1, built
//	                                from open-iscsi by hadron's Dockerfile
//
// logrotate.timer is deliberately absent: Hadron ships no logrotate, so
// enabling it unguarded failed on every boot of the default flavor
// (kairos-io/kairos#4778). Add a unit here only after checking the Hadron
// image for it, otherwise guard the stage with an `if:` on the unit file.
var unitsEveryImageHas = []string{
	"multi-user.target",
	"getty@tty1",
	"iscsid",
}

type enableStage struct {
	Name                 string `yaml:"name"`
	If                   string `yaml:"if"`
	OnlyIfServiceManager string `yaml:"only_service_manager"`
	Systemctl            struct {
		Enable []string `yaml:"enable"`
	} `yaml:"systemctl"`
}

type enableConfig struct {
	Stages map[string][]enableStage `yaml:"stages"`
}

func readEnableConfig() enableConfig {
	content, err := os.ReadFile(filepath.Join(cloudConfigsDir, "09_systemd_services.yaml"))
	Expect(err).NotTo(HaveOccurred())

	var cfg enableConfig
	Expect(yaml.Unmarshal(content, &cfg)).To(Succeed())
	return cfg
}

var _ = Describe("Bundled systemd services cloudconfig", func() {
	It("enables a unit without a guard only when every systemd image has it", func() {
		seen := 0
		for _, stage := range readEnableConfig().Stages["initramfs"] {
			if stage.OnlyIfServiceManager != "systemd" || stage.If != "" {
				continue
			}
			for _, unit := range stage.Systemctl.Enable {
				seen++
				Expect(unitsEveryImageHas).To(ContainElement(unit),
					"%q enables %s with no guard, so it has to exist on every systemd image, Hadron included",
					stage.Name, unit)
			}
		}
		Expect(seen).NotTo(BeZero(), "no unguarded systemd enable found, the stage this pins has moved")
	})

	It("enables logrotate.timer only where the unit file is present", func() {
		guards := map[string]string{}
		for _, stage := range readEnableConfig().Stages["initramfs"] {
			for _, unit := range stage.Systemctl.Enable {
				if unit == "logrotate.timer" {
					guards[stage.Name] = stage.If
				}
			}
		}
		Expect(guards).To(HaveLen(1), "expected exactly one stage to enable logrotate.timer")
		for name, guard := range guards {
			// Both packaging families put it in the same place:
			// usr/lib/systemd/system/logrotate.timer in
			// quay.io/kairos/ubuntu:20.04-core-amd64-generic-v3.4.1 and in
			// quay.io/kairos/rockylinux:9-core-amd64-generic-v3.5.3.
			Expect(guard).To(ContainSubstring("/usr/lib/systemd/system/logrotate.timer"),
				"%q must test for the unit before enabling it", name)
		}
	})

	// The premise of the list above: Hadron gets no packages, so a unit there
	// can only come from the image. Kept as a spec rather than a comment so
	// that dropping the early return in GetInstallStage lands here.
	It("installs no package on Hadron, so the unit list cannot lean on one", func() {
		l := logger.NewKairosLogger("validation", "error", true)

		installStages, err := stages.GetInstallStage(values.System{
			Distro: values.Hadron,
			Family: values.HadronFamily,
			Arch:   values.ArchAMD64,
		}, l)
		Expect(err).NotTo(HaveOccurred())
		Expect(installStages).To(BeEmpty())

		// The same question asked of a flavor that does install packages
		// returns the logrotate this file used to assume everywhere, which is
		// what makes the assertion above a difference and not a tautology.
		packages, err := values.GetPackages(values.System{
			Distro:  values.RockyLinux,
			Family:  values.RedHatFamily,
			Version: "9",
			Arch:    values.ArchAMD64,
		}, l)
		Expect(err).NotTo(HaveOccurred())
		Expect(packages).To(ContainElement("logrotate"))
	})
})
