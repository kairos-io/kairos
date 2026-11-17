package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/kairos-io/AuroraBoot/pkg/handlers"
	"github.com/kairos-io/AuroraBoot/pkg/isoserve"
	"github.com/kairos-io/AuroraBoot/pkg/store"
	"github.com/labstack/echo/v4"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// insertCaptureBMC is a minimal session-less Redfish service with one system and
// one virtual CD. It records the InsertMedia body and then fails the action, so
// a spec can assert the URL and protocol the deploy handed the BMC without
// driving the rest of the deploy flow.
type insertCaptureBMC struct {
	server *httptest.Server

	mu     sync.Mutex
	insert map[string]any
}

func newInsertCaptureBMC() *insertCaptureBMC {
	b := &insertCaptureBMC{}
	b.server = httptest.NewTLSServer(http.HandlerFunc(b.handle))
	return b
}

func (b *insertCaptureBMC) Close() { b.server.Close() }

func (b *insertCaptureBMC) insertBody() map[string]any {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.insert
}

func (b *insertCaptureBMC) handle(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	enc := func(v any) { _ = json.NewEncoder(w).Encode(v) }
	const sys = "/redfish/v1/Systems/sys-1"
	const cd = sys + "/VirtualMedia/Cd"

	switch {
	case r.URL.Path == "/redfish/v1/" || r.URL.Path == "/redfish/v1":
		enc(map[string]any{
			"@odata.id":      "/redfish/v1/",
			"@odata.type":    "#ServiceRoot.v1_5_0.ServiceRoot",
			"Id":             "RootService",
			"Name":           "Root Service",
			"RedfishVersion": "1.6.0",
			"Systems":        map[string]any{"@odata.id": "/redfish/v1/Systems"},
		})
	case r.URL.Path == "/redfish/v1/Systems":
		enc(map[string]any{
			"@odata.id":           "/redfish/v1/Systems",
			"@odata.type":         "#ComputerSystemCollection.ComputerSystemCollection",
			"Name":                "Systems Collection",
			"Members@odata.count": 1,
			"Members":             []map[string]any{{"@odata.id": sys}},
		})
	case r.URL.Path == sys && r.Method == http.MethodGet:
		enc(map[string]any{
			"@odata.id":    sys,
			"@odata.type":  "#ComputerSystem.v1_5_0.ComputerSystem",
			"Id":           "sys-1",
			"Name":         "System",
			"PowerState":   "On",
			"VirtualMedia": map[string]any{"@odata.id": sys + "/VirtualMedia"},
		})
	case r.URL.Path == sys+"/VirtualMedia":
		enc(map[string]any{
			"@odata.id":           sys + "/VirtualMedia",
			"@odata.type":         "#VirtualMediaCollection.VirtualMediaCollection",
			"Name":                "VirtualMedia Collection",
			"Members@odata.count": 1,
			"Members":             []map[string]any{{"@odata.id": cd}},
		})
	case r.URL.Path == cd && r.Method == http.MethodGet:
		enc(map[string]any{
			"@odata.id":      cd,
			"@odata.type":    "#VirtualMedia.v1_3_0.VirtualMedia",
			"Id":             "Cd",
			"Name":           "Virtual CD",
			"MediaTypes":     []string{"CD", "DVD"},
			"Inserted":       false,
			"WriteProtected": true,
			"Actions": map[string]any{
				"#VirtualMedia.InsertMedia": map[string]any{"target": cd + "/Actions/VirtualMedia.InsertMedia"},
				"#VirtualMedia.EjectMedia":  map[string]any{"target": cd + "/Actions/VirtualMedia.EjectMedia"},
			},
		})
	case r.URL.Path == cd+"/Actions/VirtualMedia.InsertMedia" && r.Method == http.MethodPost:
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		b.mu.Lock()
		b.insert = body
		b.mu.Unlock()
		w.WriteHeader(http.StatusInternalServerError)
		enc(map[string]any{"error": map[string]any{"code": "Base.1.0.GeneralError", "message": "stop here"}})
	default:
		http.Error(w, "not found: "+r.URL.Path, http.StatusNotFound)
	}
}

var _ = Describe("DeployRedfish local-serve advertised URL", func() {
	var (
		e            *echo.Echo
		artifacts    *fakeArtifactStore
		settings     *fakeSettingsStore
		artifactsDir string
		bmc          *insertCaptureBMC
	)

	BeforeEach(func() {
		e = echo.New()
		artifactsDir = GinkgoT().TempDir()
		Expect(os.MkdirAll(filepath.Join(artifactsDir, "art-1"), 0755)).To(Succeed())
		isoPath := filepath.Join(artifactsDir, "art-1", "kairos.iso")
		Expect(os.WriteFile(isoPath, []byte("ISO-BYTES"), 0644)).To(Succeed())

		artifacts = &fakeArtifactStore{}
		Expect(artifacts.Create(context.Background(), &store.ArtifactRecord{
			ID:            "art-1",
			ArtifactFiles: []string{isoPath},
		})).To(Succeed())
		settings = newFakeSettingsStore()
		settings.values[handlers.SettingLocalServeEnabled] = "true"

		bmc = newInsertCaptureBMC()
		DeferCleanup(bmc.Close)
	})

	// deploy runs a local-serve deploy against the capture BMC and returns the
	// InsertMedia body the BMC received.
	deploy := func() map[string]any {
		serve := isoserve.New(isoserve.Config{BaseURL: "http://10.0.0.5:8090"})
		h := handlers.NewDeployHandler(artifacts, &fakeDeploymentStore{}, &fakeBMCTargetStore{}, nil, artifactsDir, serve, nil).
			WithSettings(settings)

		body := `{"endpoint":"` + bmc.server.URL + `","username":"u","password":"p","verifySSL":false}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts/art-1/deploy/redfish", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetParamNames("id")
		c.SetParamValues("art-1")
		Expect(h.DeployRedfish(c)).To(Succeed())
		Expect(rec.Code).To(Equal(http.StatusAccepted))

		Eventually(bmc.insertBody, "10s").ShouldNot(BeNil())
		return bmc.insertBody()
	}

	It("hands the BMC a URL on the saved advertised URL, with the protocol from its scheme", func() {
		settings.values[handlers.SettingLocalServeAdvertisedURL] = "https://10.20.30.40:8443/proxy/"

		insert := deploy()
		Expect(insert["Image"]).To(HavePrefix("https://10.20.30.40:8443/proxy/redfish/iso/"))
		Expect(insert["Image"]).To(HaveSuffix("/kairos.iso"))
		Expect(insert["TransferProtocolType"]).To(Equal("HTTPS"))
	})

	It("keeps the launch BaseURL when no advertised URL is saved", func() {
		insert := deploy()
		Expect(insert["Image"]).To(HavePrefix("http://10.0.0.5:8090/redfish/iso/"))
		Expect(insert["TransferProtocolType"]).To(Equal("HTTP"))
	})
})
