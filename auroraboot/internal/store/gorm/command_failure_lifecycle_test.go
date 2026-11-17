package gorm_test

import (
	"context"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	gormstore "github.com/kairos-io/AuroraBoot/internal/store/gorm"
	"github.com/kairos-io/AuroraBoot/pkg/store"
)

// An upgrade or reset that fails before the reboot never brings the node back
// with a new boot state, so the re-register resolvers never settle the pending
// state it set. The failure report is the only signal left, and the store
// applies it in the same write as the command row.
var _ = Describe("Gorm Store lifecycle after a failed command", func() {
	var (
		s   *gormstore.Store
		ctx context.Context
	)

	BeforeEach(func() {
		var err error
		s, err = gormstore.New(filepath.Join(GinkgoT().TempDir(), "failure.db"))
		Expect(err).NotTo(HaveOccurred())
		ctx = context.Background()
	})

	AfterEach(func() { Expect(s.Close()).To(Succeed()) })

	register := func(machineID string) *store.ManagedNode {
		n := &store.ManagedNode{MachineID: machineID}
		Expect(s.Register(ctx, n)).To(Succeed())
		return n
	}

	node := func(id string) *store.ManagedNode {
		got, err := s.NodeGetByID(ctx, id)
		Expect(err).NotTo(HaveOccurred())
		return got
	}

	// issue queues one command for n the way the command handlers do, so a
	// reset or upgrade also marks the node pending.
	issue := func(n *store.ManagedNode, command string) string {
		cmd := &store.NodeCommand{ManagedNodeID: n.ID, Command: command}
		Expect(s.CreateBatch(ctx, []*store.NodeCommand{cmd})).To(Succeed())
		return cmd.ID
	}

	// The REST status endpoint uses UpdateStatus for admin callers and
	// UpdateStatusForNode for agents, and the WebSocket path uses
	// UpdateStatusForNode. Both have to settle the lifecycle.
	reporters := []struct {
		name   string
		report func(n *store.ManagedNode, id, phase string) error
	}{
		{"UpdateStatus", func(_ *store.ManagedNode, id, phase string) error {
			return s.UpdateStatus(ctx, id, phase, "boom")
		}},
		{"UpdateStatusForNode", func(n *store.ManagedNode, id, phase string) error {
			return s.UpdateStatusForNode(ctx, id, n.ID, phase, "boom")
		}},
	}

	for _, r := range reporters {
		report := r.report
		Describe(r.name, func() {
			It("marks the pending upgrade of the node failed", func() {
				n := register("m1")
				id := issue(n, store.CmdUpgrade)
				requested := *node(n.ID).UpgradeRequestedAt

				Expect(report(n, id, store.CommandFailed)).To(Succeed())

				got := node(n.ID)
				Expect(got.UpgradeState).To(Equal(store.UpgradeStateFailed))
				Expect(*got.UpgradeRequestedAt).To(BeTemporally("==", requested))
				Expect(got.LastUpgrade).To(BeNil())
			})

			It("marks the pending reset of the node failed", func() {
				n := register("m1")
				id := issue(n, store.CmdReset)

				Expect(report(n, id, store.CommandFailed)).To(Succeed())

				got := node(n.ID)
				Expect(got.ResetState).To(Equal(store.ResetStateFailed))
				Expect(got.LastReset).To(BeNil())
			})
		})
	}

	It("UpdateStatusForNode leaves the node alone when the command is not its own", func() {
		n1, n2 := register("m1"), register("m2")
		id := issue(n1, store.CmdUpgrade)

		Expect(s.UpdateStatusForNode(ctx, id, n2.ID, store.CommandFailed, "boom")).
			To(MatchError(store.ErrCommandNotFound))

		Expect(node(n1.ID).UpgradeState).To(Equal(store.UpgradeStatePending))
	})

	It("leaves the lifecycle alone when the upgrade completes", func() {
		n := register("m1")
		id := issue(n, store.CmdUpgrade)

		Expect(s.UpdateStatus(ctx, id, store.CommandCompleted, "")).To(Succeed())

		Expect(node(n.ID).UpgradeState).To(Equal(store.UpgradeStatePending))
	})

	It("leaves the lifecycle alone when a command that does not reboot the node fails", func() {
		n := register("m1")
		issue(n, store.CmdUpgrade)
		other := issue(n, store.CmdExec)

		Expect(s.UpdateStatus(ctx, other, store.CommandFailed, "boom")).To(Succeed())

		Expect(node(n.ID).UpgradeState).To(Equal(store.UpgradeStatePending))
	})

	It("keeps the pending upgrade of a node that has another live upgrade", func() {
		n := register("m1")
		failed := issue(n, store.CmdUpgrade)
		live := issue(n, store.CmdUpgrade)
		Expect(s.UpdateStatus(ctx, live, store.CommandRunning, "")).To(Succeed())

		Expect(s.UpdateStatus(ctx, failed, store.CommandFailed, "boom")).To(Succeed())

		Expect(node(n.ID).UpgradeState).To(Equal(store.UpgradeStatePending))
	})

	// The agent completes an upgrade once it has scheduled the reboot, so the
	// pending state belongs to that upgrade until the node comes back.
	It("keeps the pending upgrade of a node whose earlier upgrade awaits its reboot", func() {
		n := register("m1")
		earlier := issue(n, store.CmdUpgrade)
		Expect(s.UpdateStatus(ctx, earlier, store.CommandCompleted, "")).To(Succeed())
		later := issue(n, store.CmdUpgrade)

		Expect(s.UpdateStatus(ctx, later, store.CommandFailed, "boom")).To(Succeed())

		Expect(node(n.ID).UpgradeState).To(Equal(store.UpgradeStatePending))
	})

	// in-progress means the node already rebooted into the reset, and the
	// re-register resolver settles it from the next boot state.
	It("leaves a reset that is already in progress alone", func() {
		n := register("m1")
		id := issue(n, store.CmdReset)
		_, err := s.AdvanceReset(ctx, n.ID, []string{store.ResetStatePending}, store.ResetStateInProgress, false)
		Expect(err).NotTo(HaveOccurred())

		Expect(s.UpdateStatus(ctx, id, store.CommandFailed, "boom")).To(Succeed())

		Expect(node(n.ID).ResetState).To(Equal(store.ResetStateInProgress))
	})

	It("leaves an upgrade that is already done alone", func() {
		n := register("m1")
		id := issue(n, store.CmdUpgrade)
		_, err := s.AdvanceUpgrade(ctx, n.ID, []string{store.UpgradeStatePending}, store.UpgradeStateDone, true)
		Expect(err).NotTo(HaveOccurred())

		Expect(s.UpdateStatus(ctx, id, store.CommandFailed, "boom")).To(Succeed())

		Expect(node(n.ID).UpgradeState).To(Equal(store.UpgradeStateDone))
	})
})
