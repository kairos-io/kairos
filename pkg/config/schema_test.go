package config_test

import (
	. "github.com/kairos-io/kairos/pkg/config"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Schema", func() {
	var config *KConfig
	var err error
	var yaml string

	JustBeforeEach(func() {
		config, err = NewConfigFromYAML(yaml, DefaultHeader, Schema{})
	})

	Context("With invalid YAML syntax", func() {
		BeforeEach(func() {
			yaml = `#cloud-config
this is:
- invalid
yaml`
		})

		It("errors", func() {
			Expect(err.Error()).To(MatchRegexp("yaml: line 4: could not find expected ':'"))
		})
	})

	Context("With the wrong header", func() {
		BeforeEach(func() {
			yaml = `---
users:
- name: "kairos"
  passwd: "kairos"`
		})

		It("errors", func() {
			Expect(err.Error()).To(MatchRegexp("missing #cloud-config header"))
		})
	})

	Context("When `users` is empty", func() {
		BeforeEach(func() {
			yaml = `#cloud-config
users: []`
		})

		It("errors", func() {
			Expect(err).ToNot(HaveOccurred())
			Expect(config.IsValid()).NotTo(BeTrue())
			Expect(config.ValidationError()).To(MatchRegexp("minimum 1 items required, but found 0 items"))
		})
	})
})
