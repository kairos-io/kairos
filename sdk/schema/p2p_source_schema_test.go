package schema_test

import (
	. "github.com/kairos-io/kairos/v4/sdk/schema"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("p2p-source schema (EXPERIMENTAL)", func() {
	valid := func(body string) string {
		return "#cloud-config\nusers:\n  - name: kairos\n" + body
	}

	DescribeTable("accepts the block and p2p sources",
		func(body string) {
			config, err := NewConfigFromYAML(valid(body), RootSchema{})
			Expect(err).ToNot(HaveOccurred())
			Expect(config.IsValid()).To(BeTrue(), "%v", config.ValidationError)
		},
		Entry("every key", "p2p-source:\n  enabled: true\n  experimental: true\n  bind: 10.0.0.2\n  port: 42069\n  allow: [10.0.0.0/24]\n  upload-limit: 2048\n  max-peers: 8\n  disable-announce: true\n"),
		Entry("limits", "p2p-source:\n  experimental: true\n  max-image-size: 8192\n  timeout: 120\n"),
		Entry("the dangerous opt out", "p2p-source:\n  experimental: true\n  insecure: true\n"),
		Entry("only the opt in", "p2p-source:\n  experimental: true\n"),
		Entry("an upgrade source", "upgrade:\n  system:\n    source: p2p://10.0.0.2,10.0.0.3/active\n"),
		Entry("an install source", "install:\n  system:\n    source: p2p://active\n"),
		Entry("a reset source", "reset:\n  system:\n    source: p2p://10.0.0.2/active?sha256=abcd\n"),
		Entry("the kairos p2p network block next to it", "p2p:\n  network_token: abc\np2p-source:\n  experimental: true\n"),
	)

	DescribeTable("rejects what the agent does not read",
		func(body string) {
			config, err := NewConfigFromYAML(valid(body), RootSchema{})
			Expect(err).ToNot(HaveOccurred())
			Expect(config.IsValid()).To(BeFalse())
		},
		Entry("an unknown key", "p2p-source:\n  experimental: true\n  skip-verify: true\n"),
		Entry("a typo", "p2p-source:\n  experimentl: true\n"),
		Entry("a scalar", "p2p-source: yes\n"),
		Entry("a port out of range", "p2p-source:\n  port: 70000\n"),
		Entry("a negative limit", "p2p-source:\n  upload-limit: -5\n"),
		Entry("keys are not a thing here", "p2p-source:\n  trusted-keys: [ed25519:AAAA]\n"),
		Entry("a signing key file is not a thing here", "p2p-source:\n  signing-key-file: /oem/k\n"),
		Entry("a negative timeout", "p2p-source:\n  timeout: -1\n"),
		Entry("a negative size", "p2p-source:\n  max-image-size: -1\n"),
		Entry("a string port", "p2p-source:\n  port: abc\n"),
	)
})
