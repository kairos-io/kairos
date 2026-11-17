package handlers_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/AuroraBoot/pkg/handlers"
	"github.com/kairos-io/AuroraBoot/pkg/isoserve"
	"github.com/kairos-io/AuroraBoot/pkg/store"
	"github.com/labstack/echo/v4"
)

var _ = Describe("DeployRedfish image-source precedence", func() {
	// The pure precedence matrix: per-deploy > per-BMC > global default. The empty
	// result signals "fall back to local serving".
	DescribeTable("resolveOperatorImageURL selects the highest-priority tier",
		func(reqURL, bmcURL, defaultURL, want string) {
			Expect(handlers.ResolveOperatorImageURL(reqURL, bmcURL, defaultURL)).To(Equal(want))
		},
		Entry("per-deploy wins over everything",
			"https://req/os.iso", "https://bmc/os.iso", "https://default/os.iso", "https://req/os.iso"),
		Entry("per-BMC wins over global default when no per-deploy URL",
			"", "https://bmc/os.iso", "https://default/os.iso", "https://bmc/os.iso"),
		Entry("global default used when neither per-deploy nor per-BMC set",
			"", "", "https://default/os.iso", "https://default/os.iso"),
		Entry("empty when no tier supplies a URL (fall back to local serve)",
			"", "", "", ""),
	)

	// Integration-level: drive DeployRedfish and assert which path was taken by the
	// resulting status code (202 = an operator URL resolved and a deploy queued;
	// 503 = nothing resolved and local serve unavailable).
	var (
		e            *echo.Echo
		artifacts    *fakeArtifactStore
		deployments  *fakeDeploymentStore
		bmcTargets   *fakeBMCTargetStore
		settings     *fakeSettingsStore
		artifactsDir string
	)

	newHandler := func(withServe bool) *handlers.DeployHandler {
		var serve *isoserve.Server
		if withServe {
			serve = isoserve.New(isoserve.Config{BaseURL: "http://10.0.0.5:8090"})
		}
		return handlers.NewDeployHandler(artifacts, deployments, bmcTargets, nil, artifactsDir, serve, nil).
			WithSettings(settings)
	}

	doDeploy := func(h *handlers.DeployHandler, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts/art-1/deploy/redfish", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetParamNames("id")
		c.SetParamValues("art-1")
		Expect(h.DeployRedfish(c)).To(Succeed())
		return rec
	}

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
		deployments = &fakeDeploymentStore{}
		bmcTargets = &fakeBMCTargetStore{}
		settings = newFakeSettingsStore()
	})

	It("uses the per-BMC ImageURL when no per-deploy URL is given", func() {
		Expect(bmcTargets.Create(context.Background(), &store.BMCTarget{
			ID: "bmc-1", Endpoint: "https://10.0.0.9", Username: "u", Password: "p",
			ImageURL: "http://10.0.0.5/per-bmc.iso",
		})).To(Succeed())
		// A global default is also set; the per-BMC URL must win over it.
		settings.values[handlers.SettingDefaultImageURL] = "http://10.0.0.5/global.iso"

		rec := doDeploy(newHandler(false), `{"bmcTargetId":"bmc-1"}`)
		// 202: an operator URL resolved (per-BMC), so a deploy was queued even with
		// no local-serve configured.
		Expect(rec.Code).To(Equal(http.StatusAccepted))
	})

	It("falls back to the global default when neither per-deploy nor per-BMC URL is set", func() {
		Expect(bmcTargets.Create(context.Background(), &store.BMCTarget{
			ID: "bmc-1", Endpoint: "https://10.0.0.9", Username: "u", Password: "p",
		})).To(Succeed())
		settings.values[handlers.SettingDefaultImageURL] = "http://10.0.0.5/global.iso"

		rec := doDeploy(newHandler(false), `{"bmcTargetId":"bmc-1"}`)
		Expect(rec.Code).To(Equal(http.StatusAccepted))
	})

	It("rejects an SSRF-blocked global default at deploy time", func() {
		Expect(bmcTargets.Create(context.Background(), &store.BMCTarget{
			ID: "bmc-1", Endpoint: "https://10.0.0.9", Username: "u", Password: "p",
		})).To(Succeed())
		settings.values[handlers.SettingDefaultImageURL] = "http://169.254.169.254/x.iso"

		rec := doDeploy(newHandler(false), `{"bmcTargetId":"bmc-1"}`)
		Expect(rec.Code).To(Equal(http.StatusBadRequest))
		Expect(rec.Body.String()).To(ContainSubstring("invalid imageUrl"))
	})

	It("returns 503 when nothing resolves and local serve is configured but not enabled", func() {
		rec := doDeploy(newHandler(true), `{"endpoint":"http://10.0.0.9","username":"u","password":"p"}`)
		Expect(rec.Code).To(Equal(http.StatusServiceUnavailable))
		Expect(rec.Body.String()).To(ContainSubstring("local ISO serving is not enabled"))
	})

	It("serves the local artifact ISO when nothing resolves and local serve is enabled", func() {
		settings.values[handlers.SettingLocalServeEnabled] = "true"
		rec := doDeploy(newHandler(true), `{"endpoint":"http://10.0.0.9","username":"u","password":"p"}`)
		Expect(rec.Code).To(Equal(http.StatusAccepted))
	})

	It("returns 503 when nothing resolves and no local-serve listener exists", func() {
		settings.values[handlers.SettingLocalServeEnabled] = "true"
		rec := doDeploy(newHandler(false), `{"endpoint":"http://10.0.0.9","username":"u","password":"p"}`)
		Expect(rec.Code).To(Equal(http.StatusServiceUnavailable))
	})
})
