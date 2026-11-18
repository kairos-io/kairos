package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/kairos-io/AuroraBoot/pkg/handlers"
	"github.com/kairos-io/AuroraBoot/pkg/store"
	"github.com/labstack/echo/v4"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// multiSystemBMC is a compact, spec-accurate Redfish service that exposes more
// than one ComputerSystem. It serves only what the inspect path exercises:
// a session-less ServiceRoot (so Connect's auto auth-mode falls back to Basic),
// the Systems collection with N members, and each ComputerSystem document. This
// lets a handler-level test prove the multi-system fail-safe: without a SystemID
// the Deployer refuses to guess (500); with the target's SystemID it targets the
// chosen system (no 500).
type multiSystemBMC struct {
	server    *httptest.Server
	systemIDs []string
}

func newMultiSystemBMC(systemIDs ...string) *multiSystemBMC {
	b := &multiSystemBMC{systemIDs: systemIDs}
	b.server = httptest.NewTLSServer(http.HandlerFunc(b.handle))
	return b
}

func (b *multiSystemBMC) Close()      { b.server.Close() }
func (b *multiSystemBMC) URL() string { return b.server.URL }

func (b *multiSystemBMC) knownSystem(id string) bool {
	for _, s := range b.systemIDs {
		if s == id {
			return true
		}
	}
	return false
}

func (b *multiSystemBMC) handle(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	enc := func(v any) { _ = json.NewEncoder(w).Encode(v) }

	if r.URL.Path == "/redfish/v1/" || r.URL.Path == "/redfish/v1" {
		// Session-less ServiceRoot: no SessionService/Links.Sessions, so the
		// Deployer's auto auth-mode pre-check falls back to HTTP Basic.
		enc(map[string]any{
			"@odata.id":      "/redfish/v1/",
			"@odata.type":    "#ServiceRoot.v1_5_0.ServiceRoot",
			"Id":             "RootService",
			"Name":           "Root Service",
			"RedfishVersion": "1.6.0",
			"Systems":        map[string]any{"@odata.id": "/redfish/v1/Systems"},
		})
		return
	}

	if r.URL.Path == "/redfish/v1/Systems" {
		members := make([]map[string]any, 0, len(b.systemIDs))
		for _, id := range b.systemIDs {
			members = append(members, map[string]any{"@odata.id": "/redfish/v1/Systems/" + id})
		}
		enc(map[string]any{
			"@odata.id":           "/redfish/v1/Systems",
			"@odata.type":         "#ComputerSystemCollection.ComputerSystemCollection",
			"Name":                "Systems Collection",
			"Members@odata.count": len(members),
			"Members":             members,
		})
		return
	}

	if id, ok := pathTail(r.URL.Path); ok && b.knownSystem(id) && r.Method == http.MethodGet {
		enc(map[string]any{
			"@odata.id":    "/redfish/v1/Systems/" + id,
			"@odata.type":  "#ComputerSystem.v1_5_0.ComputerSystem",
			"Id":           id,
			"Name":         "System " + id,
			"Manufacturer": "ACME",
			"Model":        "ProLiant-" + id,
			"SerialNumber": "SN-" + id,
			"PowerState":   "On",
			"MemorySummary": map[string]any{
				"TotalSystemMemoryGiB": 64,
			},
			"ProcessorSummary": map[string]any{
				"Count": 8,
			},
			"Boot": map[string]any{
				"BootSourceOverrideEnabled": "Disabled",
				"BootSourceOverrideTarget":  "None",
			},
		})
		return
	}

	http.Error(w, "not found: "+r.URL.Path, http.StatusNotFound)
}

// pathTail returns the final path segment under /redfish/v1/Systems/, reporting
// false when the path is not a single-segment system member.
func pathTail(path string) (string, bool) {
	const prefix = "/redfish/v1/Systems/"
	if len(path) <= len(prefix) || path[:len(prefix)] != prefix {
		return "", false
	}
	rest := path[len(prefix):]
	for i := 0; i < len(rest); i++ {
		if rest[i] == '/' {
			return "", false
		}
	}
	return rest, true
}

