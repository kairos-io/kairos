package config_test

import (
	"strings"

	. "github.com/kairos-io/kairos/pkg/config/schemas"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Install Schema", func() {
	var config *KConfig
	var err error
	var yaml string

	JustBeforeEach(func() {
		config, err = NewConfigFromYAML(yaml, "#cloud-config", InstallSchema{})
		Expect(err).ToNot(HaveOccurred())
	})

	Context("when device is auto", func() {
		BeforeEach(func() {
			yaml = `#cloud-config
device: auto`
		})

		It("succeedes", func() {
			Expect(config.IsValid()).To(BeTrue())
		})
	})

	Context("when device is a path", func() {
		BeforeEach(func() {
			yaml = `#cloud-config
device: /dev/sda`
		})

		It("succeedes", func() {
			Expect(config.IsValid()).To(BeTrue())
		})
	})

	Context("when device is other than a path or auto", func() {
		BeforeEach(func() {
			yaml = `#cloud-config
device: foobar`
		})

		It("errors", func() {
			Expect(config.IsValid()).NotTo(BeTrue())
			Expect(
				strings.Contains(config.ValidationError(),
					"does not match pattern '^(auto|/|(/[a-zA-Z0-9_-]+)+)$'",
				),
			).To(BeTrue())
		})
	})

	Context("when reboot and poweroff are true", func() {
		BeforeEach(func() {
			yaml = `#cloud-config
device: /dev/sda
reboot: true
poweroff: true`
		})

		It("errors", func() {
			Expect(config.IsValid()).NotTo(BeTrue())
			Expect(config.ValidationError()).To(MatchRegexp("value must be false"))
		})
	})

	Context("when reboot is true and poweroff is false", func() {
		BeforeEach(func() {
			yaml = `#cloud-config
device: /dev/sda
reboot: true
poweroff: false`
		})

		It("succeedes", func() {
			Expect(config.IsValid()).To(BeTrue())
		})
	})

	Context("when reboot is false and poweroff is true", func() {
		BeforeEach(func() {
			yaml = `#cloud-config
device: /dev/sda
reboot: false
poweroff: true`
		})

		It("succeedes", func() {
			Expect(config.IsValid()).To(BeTrue())
		})
	})

	Context("with no power management set", func() {
		BeforeEach(func() {
			yaml = `#cloud-config
device: /dev/sda`
		})

		It("succeedes", func() {
			Expect(config.IsValid()).To(BeTrue())
		})
	})

	Context("with all possible options", func() {
		BeforeEach(func() {
			yaml = `#cloud-config
device: "/dev/sda"
reboot: true
auto: true
image: "docker:.."
bundles:
  - rootfs_path: /usr/local/lib/extensions/<name>
    targets:
    - container://<image>
grub_options:
  extra_cmdline: "config_url=http://"
  extra_active_cmdline: "config_url=http://"
  extra_passive_cmdline: "config_url=http://"
  default_menu_entry: "foobar"
env:
  - foo=barevice: /dev/sda`
		})

		It("succeedes", func() {
			Expect(config.IsValid()).To(BeTrue())
		})
	})
})
