package batches_test

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	gormstore "github.com/kairos-io/AuroraBoot/internal/store/gorm"
	"github.com/kairos-io/AuroraBoot/pkg/batches"
	"github.com/kairos-io/AuroraBoot/pkg/store"
)

var _ = Describe("Scheduler", func() {
	var (
		s         *gormstore.Store
		bs        store.BatchStore
		sched     *batches.Scheduler
		ctx       context.Context
		mu        sync.Mutex
		delivered []*store.NodeCommand
	)

	BeforeEach(func() {
		var err error
		s, err = gormstore.New(filepath.Join(GinkgoT().TempDir(), "sched.db"))
		Expect(err).NotTo(HaveOccurred())
		bs = &gormstore.BatchStoreAdapter{S: s}
		ctx = context.Background()
		delivered = nil
		sched = batches.New(bs, func(_ context.Context, cmd *store.NodeCommand) {
			mu.Lock()
			defer mu.Unlock()
			delivered = append(delivered, cmd)
		})
	})

	AfterEach(func() { Expect(s.Close()).To(Succeed()) })

	deliveredCmds := func() []*store.NodeCommand {
		mu.Lock()
		defer mu.Unlock()
		return append([]*store.NodeCommand(nil), delivered...)
	}

	onlineNodes := func(n int) []*store.ManagedNode {
		nodes := make([]*store.ManagedNode, n)
		for i := range nodes {
			nodes[i] = &store.ManagedNode{MachineID: uuid.NewString(), Hostname: uuid.NewString()}
			Expect(s.Register(ctx, nodes[i])).To(Succeed())
			Expect(s.UpdatePhase(ctx, nodes[i].ID, store.PhaseOnline)).To(Succeed())
		}
		return nodes
	}

	request := func(command string, concurrency int, nodes ...*store.ManagedNode) batches.StartRequest {
		return batches.StartRequest{
			Command:              command,
			Args:                 map[string]string{"k": "v"},
			Nodes:                nodes,
			Selection:            "test",
			Concurrency:          concurrency,
			StopOnFailure:        true,
			RebootTimeoutSeconds: 600,
		}
	}

	It("creates the batch and delivers the first slot's commands", func() {
		nodes := onlineNodes(3)
		b, err := sched.Start(ctx, request(store.CmdExec, 2, nodes...))
		Expect(err).NotTo(HaveOccurred())
		Expect(b.ID).NotTo(BeEmpty())
		Expect(b.Phase).To(Equal(store.BatchRunning))
		Expect(b.Nodes).To(HaveLen(3))
		Expect(b.Counts).To(Equal(map[string]int{store.BatchNodeRunning: 2, store.BatchNodeWaiting: 1}))

		cmds := deliveredCmds()
		Expect(cmds).To(HaveLen(2))
		Expect(cmds[0].ManagedNodeID).To(Equal(nodes[0].ID))
		Expect(cmds[1].ManagedNodeID).To(Equal(nodes[1].ID))
	})

	It("gives a node listed twice one row", func() {
		nodes := onlineNodes(1)
		b, err := sched.Start(ctx, request(store.CmdExec, 0, nodes[0], nodes[0]))
		Expect(err).NotTo(HaveOccurred())
		Expect(b.Nodes).To(HaveLen(1))
		Expect(deliveredCmds()).To(HaveLen(1))
	})

	It("refuses to start when a node is in a batch that has not finished with it", func() {
		nodes := onlineNodes(2)
		_, err := sched.Start(ctx, request(store.CmdExec, 1, nodes[0]))
		Expect(err).NotTo(HaveOccurred())

		_, err = sched.Start(ctx, request(store.CmdExec, 1, nodes[0], nodes[1]))
		var busy *batches.ErrNodesBusy
		Expect(errors.As(err, &busy)).To(BeTrue())
		Expect(busy.NodeIDs).To(Equal([]string{nodes[0].ID}))

		list, total, err := bs.List(ctx, store.BatchFilter{})
		Expect(err).NotTo(HaveOccurred())
		Expect(total).To(Equal(1))
		Expect(list).To(HaveLen(1))
	})

	It("reports a node as busy while its row in a stopped batch is still running", func() {
		nodes := onlineNodes(2)
		b, err := sched.Start(ctx, request(store.CmdExec, 1, nodes[0], nodes[1]))
		Expect(err).NotTo(HaveOccurred())
		started, err := bs.Get(ctx, b.ID)
		Expect(err).NotTo(HaveOccurred())
		claimed, err := s.ClaimForDelivery(ctx, *started.Nodes[0].CommandID)
		Expect(err).NotTo(HaveOccurred())
		Expect(claimed).To(BeTrue())
		Expect(sched.Stop(ctx, b.ID)).To(Succeed())

		called := false
		err = sched.WhenIdle(ctx, []string{nodes[0].ID, nodes[1].ID}, func() error { called = true; return nil })
		var busy *batches.ErrNodesBusy
		Expect(errors.As(err, &busy)).To(BeTrue())
		Expect(busy.NodeIDs).To(Equal([]string{nodes[0].ID}))
		Expect(called).To(BeFalse())
	})

	It("runs create for nodes that are in no batch", func() {
		nodes := onlineNodes(1)
		called := false
		Expect(sched.WhenIdle(ctx, []string{nodes[0].ID}, func() error { called = true; return nil })).To(Succeed())
		Expect(called).To(BeTrue())
	})

	It("returns the error create returns", func() {
		nodes := onlineNodes(1)
		boom := errors.New("boom")
		Expect(sched.WhenIdle(ctx, []string{nodes[0].ID}, func() error { return boom })).To(MatchError(boom))
	})

	It("holds back a batch start for the same node until create has returned", func() {
		nodes := onlineNodes(1)
		var b *store.CommandBatch
		var startErr error
		started := make(chan struct{})
		err := sched.WhenIdle(ctx, []string{nodes[0].ID}, func() error {
			go func() {
				defer GinkgoRecover()
				defer close(started)
				b, startErr = sched.Start(ctx, request(store.CmdUpgrade, 1, nodes[0]))
			}()
			Consistently(started, 200*time.Millisecond).ShouldNot(BeClosed())
			return nil
		})
		Expect(err).NotTo(HaveOccurred())
		Eventually(started).Should(BeClosed())
		Expect(startErr).NotTo(HaveOccurred())
		Expect(b.Nodes).To(HaveLen(1))
	})

	It("lets exactly one of a concurrent batch start and create act on a node first", func() {
		for range 20 {
			nodes := onlineNodes(1)
			before, _, err := bs.List(ctx, store.BatchFilter{NodeID: nodes[0].ID})
			Expect(err).NotTo(HaveOccurred())
			Expect(before).To(BeEmpty())

			release := make(chan struct{})
			var wg sync.WaitGroup
			var idleErr error
			var createdBeside bool
			wg.Add(2)
			go func() {
				defer GinkgoRecover()
				defer wg.Done()
				<-release
				_, err := sched.Start(ctx, request(store.CmdUpgrade, 1, nodes[0]))
				Expect(err).NotTo(HaveOccurred())
			}()
			go func() {
				defer GinkgoRecover()
				defer wg.Done()
				<-release
				idleErr = sched.WhenIdle(ctx, []string{nodes[0].ID}, func() error {
					time.Sleep(5 * time.Millisecond)
					list, _, err := bs.List(ctx, store.BatchFilter{NodeID: nodes[0].ID})
					Expect(err).NotTo(HaveOccurred())
					createdBeside = len(list) > 0
					return nil
				})
			}()
			close(release)
			wg.Wait()

			var busy *batches.ErrNodesBusy
			if idleErr != nil {
				Expect(errors.As(idleErr, &busy)).To(BeTrue())
			}
			Expect(createdBeside).To(BeFalse(), "a batch started for the node between the busy check and create")
		}
	})

	It("returns ErrNoNodes for an empty node list", func() {
		_, err := sched.Start(ctx, request(store.CmdExec, 1))
		Expect(err).To(MatchError(batches.ErrNoNodes))
	})

	It("delivers the next node's command after the previous one completed", func() {
		nodes := onlineNodes(2)
		b, err := sched.Start(ctx, request(store.CmdExec, 1, nodes...))
		Expect(err).NotTo(HaveOccurred())
		first := deliveredCmds()
		Expect(first).To(HaveLen(1))

		Expect(s.UpdateStatus(ctx, first[0].ID, store.CommandCompleted, "")).To(Succeed())
		sched.Advance(ctx, b.ID)

		cmds := deliveredCmds()
		Expect(cmds).To(HaveLen(2))
		Expect(cmds[1].ManagedNodeID).To(Equal(nodes[1].ID))
	})

	It("advances every active batch on a tick", func() {
		nodes := onlineNodes(4)
		a, err := sched.Start(ctx, request(store.CmdExec, 1, nodes[0], nodes[1]))
		Expect(err).NotTo(HaveOccurred())
		c, err := sched.Start(ctx, request(store.CmdExec, 1, nodes[2], nodes[3]))
		Expect(err).NotTo(HaveOccurred())
		Expect(deliveredCmds()).To(HaveLen(2))

		for _, cmd := range deliveredCmds() {
			Expect(s.UpdateStatus(ctx, cmd.ID, store.CommandCompleted, "")).To(Succeed())
		}
		sched.Tick(ctx)

		cmds := deliveredCmds()
		Expect(cmds).To(HaveLen(4))
		Expect([]string{cmds[2].BatchID, cmds[3].BatchID}).To(ConsistOf(a.ID, c.ID))
	})

	rebootingBatchOf := func(n int) (*store.CommandBatch, []*store.ManagedNode) {
		nodes := onlineNodes(n)
		b, err := sched.Start(ctx, request(store.CmdUpgrade, 1, nodes...))
		Expect(err).NotTo(HaveOccurred())
		cmds := deliveredCmds()
		Expect(cmds).To(HaveLen(1))
		Expect(s.UpdateStatus(ctx, cmds[0].ID, store.CommandCompleted, "")).To(Succeed())
		sched.Tick(ctx)
		got, err := bs.Get(ctx, b.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(got.Nodes[0].State).To(Equal(store.BatchNodeRebooting))
		return got, nodes
	}

	rebootingBatch := func() (*store.CommandBatch, *store.ManagedNode) {
		b, nodes := rebootingBatchOf(1)
		return b, nodes[0]
	}

	It("times out a rebooting node on a tick after its deadline", func() {
		b, _ := rebootingBatch()
		sched.SetClock(func() time.Time { return b.Nodes[0].Deadline.Add(time.Minute) })

		sched.Tick(ctx)

		got, err := bs.Get(ctx, b.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(got.Nodes[0].State).To(Equal(store.BatchNodeTimedOut))
	})

	It("postpones expired deadlines when it starts running", func() {
		b, _ := rebootingBatch()
		after := b.Nodes[0].Deadline.Add(time.Hour)
		sched.SetClock(func() time.Time { return after })

		runCtx, cancel := context.WithCancel(ctx)
		done := make(chan struct{})
		go func() {
			defer close(done)
			sched.Run(runCtx, 10*time.Millisecond)
		}()
		Eventually(func() bool {
			got, err := bs.Get(ctx, b.ID)
			Expect(err).NotTo(HaveOccurred())
			return got.Nodes[0].Deadline.After(after)
		}, 5*time.Second, 10*time.Millisecond).Should(BeTrue())
		cancel()
		Eventually(done).Should(BeClosed())

		got, err := bs.Get(ctx, b.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(got.Nodes[0].State).To(Equal(store.BatchNodeRebooting))
		Expect(got.Nodes[0].Deadline).To(HaveValue(BeTemporally("==", after.Add(600*time.Second))))
	})

	Describe("NodeBooted", func() {
		It("settles a rebooting node that booted after its reboot and delivers the next command", func() {
			b, nodes := rebootingBatchOf(2)
			bootedAt := b.Nodes[0].RebootingSince.Add(time.Minute)

			sched.NodeBooted(ctx, nodes[0].ID, store.BootStateActive, &bootedAt)

			got, err := bs.Get(ctx, b.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Nodes[0].State).To(Equal(store.BatchNodeDone))
			cmds := deliveredCmds()
			Expect(cmds).To(HaveLen(2))
			Expect(cmds[1].ManagedNodeID).To(Equal(nodes[1].ID))
		})

		It("settles a rebooting node on a registration", func() {
			b, nodes := rebootingBatchOf(2)

			sched.NodeBooted(ctx, nodes[0].ID, store.BootStateActive, nil)

			got, err := bs.Get(ctx, b.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Nodes[0].State).To(Equal(store.BatchNodeDone))
			cmds := deliveredCmds()
			Expect(cmds).To(HaveLen(2))
			Expect(cmds[1].ManagedNodeID).To(Equal(nodes[1].ID))
		})

		It("leaves a node alone when its boot is from before the reboot", func() {
			b, nodes := rebootingBatchOf(2)
			bootedAt := b.Nodes[0].RebootingSince.Add(-time.Minute)

			sched.NodeBooted(ctx, nodes[0].ID, store.BootStateActive, &bootedAt)

			got, err := bs.Get(ctx, b.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Nodes[0].State).To(Equal(store.BatchNodeRebooting))
			Expect(deliveredCmds()).To(HaveLen(1))
		})
	})

	Describe("Stop", func() {
		It("stops a running batch and cancels its waiting rows and undelivered commands", func() {
			nodes := onlineNodes(2)
			b, err := sched.Start(ctx, request(store.CmdExec, 1, nodes...))
			Expect(err).NotTo(HaveOccurred())

			Expect(sched.Stop(ctx, b.ID)).To(Succeed())

			got, err := bs.Get(ctx, b.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Phase).To(Equal(store.BatchStopped))
			Expect(got.StopReason).To(Equal("stopped by the user"))
			Expect(got.Nodes[0].State).To(Equal(store.BatchNodeCanceled))
			Expect(got.Nodes[1].State).To(Equal(store.BatchNodeCanceled))
			pending, err := s.GetPending(ctx, nodes[0].ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(pending).To(BeEmpty())

			Expect(sched.Stop(ctx, b.ID)).To(MatchError(store.ErrBatchNotRunning))
		})

		It("returns ErrBatchNotFound for an unknown batch", func() {
			Expect(sched.Stop(ctx, uuid.NewString())).To(MatchError(store.ErrBatchNotFound))
		})
	})

	It("lets exactly one of two concurrent starts for the same node succeed", func() {
		for i := 0; i < 10; i++ {
			nodes := onlineNodes(1)
			gate := make(chan struct{})
			errs := make([]error, 2)
			var wg sync.WaitGroup
			for j := range errs {
				wg.Add(1)
				go func() {
					defer wg.Done()
					<-gate
					_, errs[j] = sched.Start(ctx, request(store.CmdExec, 1, nodes[0]))
				}()
			}
			close(gate)
			wg.Wait()

			succeeded := 0
			for _, err := range errs {
				if err == nil {
					succeeded++
					continue
				}
				var busy *batches.ErrNodesBusy
				Expect(errors.As(err, &busy)).To(BeTrue(), "unexpected error: %v", err)
			}
			Expect(succeeded).To(Equal(1))
		}
	})
})
