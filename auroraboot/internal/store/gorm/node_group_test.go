package gorm_test

import (
	"context"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	gormstore "github.com/kairos-io/AuroraBoot/internal/store/gorm"
	"github.com/kairos-io/AuroraBoot/pkg/store"
)

var _ = Describe("Gorm Store ungrouped nodes", func() {
	var (
		dbPath string
		ctx    context.Context
	)

	BeforeEach(func() {
		dbPath = filepath.Join(GinkgoT().TempDir(), "groups.db")
		ctx = context.Background()
	})

	// SQLite does not enforce the foreign key on group_id, so a database can
	// hold ungrouped nodes with an empty group_id. Opening the store turns
	// them into NULL, the only form PostgreSQL accepts and the one the model
	// reads as no group.
	It("reads a node stored with an empty group id as ungrouped", func() {
		s, err := gormstore.New(dbPath)
		Expect(err).NotTo(HaveOccurred())
		n := &store.ManagedNode{MachineID: "m1"}
		Expect(s.Register(ctx, n)).To(Succeed())
		Expect(s.UnsafeDB().Exec("UPDATE managed_nodes SET group_id = '' WHERE id = ?", n.ID).Error).To(Succeed())
		Expect(s.Close()).To(Succeed())

		s, err = gormstore.New(dbPath)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { Expect(s.Close()).To(Succeed()) })

		got, err := s.NodeGetByID(ctx, n.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(got.GroupID).To(BeNil())
	})
})
