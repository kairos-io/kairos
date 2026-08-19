package schema_test

import (
	"strings"

	. "github.com/kairos-io/kairos-sdk/schema"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Schema", func() {
	Context("NewConfigFromYAML", func() {
		var config *KConfig
		var err error
		var yaml string

		JustBeforeEach(func() {
			config, err = NewConfigFromYAML(yaml, RootSchema{})
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

		Context("When `users` is empty", func() {
			BeforeEach(func() {
				yaml = `#cloud-config
users: []`
			})

			It("errors", func() {
				Expect(err).ToNot(HaveOccurred())
				Expect(config.IsValid()).NotTo(BeTrue())
				Expect(config.ValidationError.Error()).To(MatchRegexp("minimum 1 items required, but found 0 items"))
			})
		})

		Context("without a valid header", func() {
			BeforeEach(func() {
				yaml = `---
users:
  - name: kairos
    passwd: kairos`
			})

			It("is successful but HasHeader returns false", func() {
				Expect(err).ToNot(HaveOccurred())
				Expect(config.HasHeader()).To(BeFalse())
			})
		})

		Context("With a valid config", func() {
			BeforeEach(func() {
				yaml = `#cloud-config
users:
  - name: kairos
    passwd: kairos`
			})

			It("is successful", func() {
				Expect(err).ToNot(HaveOccurred())
				Expect(config.HasHeader()).To(BeTrue())
			})
		})
	})

	Context("ValidateSemantics", func() {
		var kc *KConfig

		newKC := func(y string) *KConfig {
			c, err := NewConfigFromYAML(y, RootSchema{})
			Expect(err).ToNot(HaveOccurred())
			return c
		}

		Context("when ssh_hardening is not set", func() {
			BeforeEach(func() {
				kc = newKC(`#cloud-config
users:
  - name: kairos
    passwd: kairos`)
			})

			It("returns no warnings and no error", func() {
				warnings, err := kc.ValidateSemantics()
				Expect(err).ToNot(HaveOccurred())
				Expect(warnings).To(BeEmpty())
			})
		})

		Context("when ssh_hardening: true and a user has a key", func() {
			BeforeEach(func() {
				kc = newKC(`#cloud-config
install:
  ssh_hardening: true
users:
  - name: kairos
    ssh_authorized_keys:
      - ssh-ed25519 AAAAtest`)
			})

			It("returns no warnings and no error", func() {
				warnings, err := kc.ValidateSemantics()
				Expect(err).ToNot(HaveOccurred())
				Expect(warnings).To(BeEmpty())
			})
		})

		Context("when ssh_hardening: true and a user has both key and password", func() {
			BeforeEach(func() {
				kc = newKC(`#cloud-config
install:
  ssh_hardening: true
users:
  - name: kairos
    passwd: kairos
    ssh_authorized_keys:
      - ssh-ed25519 AAAAtest`)
			})

			It("warns about the unused password", func() {
				warnings, err := kc.ValidateSemantics()
				Expect(err).ToNot(HaveOccurred())
				Expect(warnings).To(HaveLen(1))
				Expect(warnings[0]).To(ContainSubstring(`"kairos"`))
				Expect(warnings[0]).To(ContainSubstring("password authentication"))
			})
		})

		Context("when ssh_hardening: true but no user has a key", func() {
			BeforeEach(func() {
				kc = newKC(`#cloud-config
install:
  ssh_hardening: true
users:
  - name: kairos
    passwd: kairos`)
			})

			It("errors because SSH would be unreachable", func() {
				_, err := kc.ValidateSemantics()
				Expect(err).To(MatchError(ContainSubstring("ssh_authorized_keys")))
			})
		})
	})

	Context("GenerateSchema", func() {
		var url string
		var schema string
		var err error

		type TestSchema struct {
			Key interface{} `json:"key,omitempty" required:"true"`
		}

		JustBeforeEach(func() {
			schema, err = GenerateSchema(TestSchema{}, url)
			Expect(err).ToNot(HaveOccurred())
		})

		It("does not include the $schema key by default", func() {
			Expect(strings.Contains(schema, `$schema`)).To(BeFalse())
		})

		It("can use any type of schma", func() {
			wants := `{
 "required": [
  "key"
 ],
 "properties": {
  "key": {}
 },
 "type": "object"
}`
			Expect(schema).To(Equal(wants))
		})

		Context("with a URL", func() {
			BeforeEach(func() {
				url = "http://foobar"
			})

			It("appends the $schema key", func() {
				Expect(strings.Contains(schema, `$schema": "http://foobar"`)).To(BeTrue())
			})
		})

	})
})
