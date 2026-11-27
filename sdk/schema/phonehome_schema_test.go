package schema_test

import (
	. "github.com/kairos-io/kairos/v4/sdk/schema"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// The phonehome block used to be absent from RootSchema, so a misspelled key
// or a wrong type validated and only showed up once the node was running:
// a misspelled allowed_commands left the default command policy in place.
// See kairos-io/kairos#5728.
var _ = Describe("Phonehome block in the root schema", func() {
	validate := func(yaml string) *KConfig {
		config, err := NewConfigFromYAML(yaml, RootSchema{})
		Expect(err).ToNot(HaveOccurred())
		return config
	}

	It("accepts every key the agent reads", func() {
		config := validate(`#cloud-config
users:
- name: kairos
phonehome:
  url: https://auroraboot.example.com
  registration_token: abc123
  group: edge
  labels:
    site: lab
    rack: "3"
  heartbeat_interval: 30s
  reconnect_backoff: 1m30s
  artifact_download_retries: 3
  artifact_download_retry_interval: 500ms
  allowed_commands:
  - reboot
  - upgrade
  - upgrade-recovery
  - unregister
  - exec
  - reset
  - apply-cloud-config
  - extension`)

		Expect(config.IsValid()).To(BeTrue(), func() string { return config.ValidationError.Error() })
	})

	It("accepts an explicitly empty allowed_commands, which denies every command", func() {
		config := validate(`#cloud-config
users:
- name: kairos
phonehome:
  url: https://auroraboot.example.com
  allowed_commands: []`)

		Expect(config.IsValid()).To(BeTrue(), func() string { return config.ValidationError.Error() })
	})

	DescribeTable("rejects a key the agent does not read",
		func(key string) {
			config := validate(`#cloud-config
users:
- name: kairos
phonehome:
  url: https://auroraboot.example.com
  ` + key + `: x`)

			Expect(config.IsValid()).To(BeFalse())
			Expect(config.ValidationError.Error()).To(ContainSubstring("/phonehome"))
			Expect(config.ValidationError.Error()).To(ContainSubstring(key))
		},
		Entry("misspelled registration_token", "registraton_token"),
		Entry("misspelled allowed_commands", "alowed_commands"),
	)

	DescribeTable("rejects a value of the wrong type",
		func(body, path string) {
			config := validate(`#cloud-config
users:
- name: kairos
phonehome:
` + body)

			Expect(config.IsValid()).To(BeFalse())
			Expect(config.ValidationError.Error()).To(ContainSubstring(path))
		},
		Entry("url as a list", "  url:\n  - https://a.example.com", "/phonehome/url"),
		Entry("a duration that does not parse", "  heartbeat_interval: banana", "/phonehome/heartbeat_interval"),
		Entry("a duration as a bare number, which yaml.v3 does not decode", "  reconnect_backoff: 30", "/phonehome/reconnect_backoff"),
		Entry("allowed_commands as a string", "  allowed_commands: reboot", "/phonehome/allowed_commands"),
		Entry("an unknown command", "  allowed_commands:\n  - rebot", "/phonehome/allowed_commands/0"),
		Entry("negative download retries", "  artifact_download_retries: -1", "/phonehome/artifact_download_retries"),
		Entry("a label that is not a string", "  labels:\n    site:\n      nested: true", "/phonehome/labels/site"),
	)

	It("is part of the generated schema", func() {
		generated, err := GenerateSchema(RootSchema{}, "")
		Expect(err).ToNot(HaveOccurred())
		Expect(generated).To(ContainSubstring(`"phonehome"`))
		Expect(generated).To(ContainSubstring(`"allowed_commands"`))
	})
})
