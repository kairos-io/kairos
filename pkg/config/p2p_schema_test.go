package config_test

import (
	"strings"

	. "github.com/kairos-io/kairos/pkg/config"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Users Schema", func() {
	var config *KConfig
	var err error
	var yaml string

	JustBeforeEach(func() {
		config, err = NewConfigFromYAML(yaml, DefaultHeader, P2PSchema{})
		Expect(err).ToNot(HaveOccurred())
	})

	Context("With a network_token and p2p.auto.enable = false", func() {
		BeforeEach(func() {
			yaml = `#cloud-config
network_token: foobar
auto:
  enable: false`
		})

		It("errors", func() {
			Expect(config.IsValid()).NotTo(BeTrue())
			Expect(
				strings.Contains(config.ValidationError(),
					"value must be \"\"",
				),
			).To(BeTrue())
		})
	})

	Context("With an empty network_token and p2p.auto.enable = true", func() {
		BeforeEach(func() {
			yaml = `#cloud-config
network_token: ""
auto:
  enable: true`
		})

		It("Fails", func() {
			Expect(config.IsValid()).NotTo(BeTrue())
			Expect(
				strings.Contains(config.ValidationError(),
					"length must be >= 1, but got 0",
				),
			).To(BeTrue())
		})
	})

	Context("With a network_token and p2p.auto.enable = true", func() {
		BeforeEach(func() {
			yaml = `#cloud-config
network_token: "foobar"
auto:
  enable: true`
		})

		It("succeeds", func() {
			Expect(config.IsValid()).To(BeTrue())
		})
	})
})
