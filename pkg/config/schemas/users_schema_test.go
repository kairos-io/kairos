package config_test

import (
	"strings"

	. "github.com/kairos-io/kairos/v2/pkg/config/schemas"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Users Schema", func() {
	var config *KConfig
	var err error
	var yaml string

	JustBeforeEach(func() {
		config, err = NewConfigFromYAML(yaml, UserSchema{})
		Expect(err).ToNot(HaveOccurred())
	})

	Context("When a user has no name", func() {
		BeforeEach(func() {
			yaml = `#cloud-config
passwd: foobar`
		})

		It("errors", func() {
			Expect(config.IsValid()).NotTo(BeTrue())
			Expect(config.ValidationError.Error()).To(MatchRegexp("missing properties: 'name'"))
		})
	})

	Context("When a user name doesn't fit the pattern", func() {
		BeforeEach(func() {
			yaml = `#cloud-config
name: "007"
passwd: "bond"`
		})

		It("errors", func() {
			Expect(config.IsValid()).NotTo(BeTrue())
			Expect(
				strings.Contains(config.ValidationError.Error(),
					"does not match pattern '([a-z_][a-z0-9_]{0,30})'",
				),
			).To(BeTrue())
		})
	})

	Context("With only the required attributes", func() {
		BeforeEach(func() {
			yaml = `#cloud-config
name: "kairos"`
		})

		It("succeeds", func() {
			Expect(config.IsValid()).To(BeTrue())
		})
	})

	Context("With all possible attributes", func() {
		BeforeEach(func() {
			yaml = `#cloud-config
name: "kairos"
passwd: "kairos"
lock_passwd: true
groups: 
 - "admin"
ssh_authorized_keys:
  - github:mudler`
		})

		It("succeeds", func() {
			Expect(config.IsValid()).To(BeTrue())
		})
	})
})
