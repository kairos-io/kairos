package cli_test

import (
	"strings"

	"github.com/kairos-io/kairos/v4/internal/version"
	. "github.com/kairos-io/kairos/v4/provider/internal/cli"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/urfave/cli/v2"
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

// Help text that names a command the user cannot run is worse than no example.
// These commands were reachable as `kairos <cmd>` when /usr/bin/kairos was this
// binary; after the multi-call rollout that shape is rejected by the dispatcher
// as an unknown sub-tool, and the way in is `kairos provider <cmd>`. Node UUIDs
// likewise moved to the agent.
//
// RegisterCMD and BridgeCMD interpolate the tool name and cannot drift. These
// two build their examples from literals, which is how they went stale, so
// pin them.
var _ = Describe("Command examples", func() {
	collect := func() string {
		var sb strings.Builder
		var walk func(cmds []*cli.Command)
		walk = func(cmds []*cli.Command) {
			for _, c := range cmds {
				sb.WriteString(c.UsageText + "\n" + c.Description + "\n")
				walk(c.Subcommands)
			}
		}
		walk(NewApp().Commands)
		return sb.String()
	}

	DescribeTable("does not tell users to run a command that no longer exists",
		func(stale string) {
			Expect(collect()).NotTo(ContainSubstring(stale))
		},
		Entry("get-kubeconfig without the provider token", "$ kairos get-kubeconfig"),
		Entry("role set without the provider token", "kairos role set"),
		Entry("uuid, which belongs to the agent now", "$ (node A) kairos uuid"),
	)

	It("shows the shape that works", func() {
		text := collect()
		Expect(text).To(ContainSubstring("kairos provider get-kubeconfig"))
		Expect(text).To(ContainSubstring("kairos provider role set"))
		Expect(text).To(ContainSubstring("kairos agent uuid"))
	})
})
