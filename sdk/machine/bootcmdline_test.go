package machine_test

import (
	"os"

	. "github.com/kairos-io/kairos-sdk/machine"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("BootCMDLine", func() {
	writeCmdline := func(contents string) string {
		f, err := os.CreateTemp("", "cmdline")
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() { _ = os.Remove(f.Name()) })
		Expect(os.WriteFile(f.Name(), []byte(contents), 0o644)).To(Succeed())
		return f.Name()
	}

	Context("KairosCmdlineYAML", func() {
		It("returns nil when no owned stanzas are present", func() {
			path := writeCmdline("root=LABEL=X rd.immucore.debug\n")
			y, err := KairosCmdlineYAML(path)
			Expect(err).ToNot(HaveOccurred())
			Expect(y).To(BeNil())
		})

		It("parses a single kairos.config stanza", func() {
			y, err := KairosCmdlineYAMLFromString(`root=X kairos.config=hostname=box`)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(y)).To(ContainSubstring("hostname: box"))
		})

		It("builds nested maps from dot notation", func() {
			y, err := KairosCmdlineYAMLFromString(
				`kairos.config=install.auto=true kairos.config=install.reboot=false`,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(y)).To(ContainSubstring("install:"))
			Expect(string(y)).To(ContainSubstring("auto: \"true\""))
			Expect(string(y)).To(ContainSubstring("reboot: \"false\""))
		})

		It("builds lists from numeric segments", func() {
			y, err := KairosCmdlineYAMLFromString(
				`kairos.config=users.0.name=kairos kairos.config=users.0.passwd=k kairos.config=users.1.name=foo`,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(y)).To(ContainSubstring("users:"))
			Expect(string(y)).To(ContainSubstring("- name: kairos"))
			Expect(string(y)).To(ContainSubstring("passwd: k"))
			Expect(string(y)).To(ContainSubstring("- name: foo"))
		})

		It("supports quoted values with spaces", func() {
			y, err := KairosCmdlineYAMLFromString(`kairos.config=hostname="my box"`)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(y)).To(ContainSubstring("hostname: my box"))
		})

		It("drops empty payloads", func() {
			y, err := KairosCmdlineYAMLFromString(`kairos.config= kairos.config=hostname=box`)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(y)).To(ContainSubstring("hostname: box"))
			Expect(string(y)).ToNot(ContainSubstring("kairos"))
		})

		It("kairos.config_url sets top-level config_url and keeps '=' in the URL intact", func() {
			y, err := KairosCmdlineYAMLFromString(
				`kairos.config_url=https://example.com/x.yaml?token=abc=def`,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(y)).To(ContainSubstring("config_url: https://example.com/x.yaml?token=abc=def"))
		})

		It("cos.setup with a bare URI routes into config_url (legacy)", func() {
			y, err := KairosCmdlineYAMLFromString(`cos.setup=https://example.com/legacy.yaml`)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(y)).To(ContainSubstring("config_url: https://example.com/legacy.yaml"))
		})

		It("cos.setup with KEY=VALUE behaves like kairos.config (legacy)", func() {
			y, err := KairosCmdlineYAMLFromString(`cos.setup=hostname=box`)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(y)).To(ContainSubstring("hostname: box"))
		})

		It("merges all three prefixes into one document", func() {
			y, err := KairosCmdlineYAMLFromString(
				`kairos.config=hostname=box kairos.config_url=https://a/x.yaml cos.setup=install.auto=true`,
			)
			Expect(err).ToNot(HaveOccurred())
			out := string(y)
			Expect(out).To(ContainSubstring("hostname: box"))
			Expect(out).To(ContainSubstring("config_url: https://a/x.yaml"))
			Expect(out).To(ContainSubstring("install:"))
			Expect(out).To(ContainSubstring("auto: \"true\""))
		})

		It("later occurrences of the same key win", func() {
			y, err := KairosCmdlineYAMLFromString(
				`kairos.config=hostname=a kairos.config=hostname=b`,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(y)).To(ContainSubstring("hostname: b"))
			Expect(string(y)).ToNot(ContainSubstring("hostname: a"))
		})
	})

	Context("DotToYAML", func() {
		It("parses generic KEY=VALUE cmdline into nested YAML", func() {
			path := writeCmdline(`config_url="foo bar" baz.bar=""`)
			b, err := DotToYAML(path)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(b)).To(Equal("baz:\n    bar: \"\"\nconfig_url: foo bar\n"), string(b))
		})

		It("works if cmdline contains a dash or underscore", func() {
			path := writeCmdline(`config-url="foo bar" ba_z.bar=""`)
			_, err := DotToYAML(path)
			Expect(err).ToNot(HaveOccurred())
		})

		It("skips every Kairos-owned prefix so payload does not leak", func() {
			path := writeCmdline(
				`root=LABEL=X kairos.config=hostname=box kairos.config_url=https://a/x.yaml cos.setup=/oem/50-extra.yaml foo=bar`,
			)
			b, err := DotToYAML(path)
			Expect(err).ToNot(HaveOccurred())
			out := string(b)
			Expect(out).ToNot(ContainSubstring("kairos:"))
			Expect(out).ToNot(ContainSubstring("cos:"))
			Expect(out).ToNot(ContainSubstring("hostname=box"))
			Expect(out).ToNot(ContainSubstring("50-extra.yaml"))
			Expect(out).To(ContainSubstring("foo: bar"))
		})
	})
})
