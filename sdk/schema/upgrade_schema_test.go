package schema_test

import (
	. "github.com/kairos-io/kairos/v4/sdk/schema"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// The upgrade block used to be absent from RootSchema, so every one of these
// configs validated, and a wrong type or a misspelled key surfaced only once
// the upgrade was already running on the node. See kairos-io/kairos#4925.
var _ = Describe("Upgrade block in the root schema", func() {
	validate := func(yaml string) *KConfig {
		config, err := NewConfigFromYAML(yaml, RootSchema{})
		Expect(err).ToNot(HaveOccurred())
		return config
	}

	It("accepts the block as the configuration reference documents it", func() {
		config := validate(`#cloud-config
users:
- name: kairos
upgrade:
  reboot: true
  poweroff: true
  grub-entry-name: Kairos
  recovery: false
  entry: recovery
  system:
    uri: "oci:quay.io/kairos/opensuse:latest"
    size: 4096
  recovery-system:
    uri: "oci:quay.io/kairos/opensuse:latest"
    size: 5000
  extra-dirs-rootfs:
    - /data
  excluded-paths:
    - /var/cache
  allow-insecure-registries: true`)

		Expect(config.IsValid()).To(BeTrue(), func() string { return config.ValidationError.Error() })
	})

	It("rejects a size that is not a number, as the install block already does", func() {
		config := validate(`#cloud-config
users:
- name: kairos
upgrade:
  system:
    size: not-a-number`)

		Expect(config.IsValid()).To(BeFalse())
		Expect(config.ValidationError.Error()).To(ContainSubstring("/upgrade/system/size"))
		Expect(config.ValidationError.Error()).To(ContainSubstring("expected integer, but got string"))
	})

	// `kairos-agent upgrade --boot-entry foo` writes `upgrade.entry: foo` into
	// the scanned config, and the UKI upgrade path installs whichever
	// systemd-boot entry it names, so the key cannot be constrained to the two
	// GRUB entry names.
	It("accepts a boot entry named after a systemd-boot entry", func() {
		config := validate(`#cloud-config
users:
- name: kairos
upgrade:
  entry: my-entry`)

		Expect(config.IsValid()).To(BeTrue(), func() string { return config.ValidationError.Error() })
	})

	DescribeTable("accepts every form of recovery the agent reads as a boolean",
		func(value string) {
			config := validate(`#cloud-config
users:
- name: kairos
upgrade:
  recovery: ` + value)

			Expect(config.IsValid()).To(BeTrue(), func() string { return config.ValidationError.Error() })
		},
		Entry("the literal", "true"),
		Entry("a quoted literal, which is what a template writes", `"true"`),
		Entry("a quoted literal in upper case", `"TRUE"`),
		Entry("a single letter", `"t"`),
		Entry("a number", "1"),
		Entry("zero", "0"),
	)

	DescribeTable("rejects a recovery value the agent cannot read as a boolean",
		func(value string) {
			config := validate(`#cloud-config
users:
- name: kairos
upgrade:
  recovery: ` + value)

			Expect(config.IsValid()).To(BeFalse())
			Expect(config.ValidationError.Error()).To(ContainSubstring("/upgrade/recovery"))
		},
		// YAML 1.2 reads an unquoted yes as a string, and
		// strconv.ParseBool rejects it, so the agent does too.
		Entry("yes", "yes"),
		Entry("a word", "maybe"),
		Entry("a mapping", "{}"),
	)

	It("accepts the other flags written as a template would write them", func() {
		config := validate(`#cloud-config
users:
- name: kairos
upgrade:
  reboot: "true"
  poweroff: "false"
  allow-insecure-registries: 1`)

		Expect(config.IsValid()).To(BeTrue(), func() string { return config.ValidationError.Error() })
	})
})

// The misspelled-key half of kairos-io/kairos#4925. unmarshallFullSpec drops
// a key it does not know without a word, so upgrade.recovery_system means the
// node upgrades its active system, reports success, and leaves recovery on
// the old image.
var _ = Describe("Upgrade block closed to undeclared keys", func() {
	validate := func(yaml string) *KConfig {
		config, err := NewConfigFromYAML(yaml, RootSchema{})
		Expect(err).ToNot(HaveOccurred())
		return config
	}

	DescribeTable("reports a key the upgrade block does not declare",
		func(key string) {
			config := validate(`#cloud-config
users:
- name: kairos
upgrade:
  ` + key + `: foo`)

			Expect(config.IsValid()).To(BeFalse())
			Expect(config.ValidationError.Error()).To(ContainSubstring(key))
		},
		Entry("the underscore spelling of recovery-system", "recovery_system"),
		Entry("the underscore spelling of grub-entry-name", "grub_entry_name"),
		Entry("a key that belongs to the install block", "device"),
		// Decoded into UpgradeUkiSpec and then overwritten with
		// GetEfiPartition before anything reads it.
		Entry("the never-honoured efi-partition", "efi-partition"),
	)

	It("still accepts every key the block declares", func() {
		config := validate(`#cloud-config
users:
- name: kairos
upgrade:
  entry: recovery
  recovery: true
  grub-entry-name: Kairos
  reboot: true
  poweroff: false
  allow-insecure-registries: true
  extra-dirs-rootfs:
  - /var/lib/longhorn
  excluded-paths:
  - /var/cache
  system:
    uri: "oci:quay.io/kairos/opensuse:latest"
  recovery-system:
    uri: "oci:quay.io/kairos/opensuse:latest"`)

		Expect(config.IsValid()).To(BeTrue(), func() string { return config.ValidationError.Error() })
	})

	It("leaves a config with no upgrade block alone", func() {
		config := validate(`#cloud-config
users:
- name: kairos`)

		Expect(config.IsValid()).To(BeTrue(), func() string { return config.ValidationError.Error() })
	})
})
