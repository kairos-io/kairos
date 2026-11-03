package machine_test

import (
	"os"

	. "github.com/kairos-io/kairos/v4/sdk/machine"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"
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

		It("skips a stanza whose list index is above 1024", func() {
			y, err := KairosCmdlineYAMLFromString(
				`kairos.config=a.1025=x kairos.config=b.1024=y kairos.config=hostname=box`,
			)
			Expect(err).ToNot(HaveOccurred())
			got := map[string]interface{}{}
			Expect(yaml.Unmarshal(y, &got)).To(Succeed())
			Expect(got).ToNot(HaveKey("a"))
			Expect(got).To(HaveKeyWithValue("hostname", "box"))
			Expect(got["b"]).To(HaveLen(1025))
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

	Context("DotStringToYAMLBestEffort", func() {
		It("returns the tokens that converted and reports the skipped ones", func() {
			var skipped error
			b, err := DotStringToYAMLBestEffort("8250.nr_uarts=0 foo.bar=baz", func(e error) { skipped = e })
			Expect(err).ToNot(HaveOccurred())
			Expect(skipped).To(HaveOccurred())
			Expect(string(b)).To(ContainSubstring("bar: baz"))
			Expect(string(b)).ToNot(ContainSubstring("nr_uarts"))
		})

		It("returns the error when no token converted", func() {
			called := false
			_, err := DotStringToYAMLBestEffort("8250.nr_uarts=0", func(error) { called = true })
			Expect(err).To(HaveOccurred())
			Expect(called).To(BeFalse())
		})

		It("leaves DotStringToYAML returning partial output and the error", func() {
			b, err := DotStringToYAML("8250.nr_uarts=0 foo.bar=baz")
			Expect(err).To(HaveOccurred())
			Expect(string(b)).To(ContainSubstring("bar: baz"))
		})
	})
	Context("cmdline splitting", func() {
		parse := func(y []byte) map[string]interface{} {
			var m map[string]interface{}
			Expect(yaml.Unmarshal(y, &m)).To(Succeed())
			return m
		}

		It("keeps a backslash in a raw kairos.config value", func() {
			y, err := KairosCmdlineYAMLFromString(`kairos.config=users.0.passwd=ab\cd`)
			Expect(err).ToNot(HaveOccurred())
			users := parse(y)["users"].([]interface{})
			Expect(users[0].(map[string]interface{})["passwd"]).To(Equal(`ab\cd`))
		})

		It("keeps the apostrophe token and every later token, including kairos.config_url", func() {
			y, err := KairosCmdlineYAMLFromString(
				`kairos.config=hostname=before kairos.config=users.0.passwd=it's kairos.config_url=http://example.com/c.yaml`,
			)
			Expect(err).ToNot(HaveOccurred())
			m := parse(y)
			Expect(m["hostname"]).To(Equal("before"))
			users := m["users"].([]interface{})
			Expect(users[0].(map[string]interface{})["passwd"]).To(Equal("it's"))
			Expect(m["config_url"]).To(Equal("http://example.com/c.yaml"))
		})

		It("keeps the only stanza when it has a lone apostrophe", func() {
			y, err := KairosCmdlineYAMLFromString(`kairos.config=users.0.passwd=it's`)
			Expect(err).ToNot(HaveOccurred())
			Expect(y).ToNot(BeNil())
			users := parse(y)["users"].([]interface{})
			Expect(users[0].(map[string]interface{})["passwd"]).To(Equal("it's"))
		})

		It("keeps every stanza after an unmatched apostrophe", func() {
			y, err := KairosCmdlineYAMLFromString(
				`kairos.config=users.0.passwd=it's kairos.config=install.auto=true kairos.config_url=http://example.com/c.yaml`,
			)
			Expect(err).ToNot(HaveOccurred())
			m := parse(y)
			users := m["users"].([]interface{})
			Expect(users[0].(map[string]interface{})["passwd"]).To(Equal("it's"))
			Expect(m["install"].(map[string]interface{})["auto"]).To(Equal("true"))
			Expect(m["config_url"]).To(Equal("http://example.com/c.yaml"))
		})

		It("groups a single-quoted value with spaces", func() {
			y, err := KairosCmdlineYAMLFromString(`kairos.config=hostname='my box' kairos.config=install.auto=true`)
			Expect(err).ToNot(HaveOccurred())
			m := parse(y)
			Expect(m["hostname"]).To(Equal("my box"))
			Expect(m["install"].(map[string]interface{})["auto"]).To(Equal("true"))
		})

		It("keeps a backslash in the generic DotStringToYAML path", func() {
			y, err := DotStringToYAML(`foo.bar=ab\cd`)
			Expect(err).ToNot(HaveOccurred())
			Expect(parse(y)["foo"].(map[string]interface{})["bar"]).To(Equal(`ab\cd`))
		})

		It("keeps later tokens after a lone apostrophe in the generic DotStringToYAML path", func() {
			y, err := DotStringToYAML(`first.key=one foo.bar=it's last.key=two`)
			Expect(err).ToNot(HaveOccurred())
			m := parse(y)
			Expect(m["first"].(map[string]interface{})["key"]).To(Equal("one"))
			Expect(m["foo"].(map[string]interface{})["bar"]).To(Equal("it's"))
			Expect(m["last"].(map[string]interface{})["key"]).To(Equal("two"))
		})

		It("groups a single-quoted value with spaces in the generic DotStringToYAML path", func() {
			y, err := DotStringToYAML(`foo.bar='a b' other.key=x`)
			Expect(err).ToNot(HaveOccurred())
			m := parse(y)
			Expect(m["foo"].(map[string]interface{})["bar"]).To(Equal("a b"))
			Expect(m["other"].(map[string]interface{})["key"]).To(Equal("x"))
		})

		It("decodes a GRUB-escaped cmdline correctly", func() {
			y, err := KairosCmdlineYAMLFromString(
				`kairos.config=users.0.passwd=it\'s kairos.config=users.1.passwd=ab\\cd kairos.config_url=http://example.com/c.yaml`,
			)
			Expect(err).ToNot(HaveOccurred())
			m := parse(y)
			users := m["users"].([]interface{})
			Expect(users[0].(map[string]interface{})["passwd"]).To(Equal("it's"))
			Expect(users[1].(map[string]interface{})["passwd"]).To(Equal(`ab\cd`))
			Expect(m["config_url"]).To(Equal("http://example.com/c.yaml"))
		})
	})
})
