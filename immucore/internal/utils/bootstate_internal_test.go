package utils

import (
	"github.com/kairos-io/kairos-sdk/state"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("boot state to label", func() {
	DescribeTable("resolves the sysroot label",
		func(boot state.Boot, expected string) {
			Expect(bootStateToSysrootLabel(boot)).To(Equal(expected))
		},
		Entry("active", state.Active, "COS_ACTIVE"),
		Entry("passive", state.Passive, "COS_PASSIVE"),
		Entry("recovery", state.Recovery, "COS_SYSTEM"),
		Entry("autoreset boots the recovery image", state.AutoReset, "COS_SYSTEM"),
		Entry("livecd has no target", state.LiveCD, ""),
		Entry("unknown has no target", state.Unknown, ""),
	)

	DescribeTable("resolves the images label",
		func(boot state.Boot, expected string) {
			Expect(bootStateToImagesLabel(boot)).To(Equal(expected))
		},
		Entry("active", state.Active, "COS_STATE"),
		Entry("passive", state.Passive, "COS_STATE"),
		Entry("recovery", state.Recovery, "COS_RECOVERY"),
		Entry("autoreset boots the recovery image", state.AutoReset, "COS_RECOVERY"),
		Entry("livecd has no target", state.LiveCD, ""),
		Entry("unknown has no target", state.Unknown, ""),
	)
})
