package phonehome

import (
	"github.com/kairos-io/kairos-sdk/state"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// The boot-state detection itself (UKI + non-UKI /proc/cmdline classification,
// including the autoreset case) is owned and tested by kairos-sdk/state. Here we
// only cover the agent's responsibility: translating the SDK vocabulary to the
// short fleet vocabulary, and — critically — never reporting an undeterminable
// boot state as "active".
var _ = DescribeTable("mapBootState",
	func(in state.Boot, want string) { Expect(mapBootState(in)).To(Equal(want)) },
	Entry("active", state.Active, bootStateActive),
	Entry("passive", state.Passive, bootStatePassive),
	Entry("recovery", state.Recovery, bootStateRecovery),
	Entry("autoreset", state.AutoReset, bootStateAutoReset),
	Entry("livecd", state.LiveCD, bootStateLiveCD),
	Entry("unknown maps to unknown, not active", state.Unknown, bootStateUnknown),
	Entry("an unrecognised value maps to unknown, not active", state.Boot("something_new_boot"), bootStateUnknown),
)
