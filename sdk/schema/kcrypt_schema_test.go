package schema_test

import (
	. "github.com/kairos-io/kairos/v4/sdk/schema"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Kcrypt Schema", func() {
	var config *KConfig
	var err error
	var yaml string

	JustBeforeEach(func() {
		config, err = NewConfigFromYAML(yaml, RootSchema{})
		Expect(err).ToNot(HaveOccurred())
	})

	Context("with the boot time encryption opt-in", func() {
		BeforeEach(func() {
			yaml = `#cloud-config
users:
  - name: kairos
    passwd: kairos
install:
  encrypted_partitions:
    - COS_PERSISTENT
kcrypt:
  encrypt_on_boot: true`
		})

		It("succeeds", func() {
			Expect(config.IsValid()).To(BeTrue(), func() string { return config.ValidationError.Error() })
		})
	})

	Context("with a challenger server and TPM settings", func() {
		BeforeEach(func() {
			yaml = `#cloud-config
users:
  - name: kairos
    passwd: kairos
kcrypt:
  challenger:
    challenger_server: "https://challenger.example.org"
    mdns: true
    certificate: "cert data"
  nv_index: "0x1500000"
  c_index: "0x1500001"
  tpm_device: "/dev/tpmrm0"`
		})

		It("succeeds", func() {
			Expect(config.IsValid()).To(BeTrue(), func() string { return config.ValidationError.Error() })
		})
	})

	Context("when encrypt_on_boot is not a boolean", func() {
		BeforeEach(func() {
			yaml = `#cloud-config
users:
  - name: kairos
    passwd: kairos
kcrypt:
  encrypt_on_boot: "definitely"`
		})

		It("errors", func() {
			Expect(config.IsValid()).To(BeFalse())
			Expect(config.ValidationError.Error()).To(MatchRegexp("expected boolean"))
		})
	})

	Context("when the challenger server is not a string", func() {
		BeforeEach(func() {
			yaml = `#cloud-config
users:
  - name: kairos
    passwd: kairos
kcrypt:
  challenger:
    challenger_server: 42`
		})

		It("errors", func() {
			Expect(config.IsValid()).To(BeFalse())
			Expect(config.ValidationError.Error()).To(MatchRegexp("expected string"))
		})
	})
})
