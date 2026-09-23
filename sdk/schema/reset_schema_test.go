package schema_test

import (
	. "github.com/kairos-io/kairos/v4/sdk/schema"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// The reset block used to be absent from RootSchema, with the same
// consequences as the upgrade one. See kairos-io/kairos#4925.
var _ = Describe("Reset block in the root schema", func() {
	validate := func(yaml string) *KConfig {
		config, err := NewConfigFromYAML(yaml, RootSchema{})
		Expect(err).ToNot(HaveOccurred())
		return config
	}

	It("accepts the block as the configuration reference documents it", func() {
		config := validate(`#cloud-config
users:
- name: kairos
reset:
  reboot: true
  poweroff: true
  system:
    uri: "oci:quay.io/kairos/opensuse:latest"
  grub-entry-name: Kairos
  reset-persistent: true
  reset-oem: false
  tty: ttyS0
  extra-dirs-rootfs:
    - /data`)

		Expect(config.IsValid()).To(BeTrue(), func() string { return config.ValidationError.Error() })
	})

	It("rejects reset-persistent written as a string", func() {
		config := validate(`#cloud-config
users:
- name: kairos
reset:
  reset-persistent: yes-please`)

		Expect(config.IsValid()).To(BeFalse())
		Expect(config.ValidationError.Error()).To(ContainSubstring("/reset/reset-persistent"))
		Expect(config.ValidationError.Error()).To(ContainSubstring("expected boolean, but got string"))
	})
})
