package schema_test

import (
	"encoding/json"

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

		It("succeeds", func() {
			Expect(config.IsValid()).To(BeTrue())
		})
	})

	Context("when device has 'special' characters", func() {
		BeforeEach(func() {
			yaml = `#cloud-config
device: "/dev/disk/by-path/pci-0000:03:00.0-scsi-0:0:0:0"`
		})

		It("succeeds", func() {
			Expect(config.IsValid()).To(BeTrue(), func() string { return config.ValidationError.Error() })
		})
	})

	Context("when device is a path", func() {
		BeforeEach(func() {
			yaml = `#cloud-config
device: /dev/sda`
		})

		It("succeeds", func() {
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

		It("succeeds", func() {
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

		It("succeeds", func() {
			Expect(config.IsValid()).To(BeTrue())
		})
	})

	Context("with no power management set", func() {
		BeforeEach(func() {
			yaml = `#cloud-config
device: /dev/sda`
		})

		It("succeeds", func() {
			Expect(config.IsValid()).To(BeTrue())
		})
	})

	Context("when no-format is set", func() {
		BeforeEach(func() {
			yaml = `#cloud-config
device: /dev/sda
no-format: true`
		})

		It("succeedes", func() {
			Expect(config.IsValid()).To(BeTrue(), func() string { return config.ValidationError.Error() })
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

		It("succeeds", func() {
			Expect(config.IsValid()).To(BeTrue())
		})
	})

	Context("with selinux enabled and mode enforcing", func() {
		BeforeEach(func() {
			yaml = `#cloud-config
selinux:
  enabled: true
  mode: enforcing`
		})

		It("succeeds", func() {
			Expect(config.IsValid()).To(BeTrue())
		})
	})

	Context("with selinux enabled and mode permissive", func() {
		BeforeEach(func() {
			yaml = `#cloud-config
selinux:
  enabled: true
  mode: permissive`
		})

		It("succeeds", func() {
			Expect(config.IsValid()).To(BeTrue())
		})
	})

	Context("with selinux enabled and no mode (permissive is the default)", func() {
		BeforeEach(func() {
			yaml = `#cloud-config
selinux:
  enabled: true`
		})

		It("succeeds", func() {
			Expect(config.IsValid()).To(BeTrue())
		})
	})

	Context("with an invalid selinux mode", func() {
		BeforeEach(func() {
			yaml = `#cloud-config
selinux:
  enabled: true
  mode: paranoid`
		})

		It("errors", func() {
			Expect(config.IsValid()).NotTo(BeTrue())
			Expect(config.ValidationError.Error()).To(MatchRegexp(`value must be one of "enforcing", "permissive"`))
		})
	})
})

var _ = Describe("Install partition schema", func() {
	var config *KConfig
	var err error
	var yaml string

	JustBeforeEach(func() {
		config, err = NewConfigFromYAML(yaml, InstallSchema{})
		Expect(err).ToNot(HaveOccurred())
	})

	Context("when an extra partition carries a filesystem label", func() {
		BeforeEach(func() {
			yaml = `#cloud-config
extra-partitions:
  - name: data
    size: 8192
    fs: ext4
    label: DATA`
		})

		It("succeeds", func() {
			Expect(config.IsValid()).To(BeTrue(), func() string { return config.ValidationError.Error() })
		})

		It("declares label as a property rather than accepting it silently", func() {
			generated, gErr := GenerateSchema(InstallSchema{}, "")
			Expect(gErr).ToNot(HaveOccurred())

			var doc map[string]interface{}
			Expect(json.Unmarshal([]byte(generated), &doc)).To(Succeed())

			definitions := doc["definitions"].(map[string]interface{})
			extra := definitions["SchemaExtraPartition"].(map[string]interface{})
			properties := extra["properties"].(map[string]interface{})
			Expect(properties).To(HaveKey("label"))
			Expect(extra["required"]).To(ContainElement("name"))
		})
	})

	Context("when an extra partition has no name", func() {
		BeforeEach(func() {
			yaml = `#cloud-config
extra-partitions:
  - size: 8192
    fs: ext4`
		})

		It("errors, as the installer does when it cannot find the partition to format", func() {
			Expect(config.IsValid()).NotTo(BeTrue())
			Expect(config.ValidationError.Error()).To(MatchRegexp(`missing properties: 'name'`))
		})
	})

	Context("when the EFI partition is given a size", func() {
		BeforeEach(func() {
			yaml = `#cloud-config
partitions:
  efi:
    size: 256`
		})

		It("succeeds", func() {
			Expect(config.IsValid()).To(BeTrue(), func() string { return config.ValidationError.Error() })
		})

		It("declares efi under partitions rather than accepting it silently", func() {
			generated, gErr := GenerateSchema(InstallSchema{}, "")
			Expect(gErr).ToNot(HaveOccurred())

			var doc map[string]interface{}
			Expect(json.Unmarshal([]byte(generated), &doc)).To(Succeed())

			definitions := doc["definitions"].(map[string]interface{})
			elemental := definitions["SchemaElementalPartitions"].(map[string]interface{})
			properties := elemental["properties"].(map[string]interface{})
			Expect(properties).To(HaveKey("efi"))
		})
	})

})
