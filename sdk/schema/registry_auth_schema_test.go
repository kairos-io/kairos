package schema_test

import (
	. "github.com/kairos-io/kairos/v4/sdk/schema"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const registryAuthSchemaUser = "users:\n  - name: kairos\n"

var _ = Describe("Registry auth schema", func() {
	valid := func(body string) string {
		return "#cloud-config\n" + registryAuthSchemaUser + body
	}

	DescribeTable("accepts valid registry auth configuration",
		func(body string) {
			config, err := NewConfigFromYAML(valid(body), RootSchema{})
			Expect(err).ToNot(HaveOccurred())
			Expect(config.IsValid()).To(BeTrue(), config.ValidationError)
		},
		Entry("install null", "install:\n  registry-auth: null\n"),
		Entry("install empty object", "install:\n  registry-auth: {}\n"),
		Entry("install file", "install:\n  registry-auth:\n    file: /run/secrets/registry-auth.yaml\n"),
		Entry("upgrade file", "upgrade:\n  registry-auth:\n    file: /run/secrets/registry-auth.yaml\n"),
		Entry("install basic auth", "install:\n  registry-auth:\n    username: user\n    password: pass\n"),
		Entry("install auth token", "install:\n  registry-auth:\n    auth: dXNlcjpwYXNz\n"),
		Entry("upgrade null", "upgrade:\n  registry-auth: null\n"),
		Entry("upgrade empty object", "upgrade:\n  registry-auth: {}\n"),
		Entry("upgrade identity token", "upgrade:\n  registry-auth:\n    identity-token: identity\n"),
		Entry("upgrade registry token", "upgrade:\n  registry-auth:\n    registry-token: token\n"),
		Entry("unrelated upgrade fields", "upgrade:\n  unrelated-setting: accepted\n"),
	)

	DescribeTable("rejects invalid registry auth configuration",
		func(body string) {
			config, err := NewConfigFromYAML(valid(body), RootSchema{})
			Expect(err).ToNot(HaveOccurred())
			Expect(config.IsValid()).To(BeFalse())
		},
		Entry("scalar value", "install:\n  registry-auth: password\n"),
		Entry("numeric file", "install:\n  registry-auth:\n    file: 42\n"),
		Entry("numeric password", "install:\n  registry-auth:\n    username: user\n    password: 42\n"),
		Entry("numeric username", "upgrade:\n  registry-auth:\n    username: 42\n"),
	)
})
