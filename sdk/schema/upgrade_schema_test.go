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

	It("rejects a boot entry that is neither the active nor the recovery one", func() {
		config := validate(`#cloud-config
users:
- name: kairos
upgrade:
  entry: passive`)

		Expect(config.IsValid()).To(BeFalse())
		Expect(config.ValidationError.Error()).To(ContainSubstring("/upgrade/entry"))
	})

	It("rejects a recovery flag that is not a boolean", func() {
		config := validate(`#cloud-config
users:
- name: kairos
upgrade:
  recovery: "true"`)

		Expect(config.IsValid()).To(BeFalse())
		Expect(config.ValidationError.Error()).To(ContainSubstring("/upgrade/recovery"))
	})
})