var _ = Describe("DeployHandler.InspectHardware system selection", func() {
	var (
		e          *echo.Echo
		bmcTargets *fakeBMCTargetStore
		bmc        *multiSystemBMC
	)

	BeforeEach(func() {
		e = echo.New()
		bmcTargets = &fakeBMCTargetStore{}
		// A BMC fronting two ComputerSystems: selecting one requires a SystemID.
		bmc = newMultiSystemBMC("node-a", "node-b")
	})

	AfterEach(func() {
		bmc.Close()
	})

	// inspect runs InspectHardware for the stored target id and returns the
	// recorder. VerifySSL is false because the fake uses a self-signed TLS cert.
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

	It("fails safe with 500 when the BMC exposes multiple systems and no SystemID is set", func() {
		Expect(bmcTargets.Create(context.Background(), &store.BMCTarget{
			ID:        "t-multi",
			Name:      "multi",
			Endpoint:  bmc.URL(),
			Username:  "admin",
			Password:  "p",
			VerifySSL: false,
		})).To(Succeed())

		rec := inspect("t-multi")
		Expect(rec.Code).To(Equal(http.StatusInternalServerError))
		// The Deployer refuses to guess and lists the available system Ids.
		Expect(rec.Body.String()).To(ContainSubstring("a system selection is required"))
		Expect(rec.Body.String()).To(ContainSubstring("node-a"))
		Expect(rec.Body.String()).To(ContainSubstring("node-b"))
	})

	It("targets the right system (no 500) when the target carries a SystemID", func() {
		Expect(bmcTargets.Create(context.Background(), &store.BMCTarget{
			ID:        "t-pinned",
			Name:      "pinned",
			Endpoint:  bmc.URL(),
			Username:  "admin",
			Password:  "p",
			VerifySSL: false,
			SystemID:  "node-b",
		})).To(Succeed())

		rec := inspect("t-pinned")
		Expect(rec.Code).To(Equal(http.StatusOK))

		var resp struct {
			MemoryGiB      int    `json:"memoryGiB"`
			ProcessorCount int    `json:"processorCount"`
			Model          string `json:"model"`
		}
		Expect(json.Unmarshal(rec.Body.Bytes(), &resp)).To(Succeed())
		// The chosen system was read (Model carries its Id), not an arbitrary one.
		Expect(resp.Model).To(Equal("ProLiant-node-b"))
		Expect(resp.MemoryGiB).To(Equal(64))
		Expect(resp.ProcessorCount).To(Equal(8))
	})
})

var _ = Describe("DeployHandler.UpdateBMCTarget SystemID", func() {
	It("persists an edited SystemID through the fixed-field copy", func() {
		e := echo.New()
		bmcTargets := &fakeBMCTargetStore{}
		Expect(bmcTargets.Create(context.Background(), &store.BMCTarget{
			ID:       "t-1",
			Name:     "bmc",
			Endpoint: "https://10.0.0.9",
			Username: "admin",
			Password: "p",
		})).To(Succeed())

		h := handlers.NewDeployHandler(nil, nil, bmcTargets, nil, "", nil, nil)
		body := `{"name":"bmc","endpoint":"https://10.0.0.9","username":"admin","systemId":"node-b"}`
		req := httptest.NewRequest(http.MethodPut, "/api/v1/bmc-targets/t-1", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetParamNames("id")
		c.SetParamValues("t-1")
		Expect(h.UpdateBMCTarget(c)).To(Succeed())
		Expect(rec.Code).To(Equal(http.StatusOK))

		got, err := bmcTargets.GetByID(context.Background(), "t-1")
		Expect(err).NotTo(HaveOccurred())
		Expect(got.SystemID).To(Equal("node-b"))
	})
})
