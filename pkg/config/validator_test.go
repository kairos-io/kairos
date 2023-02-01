package config_test

import (
	. "github.com/kairos-io/kairos/pkg/config"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Config Validator", func() {
	Context("With invalid YAML syntax", func() {
		data := `#cloud-config
this is:
- invalid
yaml`

		It("errors", func() {
			Expect(Validate(data, DefaultHeader)).To(MatchError("yaml: line 4: could not find expected ':'"))
		})
	})

	Context("Without a header", func() {
		data := `---
users:
- name: "kairos"
  passwd: "kairos"`

		It("errors", func() {
			Expect(Validate(data, DefaultHeader)).To(MatchError("missing #cloud-config header"))
		})
	})
})
