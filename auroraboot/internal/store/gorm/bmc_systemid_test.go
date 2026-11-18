package gorm_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	gormstore "github.com/kairos-io/AuroraBoot/internal/store/gorm"
	"github.com/kairos-io/AuroraBoot/pkg/store"
)

var _ = Describe("BMCTarget SystemID persistence", func() {
	var (
		ctx context.Context
		s   *gormstore.Store
	)

	BeforeEach(func() {
		ctx = context.Background()
		var err error
		s, err = gormstore.New(":memory:")
		Expect(err).NotTo(HaveOccurred())
	})

	It("persists SystemID on Create and round-trips it on read", func() {
		t := &store.BMCTarget{Name: "multi", Endpoint: "https://10.0.0.9", SystemID: "node-b"}
		Expect(s.BMCTargetCreate(ctx, t)).To(Succeed())

		got, err := s.BMCTargetGetByID(ctx, t.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(got.SystemID).To(Equal("node-b"))
	})

	It("persists an updated SystemID", func() {
		t := &store.BMCTarget{Name: "multi", Endpoint: "https://10.0.0.9"}
		Expect(s.BMCTargetCreate(ctx, t)).To(Succeed())
		Expect(t.SystemID).To(BeEmpty())

		t.SystemID = "node-a"
		Expect(s.BMCTargetUpdate(ctx, t)).To(Succeed())

		got, err := s.BMCTargetGetByID(ctx, t.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(got.SystemID).To(Equal("node-a"))
	})

	It("defaults to an empty SystemID for single-system BMCs", func() {
		t := &store.BMCTarget{Name: "single", Endpoint: "https://10.0.0.1"}
		Expect(s.BMCTargetCreate(ctx, t)).To(Succeed())

		got, err := s.BMCTargetGetByID(ctx, t.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(got.SystemID).To(BeEmpty())
	})
})
