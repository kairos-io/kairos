package store_test

import (
	"github.com/kairos-io/AuroraBoot/pkg/store"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("command type constants", func() {
	It("includes extension", func() {
		Expect(store.CmdExtension).To(Equal("extension"))
	})
})

var _ = Describe("command phases", func() {
	It("accepts every known phase", func() {
		for _, p := range []string{store.CommandPending, store.CommandDelivered, store.CommandRunning,
			store.CommandCompleted, store.CommandFailed, store.CommandExpired, store.CommandCanceled} {
			Expect(store.IsValidCommandPhase(p)).To(BeTrue(), p)
		}
	})

	It("rejects unknown phases", func() {
		for _, p := range []string{"", "Bogus", "running", "COMPLETED"} {
			Expect(store.IsValidCommandPhase(p)).To(BeFalse(), p)
		}
	})

	It("lists exactly the terminal phases", func() {
		for _, p := range store.TerminalCommandPhases {
			Expect(store.IsTerminalCommandPhase(p)).To(BeTrue(), p)
		}
		Expect(store.TerminalCommandPhases).To(HaveLen(4))
	})
})
