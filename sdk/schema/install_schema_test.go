package schema_test

import (
	. "github.com/kairos-io/kairos/v4/sdk/schema"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Install Schema", func() {
	var config *KConfig
	var err error
	var yaml string

	JustBeforeEach(func() {
		config, err = NewConfigFromYAML(yaml, InstallSchema{})
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

	Context("when device has 'special' characters", func() {
		BeforeEach(func() {
			yaml = `#cloud-config
device: "/dev/disk/by-path/pci-0000:03:00.0-scsi-0:0:0:0"`
		})

		It("succeedes", func() {
			Expect(config.IsValid()).To(BeTrue(), func() string { return config.ValidationError.Error() })
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
			Expect(config.ValidationError.Error()).
				To(ContainSubstring("does not match pattern '^(auto|/dev/.+|script://.+)$'"))
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
			Expect(config.ValidationError.Error()).To(MatchRegexp("value must be false"))
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

	Context("with oem_files", func() {
		Context("a well formed entry", func() {
			BeforeEach(func() {
				yaml = `#cloud-config
device: auto
oem_files:
  - name: foo
    content: |
      #cloud-config
      users:
        - name: kairos`
			})

			It("succeedes", func() {
				Expect(config.IsValid()).To(BeTrue(), func() string { return config.ValidationError.Error() })
			})
		})

		Context("an entry without a name", func() {
			BeforeEach(func() {
				yaml = `#cloud-config
device: auto
oem_files:
  - content: "#cloud-config"`
			})

			It("errors", func() {
				Expect(config.IsValid()).NotTo(BeTrue())
				Expect(config.ValidationError.Error()).To(ContainSubstring("name"))
			})
		})

		Context("an entry without content", func() {
			BeforeEach(func() {
				yaml = `#cloud-config
device: auto
oem_files:
  - name: foo`
			})

			It("errors", func() {
				Expect(config.IsValid()).NotTo(BeTrue())
				Expect(config.ValidationError.Error()).To(ContainSubstring("content"))
			})
		})

		Context("an entry whose name contains a slash", func() {
			BeforeEach(func() {
				yaml = `#cloud-config
device: auto
oem_files:
  - name: sub/foo
    content: "#cloud-config"`
			})

			It("errors", func() {
				Expect(config.IsValid()).NotTo(BeTrue())
				Expect(config.ValidationError.Error()).
					To(ContainSubstring("does not match pattern '^[^/]+$'"))
			})
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
