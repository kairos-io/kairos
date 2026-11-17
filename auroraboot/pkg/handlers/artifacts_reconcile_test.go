package handlers_test

import (
	"context"
	"time"

	"github.com/kairos-io/AuroraBoot/pkg/handlers"
	"github.com/kairos-io/AuroraBoot/pkg/store"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ReconcileOrphanedArtifacts", func() {
	var artifacts *fakeArtifactStore

	BeforeEach(func() {
		artifacts = &fakeArtifactStore{}
	})

	It("flips Pending and Building rows to Error and leaves Ready untouched", func() {
		ctx := context.Background()
		originalUpdatedAt := time.Now().Add(-time.Hour)

		Expect(artifacts.Create(ctx, &store.ArtifactRecord{
			ID:        "art-pending",
			Phase:     store.ArtifactPending,
			UpdatedAt: originalUpdatedAt,
		})).To(Succeed())
		Expect(artifacts.Create(ctx, &store.ArtifactRecord{
			ID:        "art-building",
			Phase:     store.ArtifactBuilding,
			Message:   "building layer 3",
			UpdatedAt: originalUpdatedAt,
		})).To(Succeed())
		Expect(artifacts.Create(ctx, &store.ArtifactRecord{
			ID:        "art-ready",
			Phase:     store.ArtifactReady,
			Message:   "done",
			UpdatedAt: originalUpdatedAt,
		})).To(Succeed())

		Expect(handlers.ReconcileOrphanedArtifacts(ctx, artifacts)).To(Succeed())

		pending, err := artifacts.GetByID(ctx, "art-pending")
		Expect(err).NotTo(HaveOccurred())
		Expect(pending.Phase).To(Equal(store.ArtifactError))
		Expect(pending.Message).To(Equal("interrupted by server restart"))
		Expect(pending.UpdatedAt).To(BeTemporally(">", originalUpdatedAt))

		building, err := artifacts.GetByID(ctx, "art-building")
		Expect(err).NotTo(HaveOccurred())
		Expect(building.Phase).To(Equal(store.ArtifactError))
		Expect(building.Message).To(Equal("interrupted by server restart"))
		Expect(building.UpdatedAt).To(BeTemporally(">", originalUpdatedAt))

		ready, err := artifacts.GetByID(ctx, "art-ready")
		Expect(err).NotTo(HaveOccurred())
		Expect(ready.Phase).To(Equal(store.ArtifactReady))
		Expect(ready.Message).To(Equal("done"))
		Expect(ready.UpdatedAt).To(Equal(originalUpdatedAt))
	})
})
