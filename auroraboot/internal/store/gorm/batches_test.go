package gorm_test

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	gormstore "github.com/kairos-io/AuroraBoot/internal/store/gorm"
	"github.com/kairos-io/AuroraBoot/pkg/store"
)

var _ = Describe("Gorm batch store", func() {
	var (
		s   *gormstore.Store
		ctx context.Context
	)

	BeforeEach(func() {
		var err error
		s, err = gormstore.New(filepath.Join(GinkgoT().TempDir(), "batches.db"))
		Expect(err).NotTo(HaveOccurred())
		ctx = context.Background()
	})

	AfterEach(func() { Expect(s.Close()).To(Succeed()) })

	waiting := func(batchID, nodeID string, position int) *store.BatchNode {
		return &store.BatchNode{BatchID: batchID, NodeID: nodeID, Position: position, State: store.BatchNodeWaiting}
	}

	create := func(id string, groupID *string, nodes ...*store.BatchNode) *store.CommandBatch {
		b := &store.CommandBatch{ID: id, Command: store.CmdUpgrade, GroupID: groupID, Concurrency: 1}
		Expect(s.BatchCreate(ctx, b, nodes)).To(Succeed())
		return b
	}

	It("stores a batch and returns its nodes in start order with counts", func() {
		create("b1", nil, waiting("b1", "n3", 2), waiting("b1", "n1", 0), waiting("b1", "n2", 1))

		got, err := s.BatchGet(ctx, "b1")
		Expect(err).NotTo(HaveOccurred())
		Expect(got.Phase).To(Equal(store.BatchRunning))
		Expect(got.Nodes).To(HaveLen(3))
		Expect([]string{got.Nodes[0].NodeID, got.Nodes[1].NodeID, got.Nodes[2].NodeID}).To(Equal([]string{"n1", "n2", "n3"}))
		Expect(got.Counts).To(Equal(map[string]int{"waiting": 3}))
	})

	It("returns ErrBatchNotFound for an unknown batch", func() {
		_, err := s.BatchGet(ctx, "missing")
		Expect(err).To(MatchError(store.ErrBatchNotFound))
	})

	It("lists batches newest first and filters them by group and by node", func() {
		g1 := "g1"
		create("b1", &g1, waiting("b1", "n0", 0))
		time.Sleep(5 * time.Millisecond)
		create("b2", nil, waiting("b2", "n1", 0))
		time.Sleep(5 * time.Millisecond)
		create("b3", nil, waiting("b3", "n2", 0))

		all, total, err := s.BatchList(ctx, store.BatchFilter{})
		Expect(err).NotTo(HaveOccurred())
		Expect(total).To(Equal(3))
		Expect([]string{all[0].ID, all[1].ID, all[2].ID}).To(Equal([]string{"b3", "b2", "b1"}))
		Expect(all[0].Nodes).To(BeEmpty())
		Expect(all[0].Counts).To(Equal(map[string]int{"waiting": 1}))

		byGroup, total, err := s.BatchList(ctx, store.BatchFilter{GroupID: "g1"})
		Expect(err).NotTo(HaveOccurred())
		Expect(total).To(Equal(1))
		Expect(byGroup[0].ID).To(Equal("b1"))

		byNode, total, err := s.BatchList(ctx, store.BatchFilter{NodeID: "n1"})
		Expect(err).NotTo(HaveOccurred())
		Expect(total).To(Equal(1))
		Expect(byNode[0].ID).To(Equal("b2"))

		page, total, err := s.BatchList(ctx, store.BatchFilter{Limit: 2})
		Expect(err).NotTo(HaveOccurred())
		Expect(page).To(HaveLen(2))
		Expect(total).To(Equal(3))
	})

	It("lists each batch with the hostnames of its first nodes in start order", func() {
		var rows []*store.BatchNode
		for i := 7; i >= 0; i-- {
			row := waiting("b1", fmt.Sprintf("n%d", i), i)
			row.Hostname = fmt.Sprintf("host-%d", i)
			rows = append(rows, row)
		}
		create("b1", nil, rows...)
		create("b2", nil, &store.BatchNode{BatchID: "b2", NodeID: "n9", Hostname: "host-9", State: store.BatchNodeWaiting})

		list, _, err := s.BatchList(ctx, store.BatchFilter{})
		Expect(err).NotTo(HaveOccurred())
		byID := map[string]*store.CommandBatch{list[0].ID: list[0], list[1].ID: list[1]}
		Expect(byID["b1"].Hostnames).To(Equal([]string{"host-0", "host-1", "host-2", "host-3", "host-4"}))
		Expect(byID["b2"].Hostnames).To(Equal([]string{"host-9"}))
	})

	It("reports nodes with a row that is not final as busy", func() {
		create("b1", nil, waiting("b1", "n1", 0), waiting("b1", "n2", 1), waiting("b1", "n3", 2))
		for node, state := range map[string]string{"n2": store.BatchNodeRebooting, "n3": store.BatchNodeDone} {
			Expect(s.UnsafeDB().Model(&store.BatchNode{}).
				Where("batch_id = ? AND node_id = ?", "b1", node).
				Update("state", state).Error).To(Succeed())
		}

		busy, err := s.BatchBusyNodes(ctx, []string{"n1", "n2", "n3", "n4"})
		Expect(err).NotTo(HaveOccurred())
		Expect(busy).To(ConsistOf("n1", "n2"))
	})

	It("classifies final states and reboot commands", func() {
		Expect(store.IsFinalBatchNodeState(store.BatchNodeDone)).To(BeTrue())
		Expect(store.IsFinalBatchNodeState(store.BatchNodeRebooting)).To(BeFalse())
		Expect(store.RebootsNode(store.CmdUpgrade)).To(BeTrue())
		Expect(store.RebootsNode("exec")).To(BeFalse())
	})
})
