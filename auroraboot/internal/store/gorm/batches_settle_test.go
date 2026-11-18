package gorm_test

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	gormstore "github.com/kairos-io/AuroraBoot/internal/store/gorm"
	"github.com/kairos-io/AuroraBoot/pkg/store"
)

var _ = Describe("Gorm batch settling", func() {
	var (
		s   *gormstore.Store
		ctx context.Context
	)

	BeforeEach(func() {
		var err error
		s, err = gormstore.New(filepath.Join(GinkgoT().TempDir(), "settle.db"))
		Expect(err).NotTo(HaveOccurred())
		ctx = context.Background()
	})

	AfterEach(func() { Expect(s.Close()).To(Succeed()) })

	// register adds n online nodes named host-0, host-1, and so on.
	register := func(n int) []*store.ManagedNode {
		nodes := make([]*store.ManagedNode, n)
		for i := range nodes {
			nodes[i] = &store.ManagedNode{MachineID: uuid.NewString(), Hostname: fmt.Sprintf("host-%d", i)}
			Expect(s.Register(ctx, nodes[i])).To(Succeed())
			Expect(s.UpdatePhase(ctx, nodes[i].ID, store.PhaseOnline)).To(Succeed())
		}
		return nodes
	}

	newBatch := func(nodes []*store.ManagedNode, command string, concurrency int, stopOnFailure bool) string {
		b := &store.CommandBatch{
			ID:                   uuid.NewString(),
			Command:              command,
			Concurrency:          concurrency,
			StopOnFailure:        stopOnFailure,
			RebootTimeoutSeconds: store.DefaultRebootTimeoutSeconds,
		}
		rows := make([]*store.BatchNode, len(nodes))
		for i, n := range nodes {
			rows[i] = &store.BatchNode{NodeID: n.ID, Position: i, Hostname: n.Hostname, State: store.BatchNodeWaiting}
		}
		Expect(s.BatchCreate(ctx, b, rows)).To(Succeed())
		return b.ID
	}

	advanceAt := func(batchID string, now time.Time) []*store.NodeCommand {
		cmds, err := s.BatchAdvance(ctx, batchID, now)
		Expect(err).NotTo(HaveOccurred())
		return cmds
	}

	advance := func(batchID string) []*store.NodeCommand { return advanceAt(batchID, time.Now()) }

	get := func(batchID string) *store.CommandBatch {
		b, err := s.BatchGet(ctx, batchID)
		Expect(err).NotTo(HaveOccurred())
		return b
	}

	booted := func(nodeID, bootState string, bootedAt *time.Time) []string {
		ids, err := s.BatchNodeBooted(ctx, nodeID, bootState, bootedAt, time.Now())
		Expect(err).NotTo(HaveOccurred())
		return ids
	}

	at := func(t time.Time) *time.Time { return &t }

	// rebooting starts a batch of command over nodes and brings node 0 to
	// rebooting, its command completed a minute ago. It returns the batch and
	// the row's RebootingSince.
	rebooting := func(nodes []*store.ManagedNode, command string, concurrency int, stopOnFailure bool) (string, time.Time) {
		id := newBatch(nodes, command, concurrency, stopOnFailure)
		cmds := advance(id)
		Expect(cmds).NotTo(BeEmpty())
		Expect(cmds[0].ManagedNodeID).To(Equal(nodes[0].ID))
		Expect(s.UpdateStatus(ctx, cmds[0].ID, store.CommandCompleted, "")).To(Succeed())
		since := time.Now().Add(-time.Minute).UTC().Truncate(time.Second)
		Expect(s.UnsafeDB().Model(&store.NodeCommand{}).Where("id = ?", cmds[0].ID).
			Update("completed_at", since).Error).To(Succeed())
		advance(id)
		Expect(get(id).Nodes[0].State).To(Equal(store.BatchNodeRebooting))
		return id, since
	}

	nodeByID := func(id string) *store.ManagedNode {
		n, err := s.NodeGetByID(ctx, id)
		Expect(err).NotTo(HaveOccurred())
		return n
	}

	// claim marks a command delivered, as pushing it to its agent does.
	claim := func(cmdID string) {
		claimed, err := s.ClaimForDelivery(ctx, cmdID)
		Expect(err).NotTo(HaveOccurred())
		Expect(claimed).To(BeTrue())
	}

	commandPhase := func(cmdID string) string {
		var cmd store.NodeCommand
		Expect(s.UnsafeDB().First(&cmd, "id = ?", cmdID).Error).To(Succeed())
		return cmd.Phase
	}

	heardFrom := func(nodeID string, t time.Time) {
		Expect(s.UnsafeDB().Model(&store.ManagedNode{}).Where("id = ?", nodeID).
			Update("last_heartbeat", t).Error).To(Succeed())
	}

	It("cancels the undelivered commands of started nodes when the batch is stopped by hand", func() {
		nodes := register(3)
		id := newBatch(nodes, store.CmdUpgrade, 0, true)
		cmds := advance(id)
		Expect(cmds).To(HaveLen(3))
		claim(cmds[0].ID)

		Expect(s.BatchStop(ctx, id, "stopped by the user", time.Now())).To(Succeed())

		b := get(id)
		Expect(b.Nodes[0].State).To(Equal(store.BatchNodeRunning))
		Expect(commandPhase(cmds[0].ID)).To(Equal(store.CommandDelivered))
		Expect(nodeByID(nodes[0].ID).UpgradeState).To(Equal(store.UpgradeStatePending))
		for i, row := range b.Nodes[1:] {
			Expect(row.State).To(Equal(store.BatchNodeCanceled))
			Expect(row.Message).To(Equal("batch stopped"))
			Expect(row.FinishedAt).NotTo(BeNil())
			Expect(commandPhase(cmds[i+1].ID)).To(Equal(store.CommandCanceled))
			Expect(nodeByID(row.NodeID).UpgradeState).To(BeEmpty())
		}
		Expect(b.FinishedAt).To(BeNil())
	})

	It("cancels the undelivered commands of started nodes when a failure stops the batch", func() {
		nodes := register(3)
		id := newBatch(nodes, store.CmdExec, 0, true)
		cmds := advance(id)
		Expect(cmds).To(HaveLen(3))
		claim(cmds[1].ID)
		Expect(s.UpdateStatus(ctx, cmds[0].ID, store.CommandFailed, "exit status 1")).To(Succeed())

		Expect(advance(id)).To(BeEmpty())

		b := get(id)
		Expect(b.Phase).To(Equal(store.BatchStopped))
		Expect(b.Nodes[1].State).To(Equal(store.BatchNodeRunning))
		Expect(b.Nodes[2].State).To(Equal(store.BatchNodeCanceled))
		Expect(b.Nodes[2].Message).To(Equal("batch stopped"))
		Expect(commandPhase(cmds[2].ID)).To(Equal(store.CommandCanceled))
		pending, err := s.GetPending(ctx, nodes[2].ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(pending).To(BeEmpty())
	})

	It("times out a started node that stopped sending heartbeats past its deadline", func() {
		nodes := register(2)
		id := newBatch(nodes, store.CmdUpgrade, 1, false)
		start := time.Now()
		cmds := advanceAt(id, start)
		Expect(get(id).Nodes[0].Deadline).To(HaveValue(BeTemporally("~", start.Add(10*time.Minute), time.Second)))
		heardFrom(nodes[0].ID, start)

		next := advanceAt(id, start.Add(11*time.Minute))

		b := get(id)
		Expect(b.Nodes[0].State).To(Equal(store.BatchNodeTimedOut))
		Expect(b.Nodes[0].Message).To(Equal("stopped sending heartbeats before it finished its command"))
		Expect(commandPhase(cmds[0].ID)).To(Equal(store.CommandCanceled))
		Expect(nodeByID(nodes[0].ID).UpgradeState).To(BeEmpty())
		Expect(next).To(HaveLen(1))
		Expect(next[0].ManagedNodeID).To(Equal(nodes[1].ID))
	})

	It("times out a started node that went offline with its command delivered and keeps the command", func() {
		nodes := register(1)
		id := newBatch(nodes, store.CmdExec, 1, false)
		start := time.Now()
		cmds := advanceAt(id, start)
		claim(cmds[0].ID)
		heardFrom(nodes[0].ID, start.Add(10*time.Minute))
		Expect(s.UpdatePhase(ctx, nodes[0].ID, store.PhaseOffline)).To(Succeed())

		advanceAt(id, start.Add(11*time.Minute))

		Expect(get(id).Nodes[0].State).To(Equal(store.BatchNodeTimedOut))
		Expect(commandPhase(cmds[0].ID)).To(Equal(store.CommandDelivered))
	})

	It("leaves a started node that still sends heartbeats running past its deadline", func() {
		nodes := register(1)
		id := newBatch(nodes, store.CmdExec, 1, false)
		start := time.Now()
		advanceAt(id, start)
		heardFrom(nodes[0].ID, start.Add(10*time.Minute))

		advanceAt(id, start.Add(11*time.Minute))

		Expect(get(id).Nodes[0].State).To(Equal(store.BatchNodeRunning))
	})

	It("keeps a postponed deadline when a command that completed before the server went down is reconciled", func() {
		nodes := register(1)
		id := newBatch(nodes, store.CmdUpgrade, 1, true)
		start := time.Now().Add(-time.Hour)
		cmds := advanceAt(id, start)
		Expect(s.UpdateStatus(ctx, cmds[0].ID, store.CommandCompleted, "")).To(Succeed())
		completed := start.Add(time.Minute)
		Expect(s.UnsafeDB().Model(&store.NodeCommand{}).Where("id = ?", cmds[0].ID).
			Update("completed_at", completed).Error).To(Succeed())
		now := time.Now()
		Expect(s.BatchPostponeExpiredDeadlines(ctx, now)).To(Succeed())

		advanceAt(id, now)

		b := get(id)
		Expect(b.Phase).To(Equal(store.BatchRunning))
		Expect(b.Nodes[0].State).To(Equal(store.BatchNodeRebooting))
		Expect(b.Nodes[0].RebootingSince).To(HaveValue(BeTemporally("~", completed, time.Second)))
		Expect(b.Nodes[0].Deadline).To(HaveValue(BeTemporally("~", now.Add(10*time.Minute), time.Second)))
	})

	It("postpones the deadline of a started node that passed while the server was down", func() {
		nodes := register(1)
		id := newBatch(nodes, store.CmdExec, 1, false)
		start := time.Now()
		advanceAt(id, start)
		now := start.Add(20 * time.Minute)

		Expect(s.BatchPostponeExpiredDeadlines(ctx, now)).To(Succeed())

		Expect(get(id).Nodes[0].Deadline).To(HaveValue(BeTemporally("~", now.Add(10*time.Minute), time.Second)))
	})

	It("times out a rebooting node past its deadline and fails its upgrade", func() {
		nodes := register(1)
		id, since := rebooting(nodes, store.CmdUpgrade, 1, false)

		advanceAt(id, since.Add(11*time.Minute))

		b := get(id)
		Expect(b.Nodes[0].State).To(Equal(store.BatchNodeTimedOut))
		Expect(b.Nodes[0].Message).To(Equal("did not come back within 10 minutes"))
		Expect(b.Nodes[0].FinishedAt).NotTo(BeNil())
		Expect(b.Phase).To(Equal(store.BatchFailed))
		Expect(nodeByID(nodes[0].ID).UpgradeState).To(Equal(store.UpgradeStateFailed))
	})

	It("times out a rebooting reset and fails the reset", func() {
		nodes := register(1)
		id, since := rebooting(nodes, store.CmdReset, 1, false)

		advanceAt(id, since.Add(11*time.Minute))

		Expect(get(id).Nodes[0].State).To(Equal(store.BatchNodeTimedOut))
		Expect(nodeByID(nodes[0].ID).ResetState).To(Equal(store.ResetStateFailed))
	})

	It("rounds the timeout up to whole minutes in its message", func() {
		nodes := register(1)
		b := &store.CommandBatch{ID: uuid.NewString(), Command: store.CmdReboot, Concurrency: 1, RebootTimeoutSeconds: 90}
		Expect(s.BatchCreate(ctx, b, []*store.BatchNode{{NodeID: nodes[0].ID, Hostname: "host-0"}})).To(Succeed())
		cmds := advance(b.ID)
		Expect(s.UpdateStatus(ctx, cmds[0].ID, store.CommandCompleted, "")).To(Succeed())
		advance(b.ID)

		advanceAt(b.ID, time.Now().Add(time.Hour))

		Expect(get(b.ID).Nodes[0].Message).To(Equal("did not come back within 2 minutes"))
	})

	// The node records its reboot when its command completes, a moment
	// before the spec reads the clock, so a boot a minute from now is after it.
	It("lets a node that timed out settle its own upgrade when it comes back", func() {
		nodes := register(1)
		id, since := rebooting(nodes, store.CmdUpgrade, 1, false)
		advanceAt(id, since.Add(11*time.Minute))
		Expect(nodeByID(nodes[0].ID).UpgradeState).To(Equal(store.UpgradeStateFailed))

		bootedAt := time.Now().Add(time.Minute)
		Expect(s.SettleAfterReboot(ctx, nodes[0].ID, store.BootStateActive, bootedAt)).To(Succeed())
		Expect(booted(nodes[0].ID, store.BootStateActive, &bootedAt)).To(BeEmpty())

		n := nodeByID(nodes[0].ID)
		Expect(n.UpgradeState).To(Equal(store.UpgradeStateDone))
		Expect(n.LastUpgrade).NotTo(BeNil())
		Expect(get(id).Nodes[0].State).To(Equal(store.BatchNodeTimedOut))
	})

	It("lets a node that timed out settle its own reset when it comes back", func() {
		nodes := register(1)
		id, since := rebooting(nodes, store.CmdReset, 1, false)
		advanceAt(id, since.Add(11*time.Minute))
		Expect(nodeByID(nodes[0].ID).ResetState).To(Equal(store.ResetStateFailed))

		Expect(s.SettleAfterReboot(ctx, nodes[0].ID, store.BootStateActive, time.Now().Add(time.Minute))).To(Succeed())

		Expect(nodeByID(nodes[0].ID).ResetState).To(Equal(store.ResetStateDone))
		Expect(get(id).Nodes[0].State).To(Equal(store.BatchNodeTimedOut))
	})

	It("keeps an upgrade its command failed as failed on a later boot", func() {
		nodes := register(1)
		id := newBatch(nodes, store.CmdUpgrade, 1, false)
		cmds := advance(id)
		Expect(s.UpdateStatus(ctx, cmds[0].ID, store.CommandFailed, "exit status 1")).To(Succeed())
		Expect(nodeByID(nodes[0].ID).UpgradeState).To(Equal(store.UpgradeStateFailed))

		Expect(s.SettleAfterReboot(ctx, nodes[0].ID, store.BootStateActive, time.Now().Add(time.Minute))).To(Succeed())

		Expect(nodeByID(nodes[0].ID).UpgradeState).To(Equal(store.UpgradeStateFailed))
	})

	It("keeps an upgrade that fell back as failed on a later boot", func() {
		nodes := register(1)
		rebooting(nodes, store.CmdUpgrade, 1, false)

		Expect(s.SettleAfterReboot(ctx, nodes[0].ID, store.BootStatePassive, time.Now().Add(time.Minute))).To(Succeed())
		Expect(nodeByID(nodes[0].ID).UpgradeState).To(Equal(store.UpgradeStateFailed))
		Expect(s.SettleAfterReboot(ctx, nodes[0].ID, store.BootStateActive, time.Now().Add(time.Hour))).To(Succeed())

		Expect(nodeByID(nodes[0].ID).UpgradeState).To(Equal(store.UpgradeStateFailed))
	})

	It("keeps an upgrade that fell back at registration as failed on a later boot", func() {
		nodes := register(1)
		rebooting(nodes, store.CmdUpgrade, 1, false)

		store.ResolveUpgrade(ctx, &gormstore.NodeStoreAdapter{S: s}, nodeByID(nodes[0].ID), store.BootStatePassive)
		Expect(nodeByID(nodes[0].ID).UpgradeState).To(Equal(store.UpgradeStateFailed))
		Expect(s.SettleAfterReboot(ctx, nodes[0].ID, store.BootStateActive, time.Now().Add(time.Hour))).To(Succeed())

		Expect(nodeByID(nodes[0].ID).UpgradeState).To(Equal(store.UpgradeStateFailed))
	})

	It("keeps a reset that fell back at registration as failed on a later boot", func() {
		nodes := register(1)
		rebooting(nodes, store.CmdReset, 1, false)

		store.ResolveReset(ctx, &gormstore.NodeStoreAdapter{S: s}, nodeByID(nodes[0].ID), store.BootStateRecovery)
		Expect(nodeByID(nodes[0].ID).ResetState).To(Equal(store.ResetStateFailed))
		Expect(s.SettleAfterReboot(ctx, nodes[0].ID, store.BootStateActive, time.Now().Add(time.Hour))).To(Succeed())

		Expect(nodeByID(nodes[0].ID).ResetState).To(Equal(store.ResetStateFailed))
	})

	It("leaves a rebooting node alone before its deadline", func() {
		nodes := register(1)
		id, since := rebooting(nodes, store.CmdUpgrade, 1, false)

		advanceAt(id, since.Add(9*time.Minute))

		b := get(id)
		Expect(b.Nodes[0].State).To(Equal(store.BatchNodeRebooting))
		Expect(b.Phase).To(Equal(store.BatchRunning))
		Expect(nodeByID(nodes[0].ID).UpgradeState).To(Equal(store.UpgradeStatePending))
	})

	It("names the failure generically when the failed command reported no result", func() {
		nodes := register(2)
		id := newBatch(nodes, store.CmdExec, 1, true)

		cmds := advance(id)
		Expect(s.UpdateStatus(ctx, cmds[0].ID, store.CommandFailed, "")).To(Succeed())
		advance(id)

		Expect(get(id).StopReason).To(Equal("host-0: command failed"))
	})

	It("stops the batch at the first failure and cancels the waiting nodes", func() {
		nodes := register(3)
		id := newBatch(nodes, store.CmdExec, 1, true)

		cmds := advance(id)
		Expect(s.UpdateStatus(ctx, cmds[0].ID, store.CommandFailed, "exit status 1")).To(Succeed())
		Expect(advance(id)).To(BeEmpty())

		b := get(id)
		Expect(b.Phase).To(Equal(store.BatchStopped))
		Expect(b.StoppedAt).NotTo(BeNil())
		Expect(b.StoppedByNodeID).To(HaveValue(Equal(nodes[0].ID)))
		Expect(b.StopReason).To(Equal("host-0: exit status 1"))
		Expect(b.FinishedAt).NotTo(BeNil())
		Expect(b.Nodes[0].State).To(Equal(store.BatchNodeFailed))
		for _, row := range b.Nodes[1:] {
			Expect(row.State).To(Equal(store.BatchNodeCanceled))
			Expect(row.Message).To(Equal("batch stopped"))
			Expect(row.FinishedAt).NotTo(BeNil())
			pending, err := s.GetPending(ctx, row.NodeID)
			Expect(err).NotTo(HaveOccurred())
			Expect(pending).To(BeEmpty())
		}
	})

	It("stops the batch when a node times out", func() {
		nodes := register(2)
		id, since := rebooting(nodes, store.CmdUpgrade, 1, true)

		advanceAt(id, since.Add(11*time.Minute))

		b := get(id)
		Expect(b.Phase).To(Equal(store.BatchStopped))
		Expect(b.StopReason).To(Equal("host-0: did not come back within 10 minutes"))
		Expect(b.Nodes[1].State).To(Equal(store.BatchNodeCanceled))
	})

	It("keeps going after a failure when stop on failure is off", func() {
		nodes := register(2)
		id := newBatch(nodes, store.CmdExec, 1, false)

		cmds := advance(id)
		Expect(s.UpdateStatus(ctx, cmds[0].ID, store.CommandFailed, "exit status 1")).To(Succeed())
		next := advance(id)
		Expect(next).To(HaveLen(1))
		Expect(next[0].ManagedNodeID).To(Equal(nodes[1].ID))
		Expect(get(id).Phase).To(Equal(store.BatchRunning))

		Expect(s.UpdateStatus(ctx, next[0].ID, store.CommandCompleted, "")).To(Succeed())
		advance(id)

		b := get(id)
		Expect(b.Phase).To(Equal(store.BatchFailed))
		Expect(b.StoppedAt).To(BeNil())
		Expect(b.FinishedAt).NotTo(BeNil())
	})

	It("lets running nodes finish after the batch stopped", func() {
		nodes := register(3)
		id := newBatch(nodes, store.CmdExec, 2, true)

		cmds := advance(id)
		Expect(cmds).To(HaveLen(2))
		claim(cmds[1].ID)
		Expect(s.UpdateStatus(ctx, cmds[0].ID, store.CommandFailed, "exit status 1")).To(Succeed())
		Expect(advance(id)).To(BeEmpty())

		b := get(id)
		Expect(b.Phase).To(Equal(store.BatchStopped))
		Expect(b.FinishedAt).To(BeNil())
		Expect(b.Nodes[1].State).To(Equal(store.BatchNodeRunning))
		Expect(b.Nodes[2].State).To(Equal(store.BatchNodeCanceled))

		Expect(s.UpdateStatus(ctx, cmds[1].ID, store.CommandCompleted, "")).To(Succeed())
		Expect(advance(id)).To(BeEmpty())

		b = get(id)
		Expect(b.Nodes[1].State).To(Equal(store.BatchNodeDone))
		Expect(b.Phase).To(Equal(store.BatchStopped))
		Expect(b.FinishedAt).NotTo(BeNil())
	})

	It("stops a running batch by hand", func() {
		nodes := register(3)
		id := newBatch(nodes, store.CmdExec, 1, true)
		claim(advance(id)[0].ID)

		Expect(s.BatchStop(ctx, id, "stopped by the user", time.Now())).To(Succeed())

		b := get(id)
		Expect(b.Phase).To(Equal(store.BatchStopped))
		Expect(b.StoppedAt).NotTo(BeNil())
		Expect(b.StoppedByNodeID).To(BeNil())
		Expect(b.StopReason).To(Equal("stopped by the user"))
		Expect(b.FinishedAt).To(BeNil())
		Expect(b.Nodes[0].State).To(Equal(store.BatchNodeRunning))
		for _, row := range b.Nodes[1:] {
			Expect(row.State).To(Equal(store.BatchNodeCanceled))
			Expect(row.Message).To(Equal("batch stopped"))
		}
		Expect(advance(id)).To(BeEmpty())
	})

	It("times out a node that keeps sending heartbeats when its delivered command never reports after a hand stop", func() {
		nodes := register(1)
		id := newBatch(nodes, store.CmdExec, 1, true)
		start := time.Now()
		cmds := advanceAt(id, start)
		claim(cmds[0].ID)
		stopped := start.Add(5 * time.Minute)
		Expect(s.BatchStop(ctx, id, "stopped by the user", stopped)).To(Succeed())
		Expect(get(id).Nodes[0].Deadline).To(HaveValue(BeTemporally("~", stopped.Add(10*time.Minute), time.Second)))

		heardFrom(nodes[0].ID, stopped.Add(9*time.Minute))
		Expect(advanceAt(id, stopped.Add(9*time.Minute))).To(BeEmpty())
		Expect(get(id).Nodes[0].State).To(Equal(store.BatchNodeRunning))

		later := stopped.Add(48 * time.Hour)
		heardFrom(nodes[0].ID, later)
		Expect(advanceAt(id, later)).To(BeEmpty())

		b := get(id)
		Expect(b.Phase).To(Equal(store.BatchStopped))
		Expect(b.FinishedAt).NotTo(BeNil())
		Expect(b.Nodes[0].State).To(Equal(store.BatchNodeTimedOut))
		Expect(b.Nodes[0].Message).To(Equal("did not finish its command after the batch stopped"))
		Expect(commandPhase(cmds[0].ID)).To(Equal(store.CommandDelivered))
		busy, err := s.BatchBusyNodes(ctx, []string{nodes[0].ID})
		Expect(err).NotTo(HaveOccurred())
		Expect(busy).To(BeEmpty())
	})

	It("finishes a batch stopped by hand when no node holds a slot", func() {
		nodes := register(2)
		id := newBatch(nodes, store.CmdExec, 1, true)

		Expect(s.BatchStop(ctx, id, "stopped by the user", time.Now())).To(Succeed())

		b := get(id)
		Expect(b.Phase).To(Equal(store.BatchStopped))
		Expect(b.FinishedAt).NotTo(BeNil())
	})

	It("refuses to stop a batch that is not running", func() {
		nodes := register(1)
		id := newBatch(nodes, store.CmdExec, 1, true)
		Expect(s.BatchStop(ctx, id, "stopped by the user", time.Now())).To(Succeed())

		Expect(s.BatchStop(ctx, id, "stopped by the user", time.Now())).To(MatchError(store.ErrBatchNotRunning))
		Expect(s.BatchStop(ctx, "missing", "stopped by the user", time.Now())).To(MatchError(store.ErrBatchNotFound))
	})

	DescribeTable("settles a rebooting node from the boot state it comes back with",
		func(command, bootState, state, message string, touched bool) {
			nodes := register(1)
			id, since := rebooting(nodes, command, 1, false)

			ids := booted(nodes[0].ID, bootState, at(since.Add(time.Minute)))
			if touched {
				Expect(ids).To(Equal([]string{id}))
			} else {
				Expect(ids).To(BeEmpty())
			}

			row := get(id).Nodes[0]
			Expect(row.State).To(Equal(state))
			Expect(row.Message).To(Equal(message))
			if store.IsFinalBatchNodeState(state) {
				Expect(row.FinishedAt).NotTo(BeNil())
			} else {
				Expect(row.FinishedAt).To(BeNil())
			}
		},
		Entry("upgrade on active", store.CmdUpgrade, store.BootStateActive, store.BatchNodeDone, "", true),
		Entry("upgrade on passive", store.CmdUpgrade, store.BootStatePassive, store.BatchNodeFailed, "fell back to passive", true),
		Entry("upgrade on recovery", store.CmdUpgrade, store.BootStateRecovery, store.BatchNodeFailed, "fell back to recovery", true),
		Entry("reset on passive", store.CmdReset, store.BootStatePassive, store.BatchNodeFailed, "fell back to passive", true),
		Entry("reset on recovery", store.CmdReset, store.BootStateRecovery, store.BatchNodeFailed, "fell back to recovery", true),
		Entry("reset on autoreset", store.CmdReset, store.BootStateAutoReset, store.BatchNodeRebooting, "", true),
		Entry("reset on active", store.CmdReset, store.BootStateActive, store.BatchNodeDone, "", true),
		Entry("reboot on active", store.CmdReboot, store.BootStateActive, store.BatchNodeDone, "", true),
		Entry("reboot on passive", store.CmdReboot, store.BootStatePassive, store.BatchNodeFailed, "fell back to passive", true),
		Entry("reboot on recovery", store.CmdReboot, store.BootStateRecovery, store.BatchNodeFailed, "fell back to recovery", true),
		Entry("reboot on an unknown boot state", store.CmdReboot, "netboot", store.BatchNodeDone, "", true),
		Entry("upgrade on an unknown boot state", store.CmdUpgrade, "netboot", store.BatchNodeRebooting, "", false),
		Entry("upgrade on autoreset", store.CmdUpgrade, store.BootStateAutoReset, store.BatchNodeRebooting, "", false),
		Entry("reset on an unknown boot state", store.CmdReset, "netboot", store.BatchNodeRebooting, "", false),
	)

	It("settles a rebooting node that registers again", func() {
		nodes := register(1)
		id, _ := rebooting(nodes, store.CmdUpgrade, 1, false)

		Expect(booted(nodes[0].ID, store.BootStateActive, nil)).To(Equal([]string{id}))

		Expect(get(id).Nodes[0].State).To(Equal(store.BatchNodeDone))
	})

	It("ignores a heartbeat from the boot before the reboot", func() {
		nodes := register(1)
		id, since := rebooting(nodes, store.CmdUpgrade, 1, false)

		Expect(booted(nodes[0].ID, store.BootStateActive, at(since.Add(-time.Hour)))).To(BeEmpty())

		Expect(get(id).Nodes[0].State).To(Equal(store.BatchNodeRebooting))
	})

	It("ignores the jitter of a whole-second uptime", func() {
		nodes := register(1)
		id, since := rebooting(nodes, store.CmdUpgrade, 1, false)

		Expect(booted(nodes[0].ID, store.BootStateActive, at(since.Add(2*time.Second)))).To(BeEmpty())

		Expect(get(id).Nodes[0].State).To(Equal(store.BatchNodeRebooting))
	})

	It("does not settle a node that comes back before its command completed", func() {
		nodes := register(1)
		id := newBatch(nodes, store.CmdUpgrade, 1, false)
		advance(id)

		Expect(booted(nodes[0].ID, store.BootStateActive, at(time.Now()))).To(BeEmpty())

		Expect(get(id).Nodes[0].State).To(Equal(store.BatchNodeRunning))
	})

	It("returns nothing for a node in no batch", func() {
		nodes := register(1)
		Expect(booted(nodes[0].ID, store.BootStateActive, nil)).To(BeEmpty())
	})

	It("follows a reset through its autoreset boot", func() {
		nodes := register(1)
		id, since := rebooting(nodes, store.CmdReset, 1, false)
		autoreset := since.Add(time.Minute)
		firstNow := time.Now().UTC().Truncate(time.Second)

		ids, err := s.BatchNodeBooted(ctx, nodes[0].ID, store.BootStateAutoReset, &autoreset, firstNow)
		Expect(err).NotTo(HaveOccurred())
		Expect(ids).To(Equal([]string{id}))
		row := get(id).Nodes[0]
		Expect(row.State).To(Equal(store.BatchNodeRebooting))
		Expect(row.RebootingSince).To(HaveValue(BeTemporally("==", autoreset)))
		Expect(row.Deadline).To(HaveValue(BeTemporally("==", firstNow.Add(10*time.Minute))))

		ids, err = s.BatchNodeBooted(ctx, nodes[0].ID, store.BootStateAutoReset, &autoreset, firstNow.Add(time.Minute))
		Expect(err).NotTo(HaveOccurred())
		Expect(ids).To(BeEmpty())
		row = get(id).Nodes[0]
		Expect(row.RebootingSince).To(HaveValue(BeTemporally("==", autoreset)))
		Expect(row.Deadline).To(HaveValue(BeTemporally("==", firstNow.Add(10*time.Minute))))

		Expect(booted(nodes[0].ID, store.BootStateActive, at(autoreset.Add(time.Minute)))).To(Equal([]string{id}))
		Expect(get(id).Nodes[0].State).To(Equal(store.BatchNodeDone))
	})

	It("restarts the deadline at now when a resetting node registers on autoreset", func() {
		nodes := register(1)
		id, _ := rebooting(nodes, store.CmdReset, 1, false)
		now := time.Now().UTC().Truncate(time.Second)

		_, err := s.BatchNodeBooted(ctx, nodes[0].ID, store.BootStateAutoReset, nil, now)
		Expect(err).NotTo(HaveOccurred())

		row := get(id).Nodes[0]
		Expect(row.State).To(Equal(store.BatchNodeRebooting))
		Expect(row.RebootingSince).To(HaveValue(BeTemporally("==", now)))
		Expect(row.Deadline).To(HaveValue(BeTemporally("==", now.Add(10*time.Minute))))
	})

	It("stops the batch when a node comes back on a fallback", func() {
		nodes := register(2)
		id, since := rebooting(nodes, store.CmdUpgrade, 1, true)

		booted(nodes[0].ID, store.BootStatePassive, at(since.Add(time.Minute)))

		b := get(id)
		Expect(b.Nodes[0].State).To(Equal(store.BatchNodeFailed))
		Expect(b.Phase).To(Equal(store.BatchStopped))
		Expect(b.StoppedByNodeID).To(HaveValue(Equal(nodes[0].ID)))
		Expect(b.StopReason).To(Equal("host-0: fell back to passive"))
		Expect(b.Nodes[1].State).To(Equal(store.BatchNodeCanceled))
		Expect(b.Nodes[1].Message).To(Equal("batch stopped"))
		Expect(b.FinishedAt).NotTo(BeNil())
	})

	It("still settles a rebooting node whose completed command was cleared from the history", func() {
		nodes := register(1)
		id, since := rebooting(nodes, store.CmdUpgrade, 1, false)

		Expect(s.CommandDeleteTerminal(ctx, nodes[0].ID)).To(Succeed())
		advance(id)
		Expect(get(id).Nodes[0].State).To(Equal(store.BatchNodeRebooting))

		booted(nodes[0].ID, store.BootStateActive, at(since.Add(time.Minute)))
		Expect(get(id).Nodes[0].State).To(Equal(store.BatchNodeDone))
	})

	It("postpones deadlines that passed while the server was down", func() {
		nodes := register(2)
		expired, since := rebooting(nodes[:1], store.CmdUpgrade, 1, false)
		later, _ := rebooting(nodes[1:], store.CmdUpgrade, 1, false)
		farDeadline := since.Add(time.Hour)
		Expect(s.UnsafeDB().Model(&store.BatchNode{}).Where("batch_id = ?", later).
			Update("deadline", farDeadline).Error).To(Succeed())
		now := since.Add(20 * time.Minute)

		Expect(s.BatchPostponeExpiredDeadlines(ctx, now)).To(Succeed())

		row := get(expired).Nodes[0]
		Expect(row.State).To(Equal(store.BatchNodeRebooting))
		Expect(row.Deadline).To(HaveValue(BeTemporally("==", now.Add(10*time.Minute))))
		Expect(get(later).Nodes[0].Deadline).To(HaveValue(BeTemporally("==", farDeadline)))
	})

	It("lists running batches and stopped batches with nodes still running as active", func() {
		nodes := register(5)
		running := newBatch(nodes[0:1], store.CmdExec, 1, true)

		stoppedRunning := newBatch(nodes[1:2], store.CmdExec, 1, true)
		claim(advance(stoppedRunning)[0].ID)
		Expect(s.BatchStop(ctx, stoppedRunning, "stopped by the user", time.Now())).To(Succeed())

		stoppedRebooting, _ := rebooting(nodes[2:3], store.CmdReboot, 1, true)
		Expect(s.BatchStop(ctx, stoppedRebooting, "stopped by the user", time.Now())).To(Succeed())

		stoppedIdle := newBatch(nodes[3:4], store.CmdExec, 1, true)
		Expect(s.BatchStop(ctx, stoppedIdle, "stopped by the user", time.Now())).To(Succeed())

		completed := newBatch(nodes[4:5], store.CmdExec, 1, true)
		cmds := advance(completed)
		Expect(s.UpdateStatus(ctx, cmds[0].ID, store.CommandCompleted, "")).To(Succeed())
		advance(completed)
		Expect(get(completed).Phase).To(Equal(store.BatchCompleted))

		ids, err := s.BatchActiveIDs(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(ids).To(ConsistOf(running, stoppedRunning, stoppedRebooting))
	})
})
