package state

import (
	"github.com/kairos-io/kairos-sdk/state"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("boot state to extension sub-directory", func() {
	DescribeTable("resolves the sub-directory",
		func(boot state.Boot, expected string, known bool) {
			subDir, ok := bootStateToExtensionSubDir(boot)
			Expect(ok).To(Equal(known))
			Expect(subDir).To(Equal(expected))
		},
		Entry("active", state.Active, "active", true),
		Entry("passive", state.Passive, "passive", true),
		Entry("recovery", state.Recovery, "recovery", true),
		Entry("autoreset runs the recovery image", state.AutoReset, "recovery", true),
		Entry("livecd installs nothing", state.LiveCD, "", false),
		Entry("unknown installs nothing", state.Unknown, "", false),
	)
})
