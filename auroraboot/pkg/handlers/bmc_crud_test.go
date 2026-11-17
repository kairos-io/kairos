package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/AuroraBoot/pkg/handlers"
	"github.com/kairos-io/AuroraBoot/pkg/store"
	"github.com/labstack/echo/v4"
)

var _ = Describe("BMC target CRUD ImageURL validation", func() {
	var (
		e          *echo.Echo
		bmcTargets *fakeBMCTargetStore
		h          *handlers.DeployHandler
	)

	BeforeEach(func() {
		e = echo.New()
		bmcTargets = &fakeBMCTargetStore{}
		h = handlers.NewDeployHandler(&fakeArtifactStore{}, &fakeDeploymentStore{}, bmcTargets, nil, "", nil, nil)
	})

	create := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/bmc-targets", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		Expect(h.CreateBMCTarget(c)).To(Succeed())
		return rec
	}

	update := func(id, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPut, "/api/v1/bmc-targets/"+id, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetParamNames("id")
		c.SetParamValues(id)
		Expect(h.UpdateBMCTarget(c)).To(Succeed())
		return rec
	}

	It("accepts and persists a valid per-BMC ImageURL on create", func() {
		rec := create(`{"name":"h","endpoint":"https://10.0.0.9","imageUrl":"https://10.0.0.5/os.iso"}`)
		Expect(rec.Code).To(Equal(http.StatusCreated))
		Expect(bmcTargets.targets).To(HaveLen(1))
		Expect(bmcTargets.targets[0].ImageURL).To(Equal("https://10.0.0.5/os.iso"))
	})

	It("rejects an SSRF-blocked ImageURL on create with 400", func() {
		rec := create(`{"name":"h","endpoint":"https://10.0.0.9","imageUrl":"http://169.254.169.254/x.iso"}`)
		Expect(rec.Code).To(Equal(http.StatusBadRequest))
		Expect(rec.Body.String()).To(ContainSubstring("invalid imageUrl"))
		Expect(bmcTargets.targets).To(BeEmpty())
	})

	It("rejects an SSRF-blocked ImageURL on update with 400", func() {
		bmcTargets.targets = []*store.BMCTarget{{ID: "bmc-1", Endpoint: "https://10.0.0.9"}}
		rec := update("bmc-1", `{"name":"h","endpoint":"https://10.0.0.9","imageUrl":"http://169.254.169.254/x.iso"}`)
		Expect(rec.Code).To(Equal(http.StatusBadRequest))
		Expect(rec.Body.String()).To(ContainSubstring("invalid imageUrl"))
		// The stored target is unchanged.
		Expect(bmcTargets.targets[0].ImageURL).To(BeEmpty())
	})

	It("persists an updated ImageURL on update", func() {
		bmcTargets.targets = []*store.BMCTarget{{ID: "bmc-1", Endpoint: "https://10.0.0.9"}}
		rec := update("bmc-1", `{"name":"h","endpoint":"https://10.0.0.9","imageUrl":"https://10.0.0.5/os.iso"}`)
		Expect(rec.Code).To(Equal(http.StatusOK))
		Expect(bmcTargets.targets[0].ImageURL).To(Equal("https://10.0.0.5/os.iso"))
	})

	It("persists the EjectPowerCycle flag on update", func() {
		bmcTargets.targets = []*store.BMCTarget{{ID: "bmc-1", Endpoint: "https://10.0.0.9"}}
		rec := update("bmc-1", `{"name":"h","endpoint":"https://10.0.0.9","ejectPowerCycle":true}`)
		Expect(rec.Code).To(Equal(http.StatusOK))
		Expect(bmcTargets.targets[0].EjectPowerCycle).To(BeTrue())
	})

	It("stores the password sent on create and never returns it", func() {
		rec := create(`{"name":"h","endpoint":"https://10.0.0.9","username":"admin","password":"s3cret"}`)
		Expect(rec.Code).To(Equal(http.StatusCreated))
		Expect(bmcTargets.targets).To(HaveLen(1))
		Expect(bmcTargets.targets[0].Password).To(Equal("s3cret"))
		Expect(rec.Body.String()).NotTo(ContainSubstring("s3cret"))
		Expect(rec.Body.String()).NotTo(ContainSubstring("password"))
	})

	It("replaces the stored password when update sends a non-empty one", func() {
		bmcTargets.targets = []*store.BMCTarget{{ID: "bmc-1", Endpoint: "https://10.0.0.9", Password: "old"}}
		rec := update("bmc-1", `{"name":"h","endpoint":"https://10.0.0.9","password":"new-pass"}`)
		Expect(rec.Code).To(Equal(http.StatusOK))
		Expect(bmcTargets.targets[0].Password).To(Equal("new-pass"))
		Expect(rec.Body.String()).NotTo(ContainSubstring("new-pass"))
		Expect(rec.Body.String()).NotTo(ContainSubstring("password"))
	})

	It("keeps the stored password when update omits it", func() {
		bmcTargets.targets = []*store.BMCTarget{{ID: "bmc-1", Endpoint: "https://10.0.0.9", Password: "old"}}
		rec := update("bmc-1", `{"name":"h","endpoint":"https://10.0.0.9","password":""}`)
		Expect(rec.Code).To(Equal(http.StatusOK))
		Expect(bmcTargets.targets[0].Password).To(Equal("old"))
	})

	It("does not return stored passwords when listing", func() {
		bmcTargets.targets = []*store.BMCTarget{{ID: "bmc-1", Endpoint: "https://10.0.0.9", Password: "s3cret"}}
		req := httptest.NewRequest(http.MethodGet, "/api/v1/bmc-targets", nil)
		rec := httptest.NewRecorder()
		Expect(h.ListBMCTargets(e.NewContext(req, rec))).To(Succeed())
		Expect(rec.Code).To(Equal(http.StatusOK))
		Expect(rec.Body.String()).NotTo(ContainSubstring("s3cret"))
	})

	It("rejects an update with an empty endpoint and keeps the stored one", func() {
		bmcTargets.targets = []*store.BMCTarget{{ID: "bmc-1", Endpoint: "https://10.0.0.9"}}
		rec := update("bmc-1", `{"name":"h"}`)
		Expect(rec.Code).To(Equal(http.StatusBadRequest))
		Expect(rec.Body.String()).To(ContainSubstring("endpoint is required"))
		Expect(bmcTargets.targets[0].Endpoint).To(Equal("https://10.0.0.9"))
	})

	It("accepts a normal RFC1918 endpoint on create", func() {
		rec := create(`{"name":"h","endpoint":"https://192.168.1.50"}`)
		Expect(rec.Code).To(Equal(http.StatusCreated))
		Expect(bmcTargets.targets).To(HaveLen(1))
		Expect(bmcTargets.targets[0].Endpoint).To(Equal("https://192.168.1.50"))
	})

	It("rejects an SSRF-blocked metadata-IP endpoint on create with 400", func() {
		rec := create(`{"name":"h","endpoint":"https://169.254.169.254"}`)
		Expect(rec.Code).To(Equal(http.StatusBadRequest))
		Expect(rec.Body.String()).To(ContainSubstring("invalid endpoint"))
		Expect(bmcTargets.targets).To(BeEmpty())
	})

	It("rejects a loopback endpoint on create with 400", func() {
		rec := create(`{"name":"h","endpoint":"https://127.0.0.1"}`)
		Expect(rec.Code).To(Equal(http.StatusBadRequest))
		Expect(rec.Body.String()).To(ContainSubstring("invalid endpoint"))
		Expect(bmcTargets.targets).To(BeEmpty())
	})

	It("rejects an SSRF-blocked metadata-IP endpoint on update with 400", func() {
		bmcTargets.targets = []*store.BMCTarget{{ID: "bmc-1", Endpoint: "https://10.0.0.9"}}
		rec := update("bmc-1", `{"name":"h","endpoint":"https://169.254.169.254"}`)
		Expect(rec.Code).To(Equal(http.StatusBadRequest))
		Expect(rec.Body.String()).To(ContainSubstring("invalid endpoint"))
		// The stored target is unchanged.
		Expect(bmcTargets.targets[0].Endpoint).To(Equal("https://10.0.0.9"))
	})

	It("accepts a normal RFC1918 endpoint on update", func() {
		bmcTargets.targets = []*store.BMCTarget{{ID: "bmc-1", Endpoint: "https://10.0.0.9"}}
		rec := update("bmc-1", `{"name":"h","endpoint":"https://192.168.1.50"}`)
		Expect(rec.Code).To(Equal(http.StatusOK))
		Expect(bmcTargets.targets[0].Endpoint).To(Equal("https://192.168.1.50"))
	})

	It("ignores client-supplied status-cache fields on create (server-owned)", func() {
		rec := create(`{"name":"h","endpoint":"https://10.0.0.9","lastStatus":"reachable","lastModel":"FAKE","lastFeatures":["UEFI"]}`)
		Expect(rec.Code).To(Equal(http.StatusCreated))
		Expect(bmcTargets.targets).To(HaveLen(1))
		// A client cannot fabricate a cached status without an actual check.
		Expect(bmcTargets.targets[0].LastStatus).To(BeEmpty())
		Expect(bmcTargets.targets[0].LastModel).To(BeEmpty())
		Expect(bmcTargets.targets[0].LastFeatures).To(BeEmpty())
	})

	It("does not let an update overwrite the server-owned status cache", func() {
		bmcTargets.targets = []*store.BMCTarget{{
			ID: "bmc-1", Endpoint: "https://10.0.0.9", LastStatus: "reachable", LastModel: "REAL",
		}}
		rec := update("bmc-1", `{"name":"h","endpoint":"https://10.0.0.9","lastStatus":"unreachable","lastModel":"FAKE"}`)
		Expect(rec.Code).To(Equal(http.StatusOK))
		// The fixed-field copy never touches the status cache: it is preserved.
		Expect(bmcTargets.targets[0].LastStatus).To(Equal("reachable"))
		Expect(bmcTargets.targets[0].LastModel).To(Equal("REAL"))
	})
})
