package handlers_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/kairos-io/AuroraBoot/pkg/handlers"
	"github.com/kairos-io/AuroraBoot/pkg/isoserve"
	"github.com/kairos-io/AuroraBoot/pkg/redfish"
	"github.com/kairos-io/AuroraBoot/pkg/store"
	"github.com/labstack/echo/v4"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// virtualMediaBMC is a compact, session-less Redfish service with one
// ComputerSystem and one CD VirtualMedia. It serves just enough for
// Deployer.Deploy to run InsertMedia -> boot override -> Reset, and records the
// image URL it was asked to mount so a test can read the ISO the way a BMC does.
type virtualMediaBMC struct {
	server       *httptest.Server
	insertStatus int

	mu    sync.Mutex
	image string
}

func newVirtualMediaBMC() *virtualMediaBMC {
	b := &virtualMediaBMC{insertStatus: http.StatusNoContent}
	b.server = httptest.NewTLSServer(http.HandlerFunc(b.handle))
	return b
}

func (b *virtualMediaBMC) Close()      { b.server.Close() }
func (b *virtualMediaBMC) URL() string { return b.server.URL }

func (b *virtualMediaBMC) mountedImage() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.image
}

func (b *virtualMediaBMC) handle(w http.ResponseWriter, r *http.Request) {
	const (
		sys = "/redfish/v1/Systems/sys-1"
		cd  = sys + "/VirtualMedia/Cd"
	)
	w.Header().Set("Content-Type", "application/json")
	enc := func(v any) { _ = json.NewEncoder(w).Encode(v) }

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
			"Boot": map[string]any{
				"BootSourceOverrideEnabled": "Disabled",
				"BootSourceOverrideTarget":  "None",
			},
			"Actions": map[string]any{
				"#ComputerSystem.Reset": map[string]any{
					"target":                            sys + "/Actions/ComputerSystem.Reset",
					"ResetType@Redfish.AllowableValues": []string{"On", "ForceOff", "ForceRestart"},
				},
			},
		})
	case r.URL.Path == sys && r.Method == http.MethodPatch:
		w.WriteHeader(http.StatusNoContent)
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
			"@odata.id":   cd,
			"@odata.type": "#VirtualMedia.v1_3_0.VirtualMedia",
			"Id":          "Cd",
			"Name":        "Virtual CD",
			"MediaTypes":  []string{"CD", "DVD"},
			"Inserted":    false,
			"Actions": map[string]any{
				"#VirtualMedia.InsertMedia": map[string]any{"target": cd + "/Actions/VirtualMedia.InsertMedia"},
				"#VirtualMedia.EjectMedia":  map[string]any{"target": cd + "/Actions/VirtualMedia.EjectMedia"},
			},
		})
	case r.URL.Path == cd+"/Actions/VirtualMedia.InsertMedia" && r.Method == http.MethodPost:
		raw, _ := io.ReadAll(r.Body)
		var body struct {
			Image string `json:"Image"`
		}
		_ = json.Unmarshal(raw, &body)
		b.mu.Lock()
		b.image = body.Image
		b.mu.Unlock()
		if b.insertStatus >= 400 {
			http.Error(w, `{"error":{"code":"Base.1.0.GeneralError","message":"InsertMedia failed"}}`, b.insertStatus)
			return
		}
		w.WriteHeader(b.insertStatus)
	case r.URL.Path == sys+"/Actions/ComputerSystem.Reset" && r.Method == http.MethodPost:
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "not found: "+r.URL.Path, http.StatusNotFound)
	}
}

