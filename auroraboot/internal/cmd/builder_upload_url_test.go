package cmd_test

import (
	cmdpkg "github.com/kairos-io/AuroraBoot/internal/cmd"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ResolveBuilderUploadURL", Label("cmd", "builder-upload-url"), func() {
	When("--builder-upload-url is set", func() {
		It("returns it verbatim", func() {
			Expect(cmdpkg.ResolveBuilderUploadURL(
				"http://ab.kairos-operator.svc.cluster.local:80",
				"https://auroraboot.example.com",
			)).To(Equal("http://ab.kairos-operator.svc.cluster.local:80"))
		})
	})

	When("--builder-upload-url is empty", func() {
		It("falls back to the public --url", func() {
			Expect(cmdpkg.ResolveBuilderUploadURL(
				"",
				"https://auroraboot.example.com",
			)).To(Equal("https://auroraboot.example.com"))
		})
	})

	When("both are empty", func() {
		It("returns an empty string", func() {
			Expect(cmdpkg.ResolveBuilderUploadURL("", "")).To(BeEmpty())
		})
	})
})
