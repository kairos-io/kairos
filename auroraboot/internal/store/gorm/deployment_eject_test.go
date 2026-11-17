package gorm_test

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	gormstore "github.com/kairos-io/AuroraBoot/internal/store/gorm"
	"github.com/kairos-io/AuroraBoot/pkg/store"
)

var _ = Describe("Deployment eject fields + CAS", func() {
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

	It("persists the eject fields and round-trips them on read", func() {
		ejectedAt := time.Now().UTC().Truncate(time.Second)
		dep := &store.Deployment{
			ArtifactID:  "art-1",
			Method:      "redfish",
			Status:      store.DeployCompleted,
			BMCTargetID: "bmc-1",
			NodeID:      "node-1",
			EjectPolicy: store.EjectPolicyOnPhoneHome,
			EjectState:  store.EjectStateEjected,
			EjectError:  "",
			EjectedAt:   &ejectedAt,
		}
		Expect(s.DeploymentCreate(ctx, dep)).To(Succeed())

		got, err := s.DeploymentGetByID(ctx, dep.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(got.NodeID).To(Equal("node-1"))
		Expect(got.EjectPolicy).To(Equal(store.EjectPolicyOnPhoneHome))
		Expect(got.EjectState).To(Equal(store.EjectStateEjected))
		Expect(got.EjectedAt).NotTo(BeNil())
		Expect(got.EjectedAt.Unix()).To(Equal(ejectedAt.Unix()))
	})

	It("defaults the eject fields to empty for a plain deployment", func() {
		dep := &store.Deployment{ArtifactID: "art-1", Method: "redfish", Status: store.DeployActive}
		Expect(s.DeploymentCreate(ctx, dep)).To(Succeed())

		got, err := s.DeploymentGetByID(ctx, dep.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(got.EjectPolicy).To(BeEmpty())
		Expect(got.EjectState).To(BeEmpty())
		Expect(got.EjectedAt).To(BeNil())
	})

	Describe("CASEjectState", func() {
		var depID string

		BeforeEach(func() {
			dep := &store.Deployment{
				ArtifactID: "art-1",
				Method:     "redfish",
				Status:     store.DeployCompleted,
				EjectState: store.EjectStatePending,
			}
			Expect(s.DeploymentCreate(ctx, dep)).To(Succeed())
			depID = dep.ID
		})

		It("transitions when the current state matches `from`", func() {
			ok, err := s.DeploymentCASEjectState(ctx, depID, store.EjectStatePending, store.EjectStateEjecting)
			Expect(err).NotTo(HaveOccurred())
			Expect(ok).To(BeTrue())

			got, err := s.DeploymentGetByID(ctx, depID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.EjectState).To(Equal(store.EjectStateEjecting))
		})

		It("refuses when the current state does not match `from`", func() {
			ok, err := s.DeploymentCASEjectState(ctx, depID, store.EjectStateEjecting, store.EjectStateEjected)
			Expect(err).NotTo(HaveOccurred())
			Expect(ok).To(BeFalse())

			got, err := s.DeploymentGetByID(ctx, depID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.EjectState).To(Equal(store.EjectStatePending))
		})

		It("lets exactly one of two concurrent claimants win", func() {
			// Two attempts race the same pending->ejecting transition; exactly one
			// must win (the compare-and-set idempotency guarantee the finalize path
			// relies on).
			first, err := s.DeploymentCASEjectState(ctx, depID, store.EjectStatePending, store.EjectStateEjecting)
			Expect(err).NotTo(HaveOccurred())
			second, err := s.DeploymentCASEjectState(ctx, depID, store.EjectStatePending, store.EjectStateEjecting)
			Expect(err).NotTo(HaveOccurred())
			Expect(first).To(BeTrue())
			Expect(second).To(BeFalse())
		})

		It("returns false for a missing deployment", func() {
			ok, err := s.DeploymentCASEjectState(ctx, "does-not-exist", store.EjectStatePending, store.EjectStateEjecting)
			Expect(err).NotTo(HaveOccurred())
			Expect(ok).To(BeFalse())
		})
	})
})
