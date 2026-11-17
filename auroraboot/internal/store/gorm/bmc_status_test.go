package gorm_test

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	gormstore "github.com/kairos-io/AuroraBoot/internal/store/gorm"
	"github.com/kairos-io/AuroraBoot/pkg/store"
)

var _ = Describe("BMCTarget status-cache persistence", func() {
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

	It("persists every status-cache field on Update and round-trips it on read", func() {
		t := &store.BMCTarget{Name: "h", Endpoint: "https://10.0.0.9"}
		Expect(s.BMCTargetCreate(ctx, t)).To(Succeed())

		inspectAt := time.Now().UTC().Truncate(time.Second)
		pingAt := inspectAt.Add(time.Minute)
		t.LastStatus = "reachable"
		t.LastError = ""
		t.LastInspectAt = &inspectAt
		t.LastPingAt = &pingAt
		t.LastModel = "PowerEdge R760"
		t.LastManufacturer = "Dell"
		t.LastSerial = "SN-123"
		t.LastMemoryGiB = 256
		t.LastCPUCount = 64
		t.LastFeatures = []string{"SecureBoot", "UEFI"}
		Expect(s.BMCTargetUpdate(ctx, t)).To(Succeed())

		got, err := s.BMCTargetGetByID(ctx, t.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(got.LastStatus).To(Equal("reachable"))
		Expect(got.LastError).To(BeEmpty())
		Expect(got.LastInspectAt).NotTo(BeNil())
		Expect(got.LastInspectAt.UTC()).To(Equal(inspectAt))
		Expect(got.LastPingAt).NotTo(BeNil())
		Expect(got.LastPingAt.UTC()).To(Equal(pingAt))
		Expect(got.LastModel).To(Equal("PowerEdge R760"))
		Expect(got.LastManufacturer).To(Equal("Dell"))
		Expect(got.LastSerial).To(Equal("SN-123"))
		Expect(got.LastMemoryGiB).To(Equal(256))
		Expect(got.LastCPUCount).To(Equal(64))
	})

	It("round-trips LastFeatures through the json serializer", func() {
		t := &store.BMCTarget{Name: "h", Endpoint: "https://10.0.0.9"}
		Expect(s.BMCTargetCreate(ctx, t)).To(Succeed())

		t.LastFeatures = []string{"SecureBoot", "UEFI"}
		Expect(s.BMCTargetUpdate(ctx, t)).To(Succeed())

		got, err := s.BMCTargetGetByID(ctx, t.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(got.LastFeatures).To(Equal([]string{"SecureBoot", "UEFI"}))

		// And the list path decodes it too.
		all, err := s.BMCTargetList(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(all).To(HaveLen(1))
		Expect(all[0].LastFeatures).To(Equal([]string{"SecureBoot", "UEFI"}))
	})

	It("defaults status-cache fields to zero for a fresh target", func() {
		t := &store.BMCTarget{Name: "fresh", Endpoint: "https://10.0.0.1"}
		Expect(s.BMCTargetCreate(ctx, t)).To(Succeed())

		got, err := s.BMCTargetGetByID(ctx, t.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(got.LastStatus).To(BeEmpty())
		Expect(got.LastInspectAt).To(BeNil())
		Expect(got.LastPingAt).To(BeNil())
		Expect(got.LastFeatures).To(BeEmpty())
	})
})
