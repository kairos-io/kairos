package gorm_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	gormstore "github.com/kairos-io/AuroraBoot/internal/store/gorm"
	"github.com/kairos-io/AuroraBoot/pkg/store"
)

var _ = Describe("BMCTarget ImageURL persistence", func() {
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

	It("persists ImageURL on Create and round-trips it on read", func() {
		t := &store.BMCTarget{Name: "host", Endpoint: "https://10.0.0.9", ImageURL: "https://10.0.0.5/os.iso"}
		Expect(s.BMCTargetCreate(ctx, t)).To(Succeed())

		got, err := s.BMCTargetGetByID(ctx, t.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(got.ImageURL).To(Equal("https://10.0.0.5/os.iso"))
	})

	It("persists an updated ImageURL", func() {
		t := &store.BMCTarget{Name: "host", Endpoint: "https://10.0.0.9"}
		Expect(s.BMCTargetCreate(ctx, t)).To(Succeed())
		Expect(t.ImageURL).To(BeEmpty())

		t.ImageURL = "https://10.0.0.6/os.iso"
		Expect(s.BMCTargetUpdate(ctx, t)).To(Succeed())

		got, err := s.BMCTargetGetByID(ctx, t.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(got.ImageURL).To(Equal("https://10.0.0.6/os.iso"))
	})

	It("defaults to an empty ImageURL", func() {
		t := &store.BMCTarget{Name: "host", Endpoint: "https://10.0.0.1"}
		Expect(s.BMCTargetCreate(ctx, t)).To(Succeed())

		got, err := s.BMCTargetGetByID(ctx, t.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(got.ImageURL).To(BeEmpty())
	})
})
