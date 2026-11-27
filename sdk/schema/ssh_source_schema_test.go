package schema_test

import (
	. "github.com/kairos-io/kairos/v4/sdk/schema"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ssh source schema", func() {
	valid := func(body string) string {
		return "#cloud-config\nusers:\n  - name: kairos\n" + body
	}

	DescribeTable("accepts ssh sources",
		func(body string) {
			config, err := NewConfigFromYAML(valid(body), RootSchema{})
			Expect(err).ToNot(HaveOccurred())
			Expect(config.IsValid()).To(BeTrue(), "%v", config.ValidationError)
		},
		Entry("upgrade with a known hosts file", "upgrade:\n  system:\n    source: ssh://root@10.0.0.5/active\n  ssh-source:\n    identity-file: /oem/id\n    known-hosts-file: /oem/kh\n"),
		Entry("upgrade with fingerprints", "upgrade:\n  ssh-source:\n    identity-file: /oem/id\n    host-key-fingerprints:\n      - SHA256:abc\n"),
		Entry("root block", "ssh-source:\n  user: fleet\n  identity-file: /oem/id\n"),
		Entry("reset", "reset:\n  system:\n    source: ssh://node1/active\n  ssh-source:\n    identity-file: /oem/id\n"),
		Entry("only a user", "upgrade:\n  ssh-source:\n    user: fleet\n"),
		Entry("install", "install:\n  system:\n    source: ssh://root@10.0.0.5:2222\n  ssh-source:\n    identity-file: /oem/id\n    known-hosts-file: /oem/kh\n"),
	)

	DescribeTable("rejects what the agent does not read",
		func(body string) {
			config, err := NewConfigFromYAML(valid(body), RootSchema{})
			Expect(err).ToNot(HaveOccurred())
			Expect(config.IsValid()).To(BeFalse())
		},
		Entry("an inline key", "upgrade:\n  ssh-source:\n    identity: secret\n"),
		Entry("a switch to skip host key checks", "upgrade:\n  ssh-source:\n    insecure: true\n"),
		Entry("an unknown key at the root block", "ssh-source:\n  insecure: true\n"),
		Entry("a scalar", "upgrade:\n  ssh-source: yes\n"),
		Entry("a numeric path", "upgrade:\n  ssh-source:\n    identity-file: 5\n"),
	)
})
