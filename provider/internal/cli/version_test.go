package cli_test

import (
	. "github.com/kairos-io/kairos/v4/provider/internal/cli"
	"github.com/kairos-io/kairos/v4/internal/version"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// The provider ships in the same image as the multi-call kairos binary and is
// built from the same tree, so "which provider is this" and "which kairos is
// this" have to have one answer. Before the monorepo absorbed this code the
// provider carried its own version string, stamped by its own release
// pipeline; keeping that would let the two drift apart in a single image.
var _ = Describe("Reported version", func() {
	It("is the monorepo version the rest of the binaries report", func() {
		Expect(NewApp().Version).To(Equal(version.Version))
	})

	It("is not a hardcoded placeholder", func() {
		Expect(NewApp().Version).NotTo(Equal("0.0.0"))
	})
})

// The provider's commands are reached as `kairos provider <cmd>`, so that is
// what usage text has to name. Calling itself "kairos" would print examples
// like `kairos role list`, which the multi-call dispatcher rejects as an
// unknown sub-tool.
var _ = Describe("Usage text", func() {
	It("names the command shape users actually type", func() {
		Expect(NewApp().Name).To(Equal("kairos provider"))
	})

	It("uses that name in the examples of subcommands that build their own", func() {
		Expect(RegisterCMD("kairos provider").UsageText).To(HavePrefix("kairos provider register"))
	})
})
