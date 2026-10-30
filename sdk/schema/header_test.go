package schema_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/kairos/v4/sdk/collector"
	"github.com/kairos-io/kairos/v4/sdk/schema"
)

var _ = Describe("cloud-config header", func() {
	// The loader and the validator used to answer this question differently,
	// so every case asserts both: a document the system applies has to
	// validate, and one it skips has to fail.
	assert := func(document string, expected bool) {
		Expect(schema.HasValidHeader(document)).To(Equal(expected), "schema.HasValidHeader")
		Expect(collector.HasValidHeader(document)).To(Equal(expected), "collector.HasValidHeader")

		config, err := schema.NewConfigFromYAML(document, schema.RootSchema{})
		Expect(err).ToNot(HaveOccurred())
		Expect(config.HasHeader()).To(Equal(expected), "KConfig.HasHeader")
	}

	DescribeTable("is recognised", assert,
		Entry("on the first line", "#cloud-config\nhostname: a\n", true),
		Entry("after a blank line", "\n#cloud-config\nhostname: a\n", true),
		Entry("after a comment", "# My cluster\n#cloud-config\nhostname: a\n", true),
		Entry("after a jinja declaration",
			"## template: jinja\n#cloud-config\nhostname: a\n", true),
		Entry("with trailing whitespace", "#cloud-config  \t\nhostname: a\n", true),
		Entry("with a carriage return", "#cloud-config\r\nhostname: a\r\n", true),
		Entry("on the ninth line",
			"#a\n#b\n#c\n#d\n#e\n#f\n#g\n#h\n#cloud-config\nhostname: a\n", true),
		Entry("as the legacy #kairos-config", "#kairos-config\nhostname: a\n", true),
		Entry("as the legacy #node-config", "#node-config\nhostname: a\n", true),
		Entry("alone, with no body", "#cloud-config\n", true),
	)

	DescribeTable("is not recognised", assert,
		Entry("when absent", "---\nhostname: a\n", false),
		Entry("when the document is empty", "", false),
		// A file whose first line merely starts with the header is a different
		// file. The loader skips it, so the validator has to fail it.
		Entry("when the line only starts with it",
			"#cloud-configuration\nhostname: a\n", false),
		Entry("when it is indented", "  #cloud-config\nhostname: a\n", false),
		Entry("when it carries a trailing comment",
			"#cloud-config # the cluster\nhostname: a\n", false),
		// Nine lines is where the loader stops looking.
		Entry("past the ninth line",
			"#a\n#b\n#c\n#d\n#e\n#f\n#g\n#h\n#i\n#cloud-config\nhostname: a\n", false),
		Entry("as a value rather than a header",
			"hostname: a\nwrite_files:\n  - content: \"#cloud-config\"\n", false),
	)

	It("accepts the documented HA example, which opens with a blank line", func() {
		// docs/installation/p2p.md, full example with KubeVIP and HA. The
		// fenced block starts with a blank line, which the system boots with
		// and the validator used to reject.
		document := "\n#cloud-config\n\ninstall:\n  auto: true\n  device: \"auto\"\n"
		Expect(schema.Validate(document)).To(Succeed())
	})
})
