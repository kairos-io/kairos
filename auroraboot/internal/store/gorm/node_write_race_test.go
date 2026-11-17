package gorm_test

import (
	"context"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm"

	gormstore "github.com/kairos-io/AuroraBoot/internal/store/gorm"
	"github.com/kairos-io/AuroraBoot/pkg/store"
)

// A heartbeat or a label edit arrives while the node's upgrade is settled by
// another request. Whatever the writer read of the node before that settles
// must not be written back over it.
var _ = Describe("Gorm Store node writes racing a lifecycle change", func() {
	var (
		s   *gormstore.Store
		ctx context.Context
	)

	BeforeEach(func() {
		var err error
		s, err = gormstore.New(filepath.Join(GinkgoT().TempDir(), "race.db"))
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { Expect(s.Close()).To(Succeed()) })
		ctx = context.Background()
	})

	// meanwhile runs fn once, at the writer's first statement on
	// managed_nodes: after a read, where a read-modify-write writer holds the
	// copy it will save, or before a write, for a writer that does not read.
	meanwhile := func(fn func()) {
		armed := true
		fire := func(db *gorm.DB) {
			if !armed || db.DryRun || db.Statement.Table != "managed_nodes" {
				return
			}
			armed = false
			fn()
		}
		cb := s.UnsafeDB().Callback()
		Expect(cb.Query().After("gorm:query").Register("test:meanwhile", fire)).To(Succeed())
		Expect(cb.Update().Before("gorm:update").Register("test:meanwhile", fire)).To(Succeed())
	}

	DescribeTable("keeps an upgrade failure that commits while the writer runs",
		func(write func(nodeID string) error) {
			n := &store.ManagedNode{MachineID: "m1"}
			Expect(s.Register(ctx, n)).To(Succeed())
			cmd := issueCommand(ctx, s, n.ID, store.CmdUpgrade)
			Expect(s.UpdateStatus(ctx, cmd, store.CommandRunning, "")).To(Succeed())

			meanwhile(func() {
				Expect(s.UpdateStatus(ctx, cmd, store.CommandFailed, "boom")).To(Succeed())
			})
			Expect(write(n.ID)).To(Succeed())

			got, err := s.NodeGetByID(ctx, n.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.UpgradeState).To(Equal(store.UpgradeStateFailed))
		},
		Entry("heartbeat", func(id string) error {
			return s.UpdateHeartbeat(ctx, id, "v2", map[string]string{"ID": "kairos"}, nil, "active", "host", "10.0.0.1")
		}),
		Entry("label edit", func(id string) error {
			return s.SetLabels(ctx, id, map[string]string{"role": "worker"})
		}),
	)
})
