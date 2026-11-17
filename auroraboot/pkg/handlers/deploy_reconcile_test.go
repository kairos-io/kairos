package handlers_test

import (
	"context"
	"time"

	"github.com/kairos-io/AuroraBoot/pkg/handlers"
	"github.com/kairos-io/AuroraBoot/pkg/store"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ReconcileOrphanedDeployments", func() {
	var deployments *fakeDeploymentStore

	BeforeEach(func() {
		deployments = &fakeDeploymentStore{}
	})

	It("flips a pre-seeded Active deployment to Failed", func() {
		started := time.Now().Add(-time.Hour)
		Expect(deployments.Create(context.Background(), &store.Deployment{
			ID:        "dep-active",
			Method:    "redfish",
			Status:    store.DeployActive,
			Progress:  30,
			StartedAt: started,
		})).To(Succeed())

		Expect(handlers.ReconcileOrphanedDeployments(context.Background(), deployments)).To(Succeed())

		got, err := deployments.GetByID(context.Background(), "dep-active")
		Expect(err).NotTo(HaveOccurred())
		Expect(got.Status).To(Equal(store.DeployFailed))
		Expect(got.Message).To(Equal("interrupted by server restart"))
		Expect(got.CompletedAt).NotTo(BeNil())
	})

	It("leaves terminal deployments untouched", func() {
		Expect(deployments.Create(context.Background(), &store.Deployment{
			ID:     "dep-done",
			Status: store.DeployCompleted,
		})).To(Succeed())
		Expect(deployments.Create(context.Background(), &store.Deployment{
			ID:     "dep-failed",
			Status: store.DeployFailed,
		})).To(Succeed())

		Expect(handlers.ReconcileOrphanedDeployments(context.Background(), deployments)).To(Succeed())

		done, _ := deployments.GetByID(context.Background(), "dep-done")
		Expect(done.Status).To(Equal(store.DeployCompleted))
		failed, _ := deployments.GetByID(context.Background(), "dep-failed")
		Expect(failed.Status).To(Equal(store.DeployFailed))
		Expect(failed.Message).To(BeEmpty())
	})
})
