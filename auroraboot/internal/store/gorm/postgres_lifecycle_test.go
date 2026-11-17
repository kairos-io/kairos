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

// rendezvous holds the first two callers until both have arrived, then lets
// every caller through. It gives up after a timeout so a store that
// serializes the callers some other way fails an assertion instead of
// hanging.
type rendezvous struct {
	arrived atomic.Int32
	both    chan struct{}
}

func newRendezvous() *rendezvous { return &rendezvous{both: make(chan struct{})} }

func (r *rendezvous) arrive() {
	switch r.arrived.Add(1) {
	case 1:
		select {
		case <-r.both:
		case <-time.After(5 * time.Second):
		}
	case 2:
		close(r.both)
	}
}

// SQLite runs one writer at a time, so the overlaps below can only happen on
// PostgreSQL.
var _ = Describe("Gorm Store lifecycle on PostgreSQL", func() {
	var (
		s     *gormstore.Store
		ctx   context.Context
		meet  *rendezvous
		armed atomic.Bool
	)

	BeforeEach(func() {
		ctx = context.Background()
		s = newPostgresStore()

		// Each transaction under test reaches managed_nodes only after it
		// wrote its command row. Holding both there makes them overlap at
		// the point where one has to see the other's outcome.
		meet = newRendezvous()
		armed.Store(false)
		hold := func(db *gorm.DB) {
			if armed.Load() && db.Statement.Table == "managed_nodes" {
				meet.arrive()
			}
		}
		cb := s.UnsafeDB().Callback()
		Expect(cb.Query().Before("gorm:query").Register("test:rendezvous", hold)).To(Succeed())
		Expect(cb.Update().Before("gorm:update").Register("test:rendezvous", hold)).To(Succeed())
	})

	register := func() *store.ManagedNode {
		n := &store.ManagedNode{MachineID: uuid.NewString()}
		Expect(s.Register(ctx, n)).To(Succeed())
		return n
	}

	upgradeState := func(id string) string {
		got, err := s.NodeGetByID(ctx, id)
		Expect(err).NotTo(HaveOccurred())
		return got.UpgradeState
	}

	together := func(fns ...func() error) {
		armed.Store(true)
		defer armed.Store(false)
		var wg sync.WaitGroup
		errs := make([]error, len(fns))
		for i, fn := range fns {
			wg.Add(1)
			go func() {
				defer wg.Done()
				errs[i] = fn()
			}()
		}
		wg.Wait()
		for _, err := range errs {
			Expect(err).NotTo(HaveOccurred())
		}
	}

	It("marks the node failed when its two upgrades fail at the same time", func() {
		n := register()
		a := issueCommand(ctx, s, n.ID, store.CmdUpgrade)
		b := issueCommand(ctx, s, n.ID, store.CmdUpgrade)
		Expect(s.UpdateStatus(ctx, a, store.CommandRunning, "")).To(Succeed())
		Expect(s.UpdateStatus(ctx, b, store.CommandRunning, "")).To(Succeed())

		together(
			func() error { return s.UpdateStatusForNode(ctx, a, n.ID, store.CommandFailed, "boom") },
			func() error { return s.UpdateStatusForNode(ctx, b, n.ID, store.CommandFailed, "boom") },
		)

		Expect(upgradeState(n.ID)).To(Equal(store.UpgradeStateFailed))
	})

	It("keeps the node pending when an upgrade is issued while the previous one fails", func() {
		n := register()
		a := issueCommand(ctx, s, n.ID, store.CmdUpgrade)
		Expect(s.UpdateStatus(ctx, a, store.CommandRunning, "")).To(Succeed())

		together(
			func() error { return s.UpdateStatusForNode(ctx, a, n.ID, store.CommandFailed, "boom") },
			func() error {
				return s.CreateBatch(ctx, []*store.NodeCommand{{ManagedNodeID: n.ID, Command: store.CmdUpgrade}})
			},
		)

		Expect(upgradeState(n.ID)).To(Equal(store.UpgradeStatePending))
	})
})
