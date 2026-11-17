package gorm_test

import (
	"context"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	gormstore "github.com/kairos-io/AuroraBoot/internal/store/gorm"
	"github.com/kairos-io/AuroraBoot/pkg/store"
)

// group_id references node_groups, and PostgreSQL enforces that foreign key
// where SQLite does not. A node outside any group has to satisfy it too.
var _ = Describe("Gorm Store ungrouped nodes on PostgreSQL", func() {
	var (
		s   *gormstore.Store
		ctx context.Context
	)

	BeforeEach(func() {
		ctx = context.Background()
		s = newPostgresStore()
	})

	group := func() *store.NodeGroup {
		g := &store.NodeGroup{Name: uuid.NewString()}
		Expect(s.Create(ctx, g)).To(Succeed())
		return g
	}

	register := func(groupID *string) *store.ManagedNode {
		n := &store.ManagedNode{MachineID: uuid.NewString(), GroupID: groupID}
		Expect(s.Register(ctx, n)).To(Succeed())
		return n
	}

	groupOf := func(id string) *string {
		got, err := s.NodeGetByID(ctx, id)
		Expect(err).NotTo(HaveOccurred())
		return got.GroupID
	}

	It("registers a node without a group", func() {
		n := register(nil)

		Expect(groupOf(n.ID)).To(BeNil())
	})

	It("saves an ungrouped node on heartbeat and label changes", func() {
		n := register(nil)

		Expect(s.UpdateHeartbeat(ctx, n.ID, "v1", nil, nil, "active", "host", "10.0.0.1")).To(Succeed())
		Expect(s.SetLabels(ctx, n.ID, map[string]string{"role": "worker"})).To(Succeed())

		Expect(groupOf(n.ID)).To(BeNil())
	})

	It("moves a node out of its group", func() {
		n := register(&group().ID)

		Expect(s.SetGroup(ctx, n.ID, nil)).To(Succeed())

		Expect(groupOf(n.ID)).To(BeNil())
	})

	It("detaches the member nodes of a deleted group", func() {
		g := group()
		n := register(&g.ID)

		Expect(s.Delete(ctx, g.ID)).To(Succeed())

		Expect(groupOf(n.ID)).To(BeNil())
	})

	It("counts grouped nodes only", func() {
		g := group()
		register(&g.ID)
		register(nil)

		counts, err := s.GroupNodeCounts(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(counts).To(Equal(map[string]int{g.ID: 1}))
	})
})
