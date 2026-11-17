package gorm_test

import (
	"context"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm"

	gormstore "github.com/kairos-io/AuroraBoot/internal/store/gorm"
	"github.com/kairos-io/AuroraBoot/pkg/store"
)

var _ = Describe("Gorm batch advance", func() {
	var (
		s      *gormstore.Store
		ctx    context.Context
		dbPath string
	)

	BeforeEach(func() {
		var err error
		dbPath = filepath.Join(GinkgoT().TempDir(), "advance.db")
		s, err = gormstore.New(dbPath)
		Expect(err).NotTo(HaveOccurred())
		ctx = context.Background()
	})

	AfterEach(func() { Expect(s.Close()).To(Succeed()) })

	register := func(n int) []*store.ManagedNode {
		nodes := make([]*store.ManagedNode, n)
		for i := range nodes {
			nodes[i] = &store.ManagedNode{MachineID: uuid.NewString(), Hostname: uuid.NewString()}
			Expect(s.Register(ctx, nodes[i])).To(Succeed())
		}
		return nodes
	}

	online := func(nodes ...*store.ManagedNode) {
		for _, n := range nodes {
			Expect(s.UpdatePhase(ctx, n.ID, store.PhaseOnline)).To(Succeed())
		}
	}

	offline := func(nodes ...*store.ManagedNode) {
		for _, n := range nodes {
			Expect(s.UpdatePhase(ctx, n.ID, store.PhaseOffline)).To(Succeed())
		}
	}

	newBatch := func(nodes []*store.ManagedNode, command string, concurrency int) string {
		b := &store.CommandBatch{
			ID:                   uuid.NewString(),
			Command:              command,
			Args:                 map[string]string{"k": "v"},
			Concurrency:          concurrency,
			StopOnFailure:        false,
			RebootTimeoutSeconds: store.DefaultRebootTimeoutSeconds,
		}
		rows := make([]*store.BatchNode, len(nodes))
		for i, n := range nodes {
			rows[i] = &store.BatchNode{NodeID: n.ID, Position: i, Hostname: n.Hostname, State: store.BatchNodeWaiting}
		}
		Expect(s.BatchCreate(ctx, b, rows)).To(Succeed())
		return b.ID
	}

	advance := func(batchID string) []*store.NodeCommand {
		cmds, err := s.BatchAdvance(ctx, batchID, time.Now())
		Expect(err).NotTo(HaveOccurred())
		return cmds
	}

	get := func(batchID string) *store.CommandBatch {
		b, err := s.BatchGet(ctx, batchID)
		Expect(err).NotTo(HaveOccurred())
		return b
	}

	It("starts as many online nodes as the concurrency allows, in order", func() {
		nodes := register(3)
		online(nodes...)
		id := newBatch(nodes, store.CmdExec, 2)

		cmds := advance(id)
		Expect(cmds).To(HaveLen(2))
		Expect([]string{cmds[0].ManagedNodeID, cmds[1].ManagedNodeID}).To(Equal([]string{nodes[0].ID, nodes[1].ID}))
		for _, c := range cmds {
			Expect(c.BatchID).To(Equal(id))
			Expect(c.Command).To(Equal(store.CmdExec))
			Expect(c.Args).To(Equal(map[string]string{"k": "v"}))
			Expect(c.Phase).To(Equal(store.CommandPending))
		}

		b := get(id)
		Expect(b.Nodes[0].State).To(Equal(store.BatchNodeRunning))
		Expect(b.Nodes[0].CommandID).To(HaveValue(Equal(cmds[0].ID)))
		Expect(b.Nodes[0].StartedAt).NotTo(BeNil())
		Expect(b.Nodes[1].State).To(Equal(store.BatchNodeRunning))
		Expect(b.Nodes[1].CommandID).To(HaveValue(Equal(cmds[1].ID)))
		Expect(b.Nodes[2].State).To(Equal(store.BatchNodeWaiting))
		Expect(b.Nodes[2].CommandID).To(BeNil())

		pending, err := s.GetPending(ctx, nodes[0].ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(pending).To(HaveLen(1))
		Expect(pending[0].ID).To(Equal(cmds[0].ID))
	})

	It("starts every node when the concurrency is 0", func() {
		nodes := register(3)
		online(nodes...)
		id := newBatch(nodes, store.CmdExec, 0)

		Expect(advance(id)).To(HaveLen(3))
		Expect(get(id).Counts).To(Equal(map[string]int{store.BatchNodeRunning: 3}))
	})

	It("starts the next node when a running node's command completes", func() {
		nodes := register(2)
		online(nodes...)
		id := newBatch(nodes, store.CmdExec, 1)

		first := advance(id)
		Expect(first).To(HaveLen(1))
		Expect(s.UpdateStatus(ctx, first[0].ID, store.CommandCompleted, "")).To(Succeed())

		second := advance(id)
		Expect(second).To(HaveLen(1))
		Expect(second[0].ManagedNodeID).To(Equal(nodes[1].ID))

		b := get(id)
		Expect(b.Nodes[0].State).To(Equal(store.BatchNodeDone))
		Expect(b.Nodes[0].FinishedAt).NotTo(BeNil())
		Expect(b.Nodes[1].State).To(Equal(store.BatchNodeRunning))
	})

	It("moves a node whose upgrade completed to rebooting with its deadline", func() {
		nodes := register(2)
		online(nodes...)
		id := newBatch(nodes, store.CmdUpgrade, 1)

		cmds := advance(id)
		Expect(cmds).To(HaveLen(1))
		Expect(s.UpdateStatus(ctx, cmds[0].ID, store.CommandCompleted, "")).To(Succeed())
		completedAt := time.Now().Add(-time.Minute).UTC().Truncate(time.Second)
		Expect(s.UnsafeDB().Model(&store.NodeCommand{}).Where("id = ?", cmds[0].ID).
			Update("completed_at", completedAt).Error).To(Succeed())

		Expect(advance(id)).To(BeEmpty())

		b := get(id)
		Expect(b.Nodes[0].State).To(Equal(store.BatchNodeRebooting))
		Expect(b.Nodes[0].RebootingSince).To(HaveValue(BeTemporally("==", completedAt)))
		Expect(b.Nodes[0].Deadline).To(HaveValue(BeTemporally("==", completedAt.Add(10*time.Minute))))
		Expect(b.Nodes[0].FinishedAt).To(BeNil())
		Expect(b.Nodes[1].State).To(Equal(store.BatchNodeWaiting))
	})

	It("marks a node failed with its command's result", func() {
		nodes := register(1)
		online(nodes...)
		id := newBatch(nodes, store.CmdExec, 1)

		cmds := advance(id)
		Expect(s.UpdateStatus(ctx, cmds[0].ID, store.CommandFailed, "exit status 1")).To(Succeed())
		advance(id)

		b := get(id)
		Expect(b.Nodes[0].State).To(Equal(store.BatchNodeFailed))
		Expect(b.Nodes[0].Message).To(Equal("exit status 1"))
		Expect(b.Nodes[0].FinishedAt).NotTo(BeNil())
		Expect(b.Phase).To(Equal(store.BatchFailed))
		Expect(b.FinishedAt).NotTo(BeNil())
	})

	It("cancels a running node whose command was deleted", func() {
		nodes := register(1)
		online(nodes...)
		id := newBatch(nodes, store.CmdExec, 1)

		cmds := advance(id)
		Expect(s.CommandDelete(ctx, cmds[0].ID)).To(Succeed())
		advance(id)

		b := get(id)
		Expect(b.Nodes[0].State).To(Equal(store.BatchNodeCanceled))
		Expect(b.Nodes[0].Message).To(Equal("command deleted"))
		Expect(b.Nodes[0].FinishedAt).NotTo(BeNil())
	})

	It("cancels the rows of a node that was deleted", func() {
		nodes := register(2)
		online(nodes...)
		id := newBatch(nodes, store.CmdExec, 1)

		advance(id)
		Expect(s.NodeDelete(ctx, nodes[0].ID)).To(Succeed())
		Expect(s.NodeDelete(ctx, nodes[1].ID)).To(Succeed())
		Expect(advance(id)).To(BeEmpty())

		b := get(id)
		for _, row := range b.Nodes {
			Expect(row.State).To(Equal(store.BatchNodeCanceled))
			Expect(row.Message).To(Equal("node deleted"))
			Expect(row.FinishedAt).NotTo(BeNil())
		}
	})

	It("passes over an offline node and starts the next online one", func() {
		nodes := register(2)
		offline(nodes[0])
		online(nodes[1])
		id := newBatch(nodes, store.CmdExec, 1)

		cmds := advance(id)
		Expect(cmds).To(HaveLen(1))
		Expect(cmds[0].ManagedNodeID).To(Equal(nodes[1].ID))

		b := get(id)
		Expect(b.Nodes[0].State).To(Equal(store.BatchNodeWaiting))
		Expect(b.Nodes[1].State).To(Equal(store.BatchNodeRunning))
	})

	It("skips the waiting nodes once only offline nodes are left", func() {
		nodes := register(2)
		online(nodes[0])
		offline(nodes[1])
		id := newBatch(nodes, store.CmdExec, 1)

		cmds := advance(id)
		Expect(s.UpdateStatus(ctx, cmds[0].ID, store.CommandCompleted, "")).To(Succeed())
		Expect(advance(id)).To(BeEmpty())

		b := get(id)
		Expect(b.Nodes[0].State).To(Equal(store.BatchNodeDone))
		Expect(b.Nodes[1].State).To(Equal(store.BatchNodeSkipped))
		Expect(b.Nodes[1].Message).To(Equal("offline when the batch reached it"))
		Expect(b.Nodes[1].FinishedAt).NotTo(BeNil())
		Expect(b.Phase).To(Equal(store.BatchCompleted))
	})

	It("completes the batch when every node is done", func() {
		nodes := register(2)
		online(nodes...)
		id := newBatch(nodes, store.CmdExec, 0)

		for _, c := range advance(id) {
			Expect(s.UpdateStatus(ctx, c.ID, store.CommandCompleted, "")).To(Succeed())
		}
		advance(id)

		b := get(id)
		Expect(b.Phase).To(Equal(store.BatchCompleted))
		Expect(b.FinishedAt).NotTo(BeNil())
		Expect(b.Counts).To(Equal(map[string]int{store.BatchNodeDone: 2}))
	})

	It("marks the upgrade lifecycle of a node when its command is created", func() {
		nodes := register(1)
		online(nodes...)
		id := newBatch(nodes, store.CmdUpgrade, 1)

		Expect(advance(id)).To(HaveLen(1))
		got, err := s.NodeGetByID(ctx, nodes[0].ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(got.UpgradeState).To(Equal(store.UpgradeStatePending))
	})

	It("expires a reset command it creates after the store's reset timeout", func() {
		s.WithResetTimeout(30 * time.Minute)
		nodes := register(1)
		online(nodes...)
		id := newBatch(nodes, store.CmdReset, 1)

		cmds := advance(id)

		Expect(cmds).To(HaveLen(1))
		Expect(cmds[0].ExpiresAt).NotTo(BeNil())
		Expect(*cmds[0].ExpiresAt).To(BeTemporally("~", time.Now().Add(30*time.Minute), time.Minute))
		stored, err := s.CommandGetByID(ctx, cmds[0].ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(stored.ExpiresAt).NotTo(BeNil())
	})

	It("leaves reset commands without an expiry when the reset timeout is disabled", func() {
		s.WithResetTimeout(-1)
		nodes := register(1)
		online(nodes...)
		id := newBatch(nodes, store.CmdReset, 1)

		cmds := advance(id)

		Expect(cmds).To(HaveLen(1))
		Expect(cmds[0].ExpiresAt).To(BeNil())
	})

	It("gives commands other than reset no expiry", func() {
		s.WithResetTimeout(30 * time.Minute)
		nodes := register(1)
		online(nodes...)
		id := newBatch(nodes, store.CmdUpgrade, 1)

		cmds := advance(id)

		Expect(cmds).To(HaveLen(1))
		Expect(cmds[0].ExpiresAt).To(BeNil())
	})

	// Agents report status and heartbeats on their own connections while a
	// batch advances. Under WAL a transaction that reads first works from a
	// snapshot, and once another connection commits, its first write fails
	// with SQLITE_BUSY_SNAPSHOT without waiting on busy_timeout. The spec
	// commits such a write from a second store on the same file as soon as
	// the advance runs any query against the database; the advance has to
	// succeed and start its node.
	It("advances the batch while another connection commits a write", func() {
		nodes := register(3)
		online(nodes...)
		id := newBatch(nodes[:2], store.CmdExec, 1)

		agent, err := gormstore.New(dbPath)
		Expect(err).NotTo(HaveOccurred())
		defer func() { Expect(agent.Close()).To(Succeed()) }()

		// The agent write runs on its own goroutine and gets a moment to
		// commit. While the advance holds the write lock the agent waits on
		// busy_timeout instead, and commits once the advance is done.
		armed := false
		agentDone := make(chan error, 1)
		Expect(s.UnsafeDB().Callback().Query().After("gorm:query").
			Register("test:concurrent_agent_write", func(db *gorm.DB) {
				// A DryRun query only renders a subquery and never
				// reaches the database.
				if !armed || db.DryRun {
					return
				}
				armed = false
				committed := make(chan struct{})
				go func() {
					defer GinkgoRecover()
					err := agent.UpdatePhase(ctx, nodes[2].ID, store.PhaseOffline)
					close(committed)
					agentDone <- err
				}()
				select {
				case <-committed:
				case <-time.After(300 * time.Millisecond):
				}
			})).To(Succeed())

		armed = true
		cmds, err := s.BatchAdvance(ctx, id, time.Now())
		armed = false
		Eventually(agentDone).Within(10 * time.Second).Should(Receive(BeNil()))
		Expect(err).NotTo(HaveOccurred())
		Expect(cmds).To(HaveLen(1))
		Expect(get(id).Nodes[0].State).To(Equal(store.BatchNodeRunning))
	})

	It("returns ErrBatchNotFound for an unknown batch", func() {
		_, err := s.BatchAdvance(ctx, "missing", time.Now())
		Expect(err).To(MatchError(store.ErrBatchNotFound))
	})
})
