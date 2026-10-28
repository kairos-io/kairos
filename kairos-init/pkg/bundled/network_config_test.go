package bundled_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/kairos-io/kairos/v4/kairos-init/pkg/bundled"
	"github.com/kairos-io/kairos/v4/kairos-init/pkg/values"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"
)

// networkdBinary is where every distro Kairos builds keeps the daemon, and
// where dracut's 01systemd-networkd module installs it in the initramfs. The
// build steps in kairos-init/pkg/stages/steps_init.go test the same path.
const networkdBinary = "/usr/lib/systemd/systemd-networkd"

type networkStage struct {
	Name     string `yaml:"name"`
	If       string `yaml:"if"`
	OnlyIfOs string `yaml:"only_os"`
	Commands []string
	Files    []struct {
		Path string `yaml:"path"`
	} `yaml:"files"`
	Directories []struct {
		Path string `yaml:"path"`
	} `yaml:"directories"`
	Systemctl struct {
		Enable []string `yaml:"enable"`
	} `yaml:"systemctl"`
}

// networkStages returns every stage of the shipped 05_network.yaml, in file
// order, regardless of which stage name it hangs off.
func networkStages() []networkStage {
	raw, err := bundled.EmbeddedConfigs.ReadFile("cloudconfigs/05_network.yaml")
	Expect(err).ToNot(HaveOccurred())

	var config struct {
		Stages map[string][]networkStage `yaml:"stages"`
	}
	Expect(yaml.Unmarshal(raw, &config)).To(Succeed())

	var all []networkStage
	for _, stages := range config.Stages {
		all = append(all, stages...)
	}
	Expect(all).ToNot(BeEmpty())
	return all
}

// touchesNetworkd reports whether the stage only makes sense on a system that
// actually runs systemd-networkd: it writes into /etc/systemd/network, or it
// drives the daemon through networkctl.
func touchesNetworkd(stage networkStage) bool {
	for _, file := range stage.Files {
		if strings.HasPrefix(file.Path, "/etc/systemd/network") {
			return true
		}
	}
	for _, dir := range stage.Directories {
		if strings.HasPrefix(dir.Path, "/etc/systemd/network") {
			return true
		}
	}
	for _, command := range stage.Commands {
		if strings.Contains(command, "networkctl") {
			return true
		}
	}
	return false
}

var _ = Describe("NetworkConfig", func() {
	// The RHEL family ships NetworkManager and no systemd-networkd, so these
	// stages used to write two .network files nothing reads and then run
	// networkctl, which is not installed. That failed the initramfs stage on
	// every boot of every Rocky, AlmaLinux, CentOS, Oracle Linux and Fedora
	// image. Gating on the OS name cannot answer this, because the family
	// supports systemd-networkd when the user puts it in the base image.
	It("guards every systemd-networkd stage on the daemon being installed", func() {
		guarded := 0
		for _, stage := range networkStages() {
			if !touchesNetworkd(stage) {
				continue
			}
			Expect(stage.If).To(ContainSubstring(networkdBinary),
				"stage %q configures systemd-networkd without testing for it", stage.Name)
			guarded++
		}
		Expect(guarded).To(Equal(2), "expected the rootfs.before and initramfs DHCP stages")
	})

	// A yip `if` runs through `sh -c`, which is dash on Debian and busybox ash
	// on Alpine, so assert the guard is a real shell test rather than trusting
	// it reads like one.
	It("writes a guard that a POSIX shell evaluates against the real path", func() {
		var guard string
		for _, stage := range networkStages() {
			if touchesNetworkd(stage) {
				guard = stage.If
				break
			}
		}
		Expect(guard).ToNot(BeEmpty())

		root := GinkgoT().TempDir()
		Expect(os.MkdirAll(filepath.Join(root, "usr/lib/systemd"), 0o755)).To(Succeed())

		// The guard hardcodes an absolute path, so point the test at a copy
		// rooted in the temp dir rather than at the machine running the suite.
		rooted := strings.ReplaceAll(guard, networkdBinary, filepath.Join(root, networkdBinary))

		Expect(exec.Command("sh", "-c", rooted).Run()).To(HaveOccurred(),
			"guard passed with no daemon installed")

		Expect(os.WriteFile(filepath.Join(root, networkdBinary), []byte("#!/bin/sh\n"), 0o755)).To(Succeed())
		Expect(exec.Command("sh", "-c", rooted).Run()).To(Succeed(),
			"guard failed with the daemon installed")
	})

	// Only RHEL's PRETTY_NAME contains "Red Hat", and yip matches only_os
	// against PRETTY_NAME. A stage meant for the whole family has to name the
	// rest of it, the way values.RHELFamilyRegex does.
	It("enables the RHEL family services on the whole family", func() {
		prettyNames := []string{
			"Red Hat Enterprise Linux 9.4 (Plow)",
			"Rocky Linux 9.8 (Blue Onyx)",
			"AlmaLinux 9.4 (Seafoam Ocelot)",
			"CentOS Stream 9",
			"Oracle Linux Server 9.4",
			"Fedora Linux 40 (Container Image)",
		}

		checked := 0
		for _, stage := range networkStages() {
			enabled := strings.Join(stage.Systemctl.Enable, " ")
			if !strings.Contains(enabled, "systemd-networkd") && !strings.Contains(enabled, "systemd-resolved") {
				continue
			}
			if !strings.Contains(stage.If, "/usr/lib/systemd/systemd-") {
				continue
			}
			pattern, err := regexp.Compile(stage.OnlyIfOs)
			Expect(err).ToNot(HaveOccurred())

			for _, name := range prettyNames {
				Expect(pattern.MatchString(name)).To(BeTrue(),
					"stage %q skips %q", stage.Name, name)
			}
			checked++
		}
		Expect(checked).To(Equal(2), "expected the networkd and resolved enable stages")
	})

	// The same list already exists in Go. If the two drift again, the stages
	// above start disagreeing with the build steps that enable the same units.
	It("covers the same distributions as the build steps", func() {
		family, err := regexp.Compile(values.RHELFamilyRegex)
		Expect(err).ToNot(HaveOccurred())

		for _, stage := range networkStages() {
			if !strings.Contains(strings.Join(stage.Systemctl.Enable, " "), "systemd-") {
				continue
			}
			if !strings.Contains(stage.If, "/usr/lib/systemd/systemd-") {
				continue
			}
			pattern, err := regexp.Compile(stage.OnlyIfOs)
			Expect(err).ToNot(HaveOccurred())

			for _, name := range []string{
				"Rocky Linux 9.8 (Blue Onyx)",
				"AlmaLinux 9.4 (Seafoam Ocelot)",
				"Red Hat Enterprise Linux 9.4 (Plow)",
			} {
				Expect(pattern.MatchString(name)).To(Equal(family.MatchString(name)),
					"stage %q disagrees with values.RHELFamilyRegex on %q", stage.Name, name)
			}
		}
	})
})
