package schema_test

import (
	"strings"

	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
	"gopkg.in/yaml.v3"

	. "github.com/kairos-io/kairos/v4/sdk/schema"
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

	// RootSchema is what `kairos-agent print-schema` publishes, and
	// agent/pkg/config.scan unmarshals the very same cloud-config into
	// sdk/types/config.Config *before* it validates. So a key the schema
	// describes with a shape Config cannot hold is not a documentation
	// slip: it aborts the scan, and every command that scans config with
	// it. These specs pin the two structs to each other.
	Context("top-level keys the agent reads", func() {
		validates := func(y string) (bool, string) {
			kc, err := NewConfigFromYAML("#cloud-config\nusers:\n  - name: kairos\n"+y, RootSchema{})
			Expect(err).ToNot(HaveOccurred())
			if kc.IsValid() {
				return true, ""
			}
			return false, kc.ValidationError.Error()
		}

		// The runtime holds `options` in a map[string]string. A sequence
		// makes yaml.Unmarshal fail outright, so the schema must not be
		// the thing that tells people to write one.
		It("accepts the options mapping the runtime reads, and rejects a sequence", func() {
			var c sdkConfig.Config
			Expect(yaml.Unmarshal([]byte("options:\n  foo: bar\n"), &c)).To(Succeed())
			Expect(c.Options).To(Equal(map[string]string{"foo": "bar"}))
			Expect(yaml.Unmarshal([]byte("options:\n  - foo\n"), &sdkConfig.Config{})).
				To(MatchError(ContainSubstring("cannot unmarshal !!seq into map[string]string")))

			ok, _ := validates("options:\n  foo: bar\n")
			Expect(ok).To(BeTrue(), "the only shape Config can hold must validate")

			ok, why := validates("options:\n  - foo\n")
			Expect(ok).To(BeFalse(), "a sequence breaks the scan, so it must not validate")
			Expect(why).To(ContainSubstring("/options"))
		})

		// Declared-property specs pass vacuously while the schema accepts
		// unknown keys, so each of these asserts on a wrong *type* too:
		// type checks only fire on properties the schema declares.
		DescribeTable("describes the key, so print-schema completes it",
			func(good, badType string) {
				ok, why := validates(good)
				Expect(ok).To(BeTrue(), why)

				ok, _ = validates(badType)
				Expect(ok).To(BeFalse(),
					"a wrong-typed value was accepted, so the key is not a declared property")
			},
			Entry("logs", "logs:\n  journal:\n    - kairos-agent\n  files:\n    - /var/log/foo\n", "logs: 5\n"),
			Entry("bind-pcrs", "bind-pcrs:\n  - \"11\"\n", "bind-pcrs: 11\n"),
			Entry("bind-public-pcrs", "bind-public-pcrs:\n  - \"7\"\n", "bind-public-pcrs: 7\n"),
			Entry("options", "options:\n  foo: bar\n", "options: 5\n"),
		)
	})
})
