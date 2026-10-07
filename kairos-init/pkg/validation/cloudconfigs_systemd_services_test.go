package validation_test

import (
	"os"
	"path/filepath"

	"github.com/kairos-io/kairos/v4/kairos-init/pkg/values"
	"github.com/kairos-io/kairos/v4/sdk/types/logger"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"
)

// serviceStage reads the systemctl block of a bundled cloud-config stage,
// which guardStage does not carry.
type serviceStage struct {
	Name               string `yaml:"name"`
	OnlyServiceManager string `yaml:"only_service_manager"`
	Systemctl          struct {
		Enable []string `yaml:"enable"`
	} `yaml:"systemctl"`
}

type serviceConfig struct {
	Stages map[string][]serviceStage `yaml:"stages"`
}

// servicePackages names the package each enabled unit comes from, per family.
// Only the units that need one are listed: multi-user.target and getty@tty1
// ship with systemd itself, so nothing installs them.
//
// The package name is the same on all four families for logrotate, and differs
// for the iSCSI initiator, which is why this is a map of maps rather than an
// assertion that the unit and the package share a name.
var servicePackages = map[string]map[values.Family]string{
	"iscsid": {
		values.DebianFamily: "open-iscsi",
		values.RedHatFamily: "iscsi-initiator-utils",
		values.SUSEFamily:   "open-iscsi",
	},
	"logrotate.timer": {
		values.DebianFamily: "logrotate",
		values.RedHatFamily: "logrotate",
		values.SUSEFamily:   "logrotate",
	},
}

// systemdSystems are the distro, family and architecture triples kairos-init
// builds that run systemd, so every one of them runs the stage below. The
// Alpine family is absent on purpose: it runs OpenRC, and
// 09_openrc_services.yaml enables its own set.
var systemdSystems = []values.System{
	{Distro: values.Ubuntu, Family: values.DebianFamily, Version: "24.04", Arch: values.ArchAMD64},
	{Distro: values.Ubuntu, Family: values.DebianFamily, Version: "24.04", Arch: values.ArchARM64},
	{Distro: values.Debian, Family: values.DebianFamily, Version: "13", Arch: values.ArchAMD64},
	{Distro: values.Debian, Family: values.DebianFamily, Version: "13", Arch: values.ArchARM64},
	{Distro: values.Fedora, Family: values.RedHatFamily, Version: "40", Arch: values.ArchAMD64},
	{Distro: values.Fedora, Family: values.RedHatFamily, Version: "40", Arch: values.ArchARM64},
	{Distro: values.RockyLinux, Family: values.RedHatFamily, Version: "9", Arch: values.ArchAMD64},
	{Distro: values.AlmaLinux, Family: values.RedHatFamily, Version: "9", Arch: values.ArchARM64},
	{Distro: values.OpenSUSELeap, Family: values.SUSEFamily, Version: "15.6", Arch: values.ArchAMD64},
	{Distro: values.OpenSUSELeap, Family: values.SUSEFamily, Version: "15.6", Arch: values.ArchARM64},
}

var _ = Describe("Bundled systemd services cloudconfig", func() {
	// 09_systemd_services.yaml ships on every image and enables its unit list
	// in the initramfs stage, so a unit it enables has to come from a package
	// the image installs. iscsid was missing from the Red Hat family, where
	// the RPM is called iscsi-initiator-utils rather than open-iscsi, so the
	// enable failed on every boot and no iSCSI backed volume could attach.
	It("installs the package behind every unit it enables", func() {
		content, err := os.ReadFile(filepath.Join("..", "bundled", "cloudconfigs", "09_systemd_services.yaml"))
		Expect(err).NotTo(HaveOccurred())

		var cfg serviceConfig
		Expect(yaml.Unmarshal(content, &cfg)).To(Succeed())

		// Read the unit names out of the file rather than retyping them, so
		// that adding a unit there reaches this spec.
		var enabled []string
		for _, stage := range cfg.Stages["initramfs"] {
			if stage.OnlyServiceManager != "systemd" {
				continue
			}
			enabled = append(enabled, stage.Systemctl.Enable...)
		}
		Expect(enabled).NotTo(BeEmpty(), "no systemd stage enables anything")
		Expect(enabled).To(ContainElement("iscsid"))

		l := logger.NewKairosLogger("validation", "error", true)
		checked := map[string]bool{}
		for _, unit := range enabled {
			perFamily, ok := servicePackages[unit]
			if !ok {
				// A unit systemd itself provides, or one added to the
				// cloud-config without a package mapping here. Left out of the
				// assertion rather than guessed at.
				continue
			}
			for _, sys := range systemdSystems {
				pkg, ok := perFamily[sys.Family]
				Expect(ok).To(BeTrue(), "no %s package name known for %s", unit, sys.Family)

				packages, err := values.GetPackages(sys, l)
				Expect(err).NotTo(HaveOccurred(), "%s/%s", sys.Distro, sys.Arch)
				Expect(packages).To(ContainElement(pkg),
					"%s/%s installs no %s, but 09_systemd_services.yaml enables %s on every boot",
					sys.Distro, sys.Arch, pkg, unit)
			}
			checked[unit] = true
		}
		Expect(checked).To(HaveKey("iscsid"))
		Expect(checked).To(HaveKey("logrotate.timer"))
	})
})