var _ = Describe("DeployHandler.DeployRedfish served ISO lifetime", func() {
	var (
		e           *echo.Echo
		deployments *fakeDeploymentStore
		bmcTargets  *fakeBMCTargetStore
		serve       *isoserve.Server
		isoTS       *httptest.Server
		bmc         *virtualMediaBMC
		h           *handlers.DeployHandler
	)

	BeforeEach(func() {
		e = echo.New()
		dir := GinkgoT().TempDir()
		Expect(os.MkdirAll(filepath.Join(dir, "art-1"), 0755)).To(Succeed())
		isoPath := filepath.Join(dir, "art-1", "kairos.iso")
		Expect(os.WriteFile(isoPath, []byte(strings.Repeat("ISO-BYTES-", 100)), 0644)).To(Succeed())

		isoTS = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			serve.Handler().ServeHTTP(w, r)
		}))
		serve = isoserve.New(isoserve.Config{BaseURL: isoTS.URL})

		bmc = newVirtualMediaBMC()

		artifacts := &fakeArtifactStore{}
		Expect(artifacts.Create(context.Background(), &store.ArtifactRecord{
			ID:            "art-1",
			ArtifactFiles: []string{isoPath},
		})).To(Succeed())
		deployments = &fakeDeploymentStore{}
		bmcTargets = &fakeBMCTargetStore{}
		Expect(bmcTargets.Create(context.Background(), &store.BMCTarget{
			ID:                "bmc-1",
			Name:              "bmc",
			Endpoint:          bmc.URL(),
			Username:          "admin",
			Password:          "p",
			VerifySSL:         false,
			EjectAfterInstall: true,
		})).To(Succeed())
		settings := newFakeSettingsStore()
		settings.values[handlers.SettingLocalServeEnabled] = "true"

		h = handlers.NewDeployHandler(artifacts, deployments, bmcTargets, nil, dir, serve, nil).
			WithSettings(settings).
			WithTestFinalizerFactory(func(redfish.Config) handlers.RedfishFinalizer { return &fakeFinalizer{} })
	})

	AfterEach(func() {
		bmc.Close()
		isoTS.Close()
	})

	deployWith := func(body string) string {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts/art-1/deploy/redfish",
			strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetParamNames("id")
		c.SetParamValues("art-1")
		Expect(h.DeployRedfish(c)).To(Succeed())
		Expect(rec.Code).To(Equal(http.StatusAccepted))
		var dep store.Deployment
		Expect(json.Unmarshal(rec.Body.Bytes(), &dep)).To(Succeed())
		return dep.ID
	}
	deploy := func() string { return deployWith(`{"bmcTargetId":"bmc-1"}`) }

	deployment := func(id string) store.Deployment {
		dep, err := deployments.GetByID(context.Background(), id)
		Expect(err).NotTo(HaveOccurred())
		return *dep
	}

	// rangeRead fetches the first bytes of the mounted image the way a BMC
	// streaming the virtual CD does, and returns the HTTP status.
	rangeRead := func() int {
		req, err := http.NewRequest(http.MethodGet, bmc.mountedImage(), nil)
		Expect(err).NotTo(HaveOccurred())
		req.Header.Set("Range", "bytes=0-99")
		resp, err := http.DefaultClient.Do(req)
		Expect(err).NotTo(HaveOccurred())
		_ = resp.Body.Close()
		return resp.StatusCode
	}

	It("keeps the ISO fetchable after Deploy returns and revokes it once the deployment is finalized", func() {
		id := deploy()
		Eventually(func() string { return deployment(id).EjectState }, 10*time.Second, 20*time.Millisecond).
			Should(Equal(store.EjectStatePending))
		Expect(deployment(id).Status).To(Equal(store.DeployCompleted))
		Expect(bmc.mountedImage()).To(HavePrefix(isoTS.URL))

		Consistently(rangeRead, 500*time.Millisecond, 50*time.Millisecond).
			Should(Equal(http.StatusPartialContent), "the BMC is still booting the installer from the served ISO")

		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/deployments/%s/finalize", id), nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetParamNames("id")
		c.SetParamValues(id)
		Expect(h.FinalizeDeployment(c)).To(Succeed())
		Expect(rec.Code).To(Equal(http.StatusOK))

		Expect(rangeRead()).To(Equal(http.StatusNotFound))
	})

	It("revokes the ISO when the deploy completes without an eject armed", func() {
		id := deployWith(`{"bmcTargetId":"bmc-1","ejectAfterInstall":false}`)
		Eventually(func() string { return deployment(id).Status }, 10*time.Second, 20*time.Millisecond).
			Should(Equal(store.DeployCompleted))
		Expect(deployment(id).EjectState).NotTo(Equal(store.EjectStatePending))
		Expect(bmc.mountedImage()).To(HavePrefix(isoTS.URL))

		Eventually(rangeRead, 2*time.Second, 20*time.Millisecond).Should(Equal(http.StatusNotFound))
	})

	It("revokes the ISO when the deploy fails", func() {
		bmc.insertStatus = http.StatusInternalServerError
		id := deploy()
		Eventually(func() string { return deployment(id).Status }, 10*time.Second, 20*time.Millisecond).
			Should(Equal(store.DeployFailed))
		Expect(bmc.mountedImage()).To(HavePrefix(isoTS.URL))

		Eventually(rangeRead, 2*time.Second, 20*time.Millisecond).Should(Equal(http.StatusNotFound))
	})
})
