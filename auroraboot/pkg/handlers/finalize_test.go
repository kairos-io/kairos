package handlers_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/AuroraBoot/pkg/handlers"
	"github.com/kairos-io/AuroraBoot/pkg/redfish"
	"github.com/kairos-io/AuroraBoot/pkg/store"
	"github.com/labstack/echo/v4"
)

// fakeFinalizer is an injectable redfish.Deployer stand-in for the eject/finalize
// path. It records that Finalize was called and against which endpoint, and can be
// made to fail Connect or Finalize to exercise the eject-failed path.
type fakeFinalizer struct {
	mu            sync.Mutex
	cfg           redfish.Config
	connectErr    error
	finalizeErr   error
	connected     bool
	finalizeCalls int
	// lastReq records the most recent FinalizeRequest so specs can assert the
	// PowerCycle flag was threaded through from the BMC target.
	lastReq redfish.FinalizeRequest
}

func (f *fakeFinalizer) Connect(_ context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.connectErr != nil {
		return f.connectErr
	}
	f.connected = true
	return nil
}

func (f *fakeFinalizer) Finalize(_ context.Context, req redfish.FinalizeRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.finalizeCalls++
	f.lastReq = req
	return f.finalizeErr
}

func (f *fakeFinalizer) lastPowerCycle() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastReq.PowerCycle
}

func (f *fakeFinalizer) Close() error { return nil }

func (f *fakeFinalizer) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.finalizeCalls
}

