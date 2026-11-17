package handlers_test

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"time"

	"github.com/kairos-io/AuroraBoot/pkg/handlers"
	"github.com/kairos-io/AuroraBoot/pkg/store"
	"github.com/labstack/echo/v4"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// newServiceRootServer stands up a plain-HTTP server that answers the Redfish
// ServiceRoot with a 200 + parseable body, so a session-free reachability ping
// succeeds. It records whether any POST (a session-create) was ever attempted.
func newServiceRootServer(posts *int32) *httptest.Server {
	var mu sync.Mutex
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && posts != nil {
			mu.Lock()
			*posts++
			mu.Unlock()
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"@odata.id":"/redfish/v1/","Id":"RootService"}`))
	}))
}

// closedEndpoint returns a URL whose listener has been closed, so a connection to
// it is refused — modelling an unreachable BMC.
func closedEndpoint() string {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	Expect(err).NotTo(HaveOccurred())
	url := "http://" + l.Addr().String()
	Expect(l.Close()).To(Succeed())
	return url
}

var _ = Describe("DeployHandler inspect persistence", func() {
	var (
		e          *echo.Echo
		bmcTargets *fakeBMCTargetStore
		bmc        *multiSystemBMC
	)

	BeforeEach(func() {
		e = echo.New()
		bmcTargets = &fakeBMCTargetStore{}
		// Single-system BMC so inspect succeeds without a SystemID.
		bmc = newMultiSystemBMC("node-a")
	})

	AfterEach(func() { bmc.Close() })

	inspect := func(targetID string) *httptest.ResponseRecorder {
		h := handlers.NewDeployHandler(nil, nil, bmcTargets, nil, "", nil, nil)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/bmc-targets/"+targetID+"/inspect", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetParamNames("id")
		c.SetParamValues(targetID)
		Expect(h.InspectHardware(c)).To(Succeed())
		return rec
	}

	It("persists reachable facts into the status cache on a successful inspect", func() {
		Expect(bmcTargets.Create(context.Background(), &store.BMCTarget{
			ID: "t-1", Name: "h", Endpoint: bmc.URL(), Username: "admin", Password: "p", VerifySSL: false,
		})).To(Succeed())

		rec := inspect("t-1")
		Expect(rec.Code).To(Equal(http.StatusOK))

		got, err := bmcTargets.GetByID(context.Background(), "t-1")
		Expect(err).NotTo(HaveOccurred())
		Expect(got.LastStatus).To(Equal("reachable"))
		Expect(got.LastError).To(BeEmpty())
		Expect(got.LastInspectAt).NotTo(BeNil())
		Expect(got.LastModel).To(Equal("ProLiant-node-a"))
		Expect(got.LastManufacturer).To(Equal("ACME"))
		Expect(got.LastSerial).To(Equal("SN-node-a"))
		Expect(got.LastMemoryGiB).To(Equal(64))
		Expect(got.LastCPUCount).To(Equal(8))
	})

	It("persists an unreachable status + scrubbed error when the connect fails", func() {
		Expect(bmcTargets.Create(context.Background(), &store.BMCTarget{
			ID: "t-bad", Name: "bad", Endpoint: closedEndpoint(), Username: "admin", Password: "p", VerifySSL: false,
		})).To(Succeed())

		rec := inspect("t-bad")
		Expect(rec.Code).To(Equal(http.StatusInternalServerError))

		got, err := bmcTargets.GetByID(context.Background(), "t-bad")
		Expect(err).NotTo(HaveOccurred())
		Expect(got.LastStatus).To(Equal("unreachable"))
		Expect(got.LastError).NotTo(BeEmpty())
		Expect(got.LastInspectAt).NotTo(BeNil())
		// Credentials never leak into the cached error.
		Expect(got.LastError).NotTo(ContainSubstring("admin"))
	})
})

var _ = Describe("DeployHandler.PingBMCTarget", func() {
	var (
		e          *echo.Echo
		bmcTargets *fakeBMCTargetStore
		h          *handlers.DeployHandler
	)

	BeforeEach(func() {
		e = echo.New()
		bmcTargets = &fakeBMCTargetStore{}
		h = handlers.NewDeployHandler(nil, nil, bmcTargets, nil, "", nil, nil)
	})

	ping := func(id string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/bmc-targets/"+id+"/status", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetParamNames("id")
		c.SetParamValues(id)
		Expect(h.PingBMCTarget(c)).To(Succeed())
		return rec
	}

	It("reports reachable and persists LastPingAt for a live ServiceRoot", func() {
		var posts int32
		srv := newServiceRootServer(&posts)
		defer srv.Close()

		Expect(bmcTargets.Create(context.Background(), &store.BMCTarget{
			ID: "t-up", Name: "up", Endpoint: srv.URL, Username: "admin", Password: "p", VerifySSL: false,
		})).To(Succeed())

		rec := ping("t-up")
		Expect(rec.Code).To(Equal(http.StatusOK))

		var resp struct {
			Status     string     `json:"status"`
			LastPingAt *time.Time `json:"lastPingAt"`
			Error      string     `json:"error"`
		}
		Expect(json.Unmarshal(rec.Body.Bytes(), &resp)).To(Succeed())
		Expect(resp.Status).To(Equal("reachable"))
		Expect(resp.LastPingAt).NotTo(BeNil())
		Expect(resp.Error).To(BeEmpty())

		// Session-free: the ping must never have POSTed a session-create.
		Expect(posts).To(Equal(int32(0)))

		got, err := bmcTargets.GetByID(context.Background(), "t-up")
		Expect(err).NotTo(HaveOccurred())
		Expect(got.LastStatus).To(Equal("reachable"))
		Expect(got.LastPingAt).NotTo(BeNil())
	})

	It("reports unreachable for a refused endpoint", func() {
		Expect(bmcTargets.Create(context.Background(), &store.BMCTarget{
			ID: "t-down", Name: "down", Endpoint: closedEndpoint(), Username: "admin", Password: "p", VerifySSL: false,
		})).To(Succeed())

		rec := ping("t-down")
		Expect(rec.Code).To(Equal(http.StatusOK))

		var resp struct {
			Status string `json:"status"`
			Error  string `json:"error"`
		}
		Expect(json.Unmarshal(rec.Body.Bytes(), &resp)).To(Succeed())
		Expect(resp.Status).To(Equal("unreachable"))
		Expect(resp.Error).NotTo(BeEmpty())

		got, err := bmcTargets.GetByID(context.Background(), "t-down")
		Expect(err).NotTo(HaveOccurred())
		Expect(got.LastStatus).To(Equal("unreachable"))
	})

	It("returns 404 for an unknown target", func() {
		rec := ping("nope")
		Expect(rec.Code).To(Equal(http.StatusNotFound))
	})
})

var _ = Describe("DeployHandler.RefreshAllBMCTargets", func() {
	var (
		e          *echo.Echo
		bmcTargets *fakeBMCTargetStore
		h          *handlers.DeployHandler
	)

	BeforeEach(func() {
		e = echo.New()
		bmcTargets = &fakeBMCTargetStore{}
		h = handlers.NewDeployHandler(nil, nil, bmcTargets, nil, "", nil, nil)
	})

	refresh := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/bmc-targets/refresh-all", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		Expect(h.RefreshAllBMCTargets(c)).To(Succeed())
		return rec
	}

	It("pings every target and returns the per-target results", func() {
		srv := newServiceRootServer(nil)
		defer srv.Close()

		Expect(bmcTargets.Create(context.Background(), &store.BMCTarget{ID: "a", Endpoint: srv.URL})).To(Succeed())
		Expect(bmcTargets.Create(context.Background(), &store.BMCTarget{ID: "b", Endpoint: closedEndpoint()})).To(Succeed())

		rec := refresh()
		Expect(rec.Code).To(Equal(http.StatusOK))

		var results []struct {
			ID         string     `json:"id"`
			Status     string     `json:"status"`
			LastPingAt *time.Time `json:"lastPingAt"`
		}
		Expect(json.Unmarshal(rec.Body.Bytes(), &results)).To(Succeed())
		Expect(results).To(HaveLen(2))

		byID := map[string]string{}
		for _, r := range results {
			byID[r.ID] = r.Status
		}
		Expect(byID["a"]).To(Equal("reachable"))
		Expect(byID["b"]).To(Equal("unreachable"))

		// Both outcomes were persisted into the cache.
		got, err := bmcTargets.GetByID(context.Background(), "a")
		Expect(err).NotTo(HaveOccurred())
		Expect(got.LastStatus).To(Equal("reachable"))
		Expect(got.LastPingAt).NotTo(BeNil())
	})

	It("returns 409 when a refresh is already in progress", func() {
		// A BMC whose ServiceRoot blocks until released holds the first refresh
		// in-flight; a second concurrent call must get 409.
		release := make(chan struct{})
		entered := make(chan struct{}, 1)
		var once sync.Once
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			once.Do(func() { entered <- struct{}{} })
			<-release
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"@odata.id":"/redfish/v1/"}`))
		}))
		defer srv.Close()

		Expect(bmcTargets.Create(context.Background(), &store.BMCTarget{ID: "slow", Endpoint: srv.URL})).To(Succeed())

		// Drive the first refresh in a goroutine; it blocks inside the ping.
		firstDone := make(chan int, 1)
		go func() {
			defer GinkgoRecover()
			req := httptest.NewRequest(http.MethodPost, "/api/v1/bmc-targets/refresh-all", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			Expect(h.RefreshAllBMCTargets(c)).To(Succeed())
			firstDone <- rec.Code
		}()

		// Wait until the first refresh is actually inside the ping (mutex held).
		Eventually(entered, 5*time.Second).Should(Receive())

		// A concurrent refresh must be rejected with 409.
		rec := refresh()
		Expect(rec.Code).To(Equal(http.StatusConflict))
		Expect(rec.Body.String()).To(ContainSubstring("already in progress"))

		// Let the first refresh finish; it succeeds with 200.
		close(release)
		Eventually(firstDone, 5*time.Second).Should(Receive(Equal(http.StatusOK)))

		// After it completes the guard is released: a fresh refresh works again.
		rec2 := refresh()
		Expect(rec2.Code).To(Equal(http.StatusOK))
	})
})
