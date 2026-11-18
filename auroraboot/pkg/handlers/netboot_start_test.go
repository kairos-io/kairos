package handlers_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/AuroraBoot/internal/netbootmgr"
	"github.com/kairos-io/AuroraBoot/pkg/handlers"
	"github.com/kairos-io/AuroraBoot/pkg/store"
	"github.com/labstack/echo/v4"
)

// fakeNetboot records Start calls instead of launching a PXE server.
type fakeNetboot struct {
	startCalls []string
	startErr   error
}

func (f *fakeNetboot) Start(_ string, artifactID string) error {
	f.startCalls = append(f.startCalls, artifactID)
	return f.startErr
}
func (f *fakeNetboot) Stop() error                  { return nil }
func (f *fakeNetboot) GetStatus() netbootmgr.Status { return netbootmgr.Status{} }
func (f *fakeNetboot) GetLogs() string              { return "" }

var _ = Describe("DeployHandler.StartNetboot", func() {
	var (
		e            *echo.Echo
		nb           *fakeNetboot
		artifactsDir string
		handler      *handlers.DeployHandler
	)

	BeforeEach(func() {
		e = echo.New()
		nb = &fakeNetboot{}
		artifactsDir = GinkgoT().TempDir()
		as := &fakeArtifactStore{records: []*store.ArtifactRecord{
			{ID: "art-1", Phase: store.ArtifactReady},
		}}
		handler = handlers.NewDeployHandler(as, &fakeDeploymentStore{}, nil, nil, artifactsDir, nil, nil).
			WithTestNetboot(nb)
	})

	start := func(artifactID string) *httptest.ResponseRecorder {
		body, err := json.Marshal(map[string]string{"artifactId": artifactID})
		Expect(err).NotTo(HaveOccurred())
		req := httptest.NewRequest(http.MethodPost, "/api/v1/netboot/start", strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		Expect(handler.StartNetboot(e.NewContext(req, rec))).To(Succeed())
		return rec
	}

	DescribeTable("returns 400 and never starts the server for an artifactId that is not a path segment",
		func(artifactID string) {
			rec := start(artifactID)
			Expect(rec.Code).To(Equal(http.StatusBadRequest))
			Expect(nb.startCalls).To(BeEmpty())
		},
		Entry("parent traversal", "../x"),
		Entry("nested path", "a/b"),
		Entry("absolute path", "/etc"),
		Entry("parent dir", ".."),
	)

	It("returns 400 and never starts the server for an unknown artifact", func() {
		rec := start("no-such-artifact")
		Expect(rec.Code).To(Equal(http.StatusBadRequest))
		Expect(nb.startCalls).To(BeEmpty())
	})

	It("starts the server for a known artifact", func() {
		rec := start("art-1")
		Expect(rec.Code).To(Equal(http.StatusOK))
		Expect(nb.startCalls).To(ConsistOf("art-1"))
	})

	It("returns 409 with a path-free reason when the server is already running", func() {
		nb.startErr = netbootmgr.ErrAlreadyRunning
		rec := start("art-1")
		Expect(rec.Code).To(Equal(http.StatusConflict))
		Expect(rec.Body.String()).To(MatchJSON(`{"error":"netboot server is already running"}`))
	})

	It("returns 400 with a path-free reason when the artifact has no netboot files", func() {
		nb.startErr = fmt.Errorf("%w: %s", netbootmgr.ErrNoNetbootFiles, artifactsDir+"/art-1/netboot/kairos-kernel")
		rec := start("art-1")
		Expect(rec.Code).To(Equal(http.StatusBadRequest))
		Expect(rec.Body.String()).To(MatchJSON(`{"error":"artifact has no netboot files"}`))
	})

	It("does not put server paths in the error body when the start fails", func() {
		nb.startErr = errors.New("required netboot file not found: " + artifactsDir + "/art-1/netboot/kairos-kernel")
		rec := start("art-1")
		Expect(rec.Code).To(Equal(http.StatusInternalServerError))
		Expect(rec.Body.String()).NotTo(ContainSubstring(artifactsDir))
		Expect(rec.Body.String()).To(ContainSubstring("error"))
	})
})
