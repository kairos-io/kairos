package config_test

import (
	"strings"

	. "github.com/kairos-io/kairos/pkg/config"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Config Validator", func() {
	// var validator *Validator
	var data string

	JustBeforeEach(func() {
		// validator = &Validator{
		// 	Header: DefaultHeader,
		// }
	})

	Context("With invalid YAML syntax", func() {
		BeforeEach(func() {
			data = `#cloud-config
this is:
- invalid
yaml`
		})

		It("errors", func() {
			Expect(Validate(data, DefaultHeader)).To(MatchError("yaml: line 4: could not find expected ':'"))
		})
	})

	Context("Without a header", func() {
		BeforeEach(func() {
			data = `---
users:
- name: "kairos"
  passwd: "kairos"`
		})

		It("errors", func() {
			Expect(Validate(data, DefaultHeader)).To(MatchError("missing #cloud-config header"))
		})
	})

	Context("When `users` is empty", func() {
		BeforeEach(func() {
			data = `#cloud-config
users: []`
		})

		It("errors", func() {
			Expect(Validate(data, DefaultHeader).Error()).To(MatchRegexp("minimum 1 items required, but found 0 items"))
		})
	})

	Context("When a user has no name", func() {
		BeforeEach(func() {
			data = `#cloud-config
users:
- passwd: foobar`
		})

		It("errors", func() {
			Expect(Validate(data, DefaultHeader).Error()).To(MatchRegexp("missing properties: 'name'"))
		})
	})

	Context("When a user name doesn't fit the pattern", func() {
		BeforeEach(func() {
			data = `#cloud-config
users:
- name: "007"
  passwd: "bond"`
		})

		It("errors", func() {
			Expect(
				strings.Contains(Validate(data, DefaultHeader).Error(),
					"does not match pattern '([a-z_][a-z0-9_]{0,30})'",
				),
			).To(BeTrue())
		})
	})

	Context("With a valid user", func() {
		BeforeEach(func() {
			data = `#cloud-config
users:
- name: "kairos"
  passwd: "kairos"
  lock_passwd: true
  groups: "admin"
  ssh_authorized_keys:
    - github:mudler`
		})

		It("succeeds", func() {
			Expect(Validate(data, DefaultHeader)).ToNot(HaveOccurred())
		})
	})

	Context("With a network_token and p2p.auto.enable = false", func() {
		BeforeEach(func() {
			data = `#cloud-config
users:
- name: "kairos"
  passwd: "kairos"
p2p:
  network_token: foobar
  auto:
    enable: false`
		})

		It("errors", func() {
			err := Validate(data, DefaultHeader)
			Expect(
				strings.Contains(err.Error(),
					"value must be \"\"",
				),
			).To(BeTrue())
		})
	})

	Context("With an empty network_token and p2p.auto.enable = true", func() {
		BeforeEach(func() {
			data = `#cloud-config
users:
- name: "kairos"
  passwd: "kairos"
p2p:
  network_token: ""
  auto:
    enable: true`
		})

		It("Fails", func() {
			err := Validate(data, DefaultHeader)
			Expect(
				strings.Contains(err.Error(),
					"length must be >= 1, but got 0",
				),
			).To(BeTrue())
		})
	})

	Context("With a network_token and p2p.auto.enable = true", func() {
		BeforeEach(func() {
			data = `#cloud-config
users:
- name: "kairos"
  passwd: "kairos"
p2p:
  network_token: "foobar"
  auto:
    enable: true`
		})

		It("succeeds", func() {
			Expect(Validate(data, DefaultHeader)).ToNot(HaveOccurred())
		})
	})
})
