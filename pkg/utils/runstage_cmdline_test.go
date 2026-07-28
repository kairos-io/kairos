/*
Copyright © 2026 SUSE LLC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package utils_test

import (
	"github.com/kairos-io/kairos-agent/v2/pkg/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"
)

// These specs exercise the cmdline-parsing helpers that kairos-agent inherits
// from kairos-sdk (PR #812). They mirror the equivalent immucore tests
// (kairos-io/immucore PR #600) so both consumers stay wired to the same SDK
// parser and behave the same across every supported cmdline form.
var _ = Describe("Kairos cmdline parsing (kairos-sdk integration)", func() {
	Context("KairosConfigURIFromString", func() {
		DescribeTable("resolves the config URI",
			func(cmdline, expected string) {
				Expect(utils.KairosConfigURIFromString(cmdline)).To(Equal(expected))
			},
			Entry("modern kairos.config_url=URL",
				"kairos.config_url=https://x.example/cfg.yaml", "https://x.example/cfg.yaml"),
			Entry("legacy bare cos.setup=URL",
				"cos.setup=https://x.example/legacy.yaml", "https://x.example/legacy.yaml"),
			Entry("legacy bare cos.setup=file path",
				"cos.setup=/oem/99_custom.yaml", "/oem/99_custom.yaml"),
			Entry("cos.setup= with key=value routes into nested config, not URI",
				"cos.setup=install.auto=true", ""),
			Entry("last cos.setup= wins",
				"cos.setup=/first.yaml cos.setup=/second.yaml", "/second.yaml"),
			Entry("quoted URL with spaces",
				`kairos.config_url="https://x.example/path with space.yaml"`, "https://x.example/path with space.yaml"),
			Entry("empty payload is ignored",
				"kairos.config_url= root=LABEL=X", ""),
			Entry("no Kairos-owned tokens",
				"root=LABEL=X quiet console=ttyS0", ""),
			Entry("kairos.config=key=value alone yields no URI",
				"kairos.config=install.auto=true kairos.config=users.0.name=kairos", ""),
			Entry("mix of nested kairos.config= and kairos.config_url= — URI still resolved",
				"kairos.config=install.auto=true kairos.config_url=https://x.example/cfg.yaml",
				"https://x.example/cfg.yaml"),
			Entry("empty cmdline",
				"", ""),
		)
	})

	Context("SDKDotNotationModifier", func() {
		It("converts generic dot-nested tokens to YAML", func() {
			out, err := utils.SDKDotNotationModifier([]byte("foo.bar=baz nested.deep.key=1"))
			Expect(err).ToNot(HaveOccurred())
			var m map[string]interface{}
			Expect(yaml.Unmarshal(out, &m)).To(Succeed())
			Expect(m).To(HaveKey("foo"))
			Expect(m["foo"]).To(HaveKeyWithValue("bar", "baz"))
			Expect(m).To(HaveKey("nested"))
		})
		It("skips Kairos-owned prefixes so they are not double-processed", func() {
			// The whole point of routing through the SDK: kairos.config=,
			// kairos.config_url= and cos.setup= are handled elsewhere and MUST
			// NOT leak into the generic dot-nested output as spurious
			// top-level keys.
			out, err := utils.SDKDotNotationModifier([]byte(
				"foo.bar=baz kairos.config=install.auto=true kairos.config_url=http://x/y cos.setup=/z",
			))
			Expect(err).ToNot(HaveOccurred())
			var m map[string]interface{}
			Expect(yaml.Unmarshal(out, &m)).To(Succeed())
			Expect(m).To(HaveKey("foo"))
			Expect(m).ToNot(HaveKey("kairos"))
			Expect(m).ToNot(HaveKey("cos"))
			Expect(m).ToNot(HaveKey("config_url"))
		})
		It("returns valid YAML for empty input", func() {
			out, err := utils.SDKDotNotationModifier([]byte(""))
			Expect(err).ToNot(HaveOccurred())
			var m map[string]interface{}
			Expect(yaml.Unmarshal(out, &m)).To(Succeed())
		})
	})
})