var _ = Describe("Eject / finalize", func() {
	var (
		deployments *fakeDeploymentStore
		bmcTargets  *fakeBMCTargetStore
		artifacts   *fakeArtifactStore
		fin         *fakeFinalizer
		h           *handlers.DeployHandler
	)

	BeforeEach(func() {
		deployments = &fakeDeploymentStore{}
		bmcTargets = &fakeBMCTargetStore{}
		artifacts = &fakeArtifactStore{}
		fin = &fakeFinalizer{}
		h = handlers.NewDeployHandler(artifacts, deployments, bmcTargets, nil, "", nil, nil).
			WithTestFinalizerFactory(func(cfg redfish.Config) handlers.RedfishFinalizer {
				fin.cfg = cfg
				return fin
			})
	})

	Describe("deploy completion arms the eject lifecycle", func() {
		It("marks the deployment pending-eject without ejecting inline", func() {
			dep := &store.Deployment{ID: "dep-1", Method: "redfish", Status: store.DeployCompleted, BMCTargetID: "bmc-1"}
			Expect(deployments.Create(context.Background(), dep)).To(Succeed())

			h.MarkEjectPendingForTest("dep-1")

			got, err := deployments.GetByID(context.Background(), "dep-1")
			Expect(err).NotTo(HaveOccurred())
			Expect(got.EjectState).To(Equal(store.EjectStatePending))
			Expect(got.EjectPolicy).To(Equal(store.EjectPolicyOnPhoneHome))
			// No inline eject ran.
			Expect(fin.calls()).To(Equal(0))
		})
	})

	Describe("MaybeFinalizeForNode", func() {
		It("finalizes a deployment linked via BMCTarget.NodeID and CAS-transitions to ejected", func() {
			Expect(bmcTargets.Create(context.Background(), &store.BMCTarget{ID: "bmc-1", Endpoint: "https://10.0.0.9", NodeID: "node-1"})).To(Succeed())
			Expect(deployments.Create(context.Background(), &store.Deployment{
				ID: "dep-1", Method: "redfish", Status: store.DeployCompleted,
				BMCTargetID: "bmc-1", EjectState: store.EjectStatePending,
			})).To(Succeed())

			h.MaybeFinalizeForNode(context.Background(), "node-1")

			Expect(fin.calls()).To(Equal(1))
			Expect(fin.cfg.Endpoint).To(Equal("https://10.0.0.9"))
			got, _ := deployments.GetByID(context.Background(), "dep-1")
			Expect(got.EjectState).To(Equal(store.EjectStateEjected))
			Expect(got.NodeID).To(Equal("node-1"))
			Expect(got.EjectedAt).NotTo(BeNil())
		})

		It("finalizes the sole unambiguous pending deployment when there is no explicit link and it completed within the window", func() {
			completed := time.Now().Add(-1 * time.Minute)
			Expect(bmcTargets.Create(context.Background(), &store.BMCTarget{ID: "bmc-1", Endpoint: "https://10.0.0.9"})).To(Succeed())
			Expect(deployments.Create(context.Background(), &store.Deployment{
				ID: "dep-1", Method: "redfish", Status: store.DeployCompleted,
				BMCTargetID: "bmc-1", EjectState: store.EjectStatePending, CompletedAt: &completed,
			})).To(Succeed())

			h.MaybeFinalizeForNode(context.Background(), "node-unlinked")

			Expect(fin.calls()).To(Equal(1))
			got, _ := deployments.GetByID(context.Background(), "dep-1")
			Expect(got.EjectState).To(Equal(store.EjectStateEjected))
		})

		It("does NOT fire the unlinked single-pending fallback when the sole deployment is older than the window", func() {
			// Completed well past ejectFallbackWindow (60m). A late phone-home is not
			// trusted to be the freshly provisioned node; it stays pending for the
			// manual Finalize endpoint.
			completed := time.Now().Add(-2 * time.Hour)
			Expect(bmcTargets.Create(context.Background(), &store.BMCTarget{ID: "bmc-1", Endpoint: "https://10.0.0.9"})).To(Succeed())
			Expect(deployments.Create(context.Background(), &store.Deployment{
				ID: "dep-1", Method: "redfish", Status: store.DeployCompleted,
				BMCTargetID: "bmc-1", EjectState: store.EjectStatePending, CompletedAt: &completed,
			})).To(Succeed())

			h.MaybeFinalizeForNode(context.Background(), "node-unlinked")

			// Fallback refused: no eject, deployment untouched.
			Expect(fin.calls()).To(Equal(0))
			got, _ := deployments.GetByID(context.Background(), "dep-1")
			Expect(got.EjectState).To(Equal(store.EjectStatePending))
		})

		It("does NOT fire the unlinked single-pending fallback when the sole deployment has no CompletedAt stamp", func() {
			// No completion stamp -> cannot prove recency -> out of window.
			Expect(bmcTargets.Create(context.Background(), &store.BMCTarget{ID: "bmc-1", Endpoint: "https://10.0.0.9"})).To(Succeed())
			Expect(deployments.Create(context.Background(), &store.Deployment{
				ID: "dep-1", Method: "redfish", Status: store.DeployCompleted,
				BMCTargetID: "bmc-1", EjectState: store.EjectStatePending,
			})).To(Succeed())

			h.MaybeFinalizeForNode(context.Background(), "node-unlinked")

			Expect(fin.calls()).To(Equal(0))
			got, _ := deployments.GetByID(context.Background(), "dep-1")
			Expect(got.EjectState).To(Equal(store.EjectStatePending))
		})

		It("honours the node<->BMC link regardless of age (linked path is not time-boxed)", func() {
			// Completed long ago, but legitimately linked to this node -> always ejects.
			completed := time.Now().Add(-72 * time.Hour)
			Expect(bmcTargets.Create(context.Background(), &store.BMCTarget{ID: "bmc-1", Endpoint: "https://10.0.0.9", NodeID: "node-1"})).To(Succeed())
			Expect(deployments.Create(context.Background(), &store.Deployment{
				ID: "dep-1", Method: "redfish", Status: store.DeployCompleted,
				BMCTargetID: "bmc-1", EjectState: store.EjectStatePending, CompletedAt: &completed,
			})).To(Succeed())

			h.MaybeFinalizeForNode(context.Background(), "node-1")

			Expect(fin.calls()).To(Equal(1))
			got, _ := deployments.GetByID(context.Background(), "dep-1")
			Expect(got.EjectState).To(Equal(store.EjectStateEjected))
		})

		It("refuses to auto-eject when there are multiple unlinked pending deployments (ambiguous)", func() {
			Expect(bmcTargets.Create(context.Background(), &store.BMCTarget{ID: "bmc-1", Endpoint: "https://10.0.0.9"})).To(Succeed())
			Expect(bmcTargets.Create(context.Background(), &store.BMCTarget{ID: "bmc-2", Endpoint: "https://10.0.0.10"})).To(Succeed())
			Expect(deployments.Create(context.Background(), &store.Deployment{
				ID: "dep-1", Method: "redfish", BMCTargetID: "bmc-1", EjectState: store.EjectStatePending,
			})).To(Succeed())
			Expect(deployments.Create(context.Background(), &store.Deployment{
				ID: "dep-2", Method: "redfish", BMCTargetID: "bmc-2", EjectState: store.EjectStatePending,
			})).To(Succeed())

			h.MaybeFinalizeForNode(context.Background(), "node-unlinked")

			// No eject: ambiguous correlation must be refused.
			Expect(fin.calls()).To(Equal(0))
			for _, id := range []string{"dep-1", "dep-2"} {
				got, _ := deployments.GetByID(context.Background(), id)
				Expect(got.EjectState).To(Equal(store.EjectStatePending))
			}
		})

		It("SECURITY: never finalizes a deployment a node is not linked to when another node's deployment is also pending", func() {
			// node-1 owns bmc-1/dep-1; bmc-2/dep-2 belongs to a DIFFERENT node. When
			// node-1 phones home it must eject ONLY dep-1, never dep-2. Because dep-1
			// is unambiguously linked, the unambiguous-single fallback does not even
			// engage — and dep-2 (linked to node-2) is untouched.
			Expect(bmcTargets.Create(context.Background(), &store.BMCTarget{ID: "bmc-1", Endpoint: "https://10.0.0.9", NodeID: "node-1"})).To(Succeed())
			Expect(bmcTargets.Create(context.Background(), &store.BMCTarget{ID: "bmc-2", Endpoint: "https://10.0.0.10", NodeID: "node-2"})).To(Succeed())
			Expect(deployments.Create(context.Background(), &store.Deployment{
				ID: "dep-1", Method: "redfish", BMCTargetID: "bmc-1", EjectState: store.EjectStatePending,
			})).To(Succeed())
			Expect(deployments.Create(context.Background(), &store.Deployment{
				ID: "dep-2", Method: "redfish", BMCTargetID: "bmc-2", EjectState: store.EjectStatePending,
			})).To(Succeed())

			h.MaybeFinalizeForNode(context.Background(), "node-1")

			Expect(fin.calls()).To(Equal(1))
			Expect(fin.cfg.Endpoint).To(Equal("https://10.0.0.9")) // bmc-1, never bmc-2
			d1, _ := deployments.GetByID(context.Background(), "dep-1")
			d2, _ := deployments.GetByID(context.Background(), "dep-2")
			Expect(d1.EjectState).To(Equal(store.EjectStateEjected))
			Expect(d2.EjectState).To(Equal(store.EjectStatePending)) // untouched
		})

		It("threads BMCTarget.EjectPowerCycle into the FinalizeRequest", func() {
			Expect(bmcTargets.Create(context.Background(), &store.BMCTarget{
				ID: "bmc-1", Endpoint: "https://10.0.0.9", NodeID: "node-1", EjectPowerCycle: true,
			})).To(Succeed())
			Expect(deployments.Create(context.Background(), &store.Deployment{
				ID: "dep-1", Method: "redfish", BMCTargetID: "bmc-1", EjectState: store.EjectStatePending,
			})).To(Succeed())

			h.MaybeFinalizeForNode(context.Background(), "node-1")

			Expect(fin.calls()).To(Equal(1))
			Expect(fin.lastPowerCycle()).To(BeTrue())
		})

		It("leaves FinalizeRequest.PowerCycle false when the BMC target does not request it", func() {
			Expect(bmcTargets.Create(context.Background(), &store.BMCTarget{
				ID: "bmc-1", Endpoint: "https://10.0.0.9", NodeID: "node-1",
			})).To(Succeed())
			Expect(deployments.Create(context.Background(), &store.Deployment{
				ID: "dep-1", Method: "redfish", BMCTargetID: "bmc-1", EjectState: store.EjectStatePending,
			})).To(Succeed())

			h.MaybeFinalizeForNode(context.Background(), "node-1")

			Expect(fin.calls()).To(Equal(1))
			Expect(fin.lastPowerCycle()).To(BeFalse())
		})

		It("records eject-failed with a scrubbed error when finalize fails", func() {
			fin.finalizeErr = context.DeadlineExceeded
			Expect(bmcTargets.Create(context.Background(), &store.BMCTarget{ID: "bmc-1", Endpoint: "https://10.0.0.9", NodeID: "node-1"})).To(Succeed())
			Expect(deployments.Create(context.Background(), &store.Deployment{
				ID: "dep-1", Method: "redfish", BMCTargetID: "bmc-1", EjectState: store.EjectStatePending,
			})).To(Succeed())

			h.MaybeFinalizeForNode(context.Background(), "node-1")

			got, _ := deployments.GetByID(context.Background(), "dep-1")
			Expect(got.EjectState).To(Equal(store.EjectStateFailed))
			Expect(got.EjectError).NotTo(BeEmpty())
		})

		It("CAS idempotency: two concurrent finalize attempts only finalize once", func() {
			Expect(bmcTargets.Create(context.Background(), &store.BMCTarget{ID: "bmc-1", Endpoint: "https://10.0.0.9", NodeID: "node-1"})).To(Succeed())
			Expect(deployments.Create(context.Background(), &store.Deployment{
				ID: "dep-1", Method: "redfish", BMCTargetID: "bmc-1", EjectState: store.EjectStatePending,
			})).To(Succeed())

			var wg sync.WaitGroup
			for i := 0; i < 8; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					h.MaybeFinalizeForNode(context.Background(), "node-1")
				}()
			}
			wg.Wait()

			// Exactly one winner reached the BMC despite 8 concurrent phone-homes.
			Expect(fin.calls()).To(Equal(1))
			got, _ := deployments.GetByID(context.Background(), "dep-1")
			Expect(got.EjectState).To(Equal(store.EjectStateEjected))
		})
	})

	Describe("manual POST /deployments/:id/finalize", func() {
		finalize := func(id string) *httptest.ResponseRecorder {
			e := echo.New()
			req := httptest.NewRequest(http.MethodPost, "/api/v1/deployments/"+id+"/finalize", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			c.SetParamNames("id")
			c.SetParamValues(id)
			Expect(h.FinalizeDeployment(c)).To(Succeed())
			return rec
		}

		It("ejects regardless of policy and returns the updated deployment", func() {
			Expect(bmcTargets.Create(context.Background(), &store.BMCTarget{ID: "bmc-1", Endpoint: "https://10.0.0.9"})).To(Succeed())
			// Policy off (EjectState empty): a manual finalize is an operator override.
			Expect(deployments.Create(context.Background(), &store.Deployment{
				ID: "dep-1", Method: "redfish", Status: store.DeployCompleted, BMCTargetID: "bmc-1",
			})).To(Succeed())

			rec := finalize("dep-1")
			Expect(rec.Code).To(Equal(http.StatusOK))
			Expect(fin.calls()).To(Equal(1))
			got, _ := deployments.GetByID(context.Background(), "dep-1")
			Expect(got.EjectState).To(Equal(store.EjectStateEjected))
		})

		It("404s for an unknown deployment", func() {
			rec := finalize("missing")
			Expect(rec.Code).To(Equal(http.StatusNotFound))
		})

		It("400s when the deployment has no BMC target", func() {
			Expect(deployments.Create(context.Background(), &store.Deployment{ID: "dep-1", Method: "redfish"})).To(Succeed())
			rec := finalize("dep-1")
			Expect(rec.Code).To(Equal(http.StatusBadRequest))
		})
	})

	Describe("POST /bmc-targets/:id/eject", func() {
		eject := func(id string) *httptest.ResponseRecorder {
			e := echo.New()
			req := httptest.NewRequest(http.MethodPost, "/api/v1/bmc-targets/"+id+"/eject", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			c.SetParamNames("id")
			c.SetParamValues(id)
			Expect(h.EjectBMCTarget(c)).To(Succeed())
			return rec
		}

		It("ejects directly with no deployment context", func() {
			Expect(bmcTargets.Create(context.Background(), &store.BMCTarget{ID: "bmc-1", Endpoint: "https://10.0.0.9"})).To(Succeed())
			rec := eject("bmc-1")
			Expect(rec.Code).To(Equal(http.StatusOK))
			Expect(fin.calls()).To(Equal(1))
			Expect(fin.cfg.Endpoint).To(Equal("https://10.0.0.9"))
		})

		It("404s for an unknown BMC target", func() {
			rec := eject("missing")
			Expect(rec.Code).To(Equal(http.StatusNotFound))
		})

		It("500s with a scrubbed error when the eject fails", func() {
			fin.finalizeErr = context.DeadlineExceeded
			Expect(bmcTargets.Create(context.Background(), &store.BMCTarget{ID: "bmc-1", Endpoint: "https://10.0.0.9"})).To(Succeed())
			rec := eject("bmc-1")
			Expect(rec.Code).To(Equal(http.StatusInternalServerError))
		})
	})

	Describe("reconcile flips ejecting -> pending", func() {
		It("resets an orphaned ejecting row to pending without auto-ejecting", func() {
			Expect(deployments.Create(context.Background(), &store.Deployment{
				ID: "dep-1", Method: "redfish", Status: store.DeployCompleted, EjectState: store.EjectStateEjecting,
			})).To(Succeed())
			// A terminal ejected row must be left alone.
			Expect(deployments.Create(context.Background(), &store.Deployment{
				ID: "dep-2", Method: "redfish", Status: store.DeployCompleted, EjectState: store.EjectStateEjected,
			})).To(Succeed())

			Expect(handlers.ReconcileOrphanedDeployments(context.Background(), deployments)).To(Succeed())

			d1, _ := deployments.GetByID(context.Background(), "dep-1")
			d2, _ := deployments.GetByID(context.Background(), "dep-2")
			Expect(d1.EjectState).To(Equal(store.EjectStatePending))
			Expect(d2.EjectState).To(Equal(store.EjectStateEjected))
			// Reconcile NEVER ejects.
			Expect(fin.calls()).To(Equal(0))
		})
	})
})
