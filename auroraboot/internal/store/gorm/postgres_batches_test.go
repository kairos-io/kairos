package gorm_test

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm"

	gormstore "github.com/kairos-io/AuroraBoot/internal/store/gorm"
	"github.com/kairos-io/AuroraBoot/pkg/store"
)

// SQLite runs one writer at a time, so two advances can only overlap on
// PostgreSQL.
var _ = Describe("Gorm batch store on PostgreSQL", func() {
	var (
		s     *gormstore.Store
		ctx   context.Context
		meet  *rendezvous
		armed atomic.Bool
	)

	BeforeEach(func() {
		ctx = context.Background()
		s = newPostgresStore()

		// Both advances read the node rows of the batch before they decide
		// how many slots are free. Holding both there makes them overlap at
		// the point where one has to see the other's starts.
		meet = newRendezvous()
		armed.Store(false)
		hold := func(db *gorm.DB) {
			if armed.Load() && !db.DryRun && db.Statement.Table == "command_batch_nodes" {
				meet.arrive()
			}
		}
		Expect(s.UnsafeDB().Callback().Query().Before("gorm:query").Register("test:rendezvous", hold)).To(Succeed())
	})

	It("never starts more nodes than the concurrency when two advances run at once", func() {
		nodes := make([]*store.BatchNode, 6)
		for i := range nodes {
			n := &store.ManagedNode{MachineID: uuid.NewString()}
			Expect(s.Register(ctx, n)).To(Succeed())
			Expect(s.UpdatePhase(ctx, n.ID, store.PhaseOnline)).To(Succeed())
			nodes[i] = &store.BatchNode{NodeID: n.ID, Position: i, State: store.BatchNodeWaiting}
		}
		b := &store.CommandBatch{ID: uuid.NewString(), Command: store.CmdExec, Concurrency: 2}
		Expect(s.BatchCreate(ctx, b, nodes)).To(Succeed())

		armed.Store(true)
		var wg sync.WaitGroup
		started := make([][]*store.NodeCommand, 2)
		errs := make([]error, 2)
		for i := range 2 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				started[i], errs[i] = s.BatchAdvance(ctx, b.ID, time.Now())
			}()
		}
		wg.Wait()
		armed.Store(false)
		Expect(errs).To(HaveEach(Succeed()))

		Expect(len(started[0]) + len(started[1])).To(Equal(2))
		var running, commands int64
		Expect(s.UnsafeDB().Model(&store.BatchNode{}).
			Where("batch_id = ? AND state = ?", b.ID, store.BatchNodeRunning).
			Count(&running).Error).To(Succeed())
		Expect(running).To(Equal(int64(2)))
		Expect(s.UnsafeDB().Model(&store.NodeCommand{}).
			Where("batch_id = ?", b.ID).
			Count(&commands).Error).To(Succeed())
		Expect(commands).To(Equal(int64(2)))
	})

	// rebootingOnPostgres starts an upgrade batch over n nodes with
	// concurrency 1 and brings node 0 to rebooting since a minute ago.
	rebootingOnPostgres := func(n int) (*store.CommandBatch, []*store.ManagedNode, time.Time) {
		nodes := make([]*store.ManagedNode, n)
		rows := make([]*store.BatchNode, n)
		for i := range nodes {
			nodes[i] = &store.ManagedNode{MachineID: uuid.NewString(), Hostname: uuid.NewString()}
			Expect(s.Register(ctx, nodes[i])).To(Succeed())
			Expect(s.UpdatePhase(ctx, nodes[i].ID, store.PhaseOnline)).To(Succeed())
			rows[i] = &store.BatchNode{NodeID: nodes[i].ID, Position: i, Hostname: nodes[i].Hostname}
		}
		b := &store.CommandBatch{
			ID: uuid.NewString(), Command: store.CmdUpgrade, Concurrency: 1,
			StopOnFailure: true, RebootTimeoutSeconds: store.DefaultRebootTimeoutSeconds,
		}
		Expect(s.BatchCreate(ctx, b, rows)).To(Succeed())
		cmds, err := s.BatchAdvance(ctx, b.ID, time.Now())
		Expect(err).NotTo(HaveOccurred())
		Expect(s.UpdateStatus(ctx, cmds[0].ID, store.CommandCompleted, "")).To(Succeed())
		since := time.Now().Add(-time.Minute).UTC().Truncate(time.Second)
		Expect(s.UnsafeDB().Model(&store.NodeCommand{}).Where("id = ?", cmds[0].ID).
			Update("completed_at", since).Error).To(Succeed())
		_, err = s.BatchAdvance(ctx, b.ID, time.Now())
		Expect(err).NotTo(HaveOccurred())
		return b, nodes, since
	}

	It("lists a batch with the hostnames of its first nodes in start order", func() {
		b := &store.CommandBatch{ID: uuid.NewString(), Command: store.CmdUpgrade, Concurrency: 1}
		var rows []*store.BatchNode
		for i := 6; i >= 0; i-- {
			rows = append(rows, &store.BatchNode{
				BatchID: b.ID, NodeID: uuid.NewString(), Position: i,
				Hostname: "host-" + string(rune('a'+i)), State: store.BatchNodeWaiting,
			})
		}
		Expect(s.BatchCreate(ctx, b, rows)).To(Succeed())

		list, _, err := s.BatchList(ctx, store.BatchFilter{})
		Expect(err).NotTo(HaveOccurred())
		Expect(list).To(HaveLen(1))
		Expect(list[0].Hostnames).To(Equal([]string{"host-a", "host-b", "host-c", "host-d", "host-e"}))
	})

	It("stops the batch when a node comes back on a fallback", func() {
		b, nodes, since := rebootingOnPostgres(2)

		bootedAt := since.Add(time.Minute)
		ids, err := s.BatchNodeBooted(ctx, nodes[0].ID, store.BootStatePassive, &bootedAt, time.Now())
		Expect(err).NotTo(HaveOccurred())
		Expect(ids).To(Equal([]string{b.ID}))

		got, err := s.BatchGet(ctx, b.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(got.Phase).To(Equal(store.BatchStopped))
		Expect(got.StoppedByNodeID).To(HaveValue(Equal(nodes[0].ID)))
		Expect(got.Nodes[0].State).To(Equal(store.BatchNodeFailed))
		Expect(got.Nodes[1].State).To(Equal(store.BatchNodeCanceled))
		Expect(got.FinishedAt).NotTo(BeNil())
	})

	It("postpones an expired deadline, then times the node out and fails its upgrade", func() {
		b, nodes, since := rebootingOnPostgres(1)
		restart := since.Add(20 * time.Minute)

		Expect(s.BatchPostponeExpiredDeadlines(ctx, restart)).To(Succeed())
		got, err := s.BatchGet(ctx, b.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(got.Nodes[0].Deadline).To(HaveValue(BeTemporally("==", restart.Add(10*time.Minute))))

		_, err = s.BatchAdvance(ctx, b.ID, restart.Add(11*time.Minute))
		Expect(err).NotTo(HaveOccurred())
		got, err = s.BatchGet(ctx, b.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(got.Nodes[0].State).To(Equal(store.BatchNodeTimedOut))
		Expect(got.Phase).To(Equal(store.BatchStopped))
		node, err := s.NodeGetByID(ctx, nodes[0].ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(node.UpgradeState).To(Equal(store.UpgradeStateFailed))

		ids, err := s.BatchActiveIDs(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(ids).NotTo(ContainElement(b.ID))
	})
})
