package server_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/AuroraBoot/pkg/server"
	"github.com/kairos-io/AuroraBoot/pkg/store"
)

// oneArtifactStore holds a single ready artifact.
type oneArtifactStore struct {
	fakeArtifactStore
	rec *store.ArtifactRecord
}

func (s *oneArtifactStore) GetByID(_ context.Context, id string) (*store.ArtifactRecord, error) {
	if id == s.rec.ID {
		return s.rec, nil
	}
	return s.fakeArtifactStore.GetByID(context.Background(), id)
}

// The web UI builds download URLs with encodeURIComponent, so a "+" in a file
// name (k3s versions such as v1.37.2+k3s1 put one there) reaches the server as
// "%2B" (kairos-io/kairos#6125).
var _ = Describe("Artifact download of a file name with escaped characters", func() {
	const isoName = "kairos-hadron-v0.5.3-standard-amd64-generic-v4.3.0-k3sv1.37.2+k3s1.iso"

	// File names with a literal "%". Sent with the "%" escaped as "%25", Go
	// keeps no raw path and Echo hands over the decoded name already.
	percentNames := []string{"50%.iso", "a+b%c.iso", "a%41.iso", "a%2Fb.iso", "plain+k3s1.iso"}

	var app http.Handler

	BeforeEach(func() {
		artifactsDir := GinkgoT().TempDir()
		dir := filepath.Join(artifactsDir, "a1")
		Expect(os.MkdirAll(dir, 0o755)).To(Succeed())
		var files []string
		for _, name := range append([]string{isoName}, percentNames...) {
			Expect(os.WriteFile(filepath.Join(dir, name), []byte("iso:"+name), 0o644)).To(Succeed())
			files = append(files, filepath.Join(dir, name))
		}

		app = server.New(server.Config{
			NodeStore:    &fakeNodeStore{},
			CommandStore: &fakeCommandStore{},
			GroupStore:   &fakeGroupStore{},
			ArtifactStore: &oneArtifactStore{rec: &store.ArtifactRecord{
				ID:            "a1",
				Phase:         store.ArtifactReady,
				ArtifactFiles: files,
			}},
			Builder:       &fakeBuilder{},
			AdminPassword: "admin-pass",
			RegToken:      "reg-token",
			AuroraBootURL: "http://localhost:8080",
			ArtifactsDir:  artifactsDir,
			KeysDir:       GinkgoT().TempDir(),
		})
	})

	get := func(path, bearer string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
		return rec
	}

	escaped := strings.ReplaceAll(isoName, "+", "%2B")

	It("serves the file to an admin whether the + is escaped or not", func() {
		for _, name := range []string{isoName, escaped} {
			rec := get("/api/v1/artifacts/a1/download/"+name, "admin-pass")
			Expect(rec.Code).To(Equal(http.StatusOK), "%s: %s", name, rec.Body.String())
			Expect(rec.Body.String()).To(Equal("iso:" + isoName))
		}
	})

	It("serves the file through a signed download link", func() {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts/a1/download-link",
			strings.NewReader(`{"filename":"`+isoName+`"}`))
		req.Header.Set("Authorization", "Bearer admin-pass")
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
		Expect(rec.Code).To(Equal(http.StatusOK), rec.Body.String())

		var link struct {
			Path string `json:"path"`
		}
		Expect(json.Unmarshal(rec.Body.Bytes(), &link)).To(Succeed())
		Expect(link.Path).To(ContainSubstring(isoName))

		// The link as returned, and as a client that escapes the name again
		// with encodeURIComponent sends it.
		for _, path := range []string{link.Path, strings.Replace(link.Path, isoName, escaped, 1)} {
			dl := get(path, "")
			Expect(dl.Code).To(Equal(http.StatusOK), "%s: %s", path, dl.Body.String())
			Expect(dl.Body.String()).To(Equal("iso:" + isoName))
		}
	})

	DescribeTable("serves a file with a literal % in its name, decoded once",
		func(path, name string) {
			rec := get("/api/v1/artifacts/a1/download/"+path, "admin-pass")
			Expect(rec.Code).To(Equal(http.StatusOK), rec.Body.String())
			Expect(rec.Body.String()).To(Equal("iso:" + name))
		},
		Entry("50%.iso", "50%25.iso", "50%.iso"),
		Entry("a+b%c.iso", "a+b%25c.iso", "a+b%c.iso"),
		Entry("a%41.iso", "a%2541.iso", "a%41.iso"),
		Entry("a%2Fb.iso", "a%252Fb.iso", "a%2Fb.iso"),
		Entry("plain+k3s1.iso", "plain+k3s1.iso", "plain+k3s1.iso"),
		// encodeURIComponent output: the %2B makes Go keep a raw path.
		Entry("a+b%c.iso with + escaped", "a%2Bb%25c.iso", "a+b%c.iso"),
	)

	It("still refuses an escaped path separator", func() {
		rec := get("/api/v1/artifacts/a1/download/..%2Fa1%2F"+escaped, "admin-pass")
		Expect(rec.Code).To(Equal(http.StatusBadRequest))
	})
})
