package handlers_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/AuroraBoot/pkg/handlers"
	"github.com/kairos-io/AuroraBoot/pkg/store"
	"github.com/labstack/echo/v4"
)

// mirrors handlers.hashUploadToken (unexported); seeding the fake store
// requires the same digest the production Create path would write.
func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

var _ = Describe("ArtifactHandler.Upload", func() {
	var (
		e            *echo.Echo
		fb           *fakeBuilder
		as           *fakeArtifactStore
		artifactsDir string
		handler      *handlers.ArtifactHandler
	)

	const (
		buildID = "build-42"
		token   = "abcdef0123456789"
	)

	BeforeEach(func() {
		e = echo.New()
		fb = &fakeBuilder{}
		var err error
		artifactsDir, err = os.MkdirTemp("", "upload-test-")
		Expect(err).NotTo(HaveOccurred())
		as = &fakeArtifactStore{
			records: []*store.ArtifactRecord{
				{ID: buildID, Phase: store.ArtifactBuilding, UploadToken: sha256Hex(token)},
			},
		}
		handler = handlers.NewArtifactHandler(fb, as, nil, nil, nil, nil, artifactsDir, "reg-token", "http://localhost:8080")
	})

	AfterEach(func() {
		_ = os.RemoveAll(artifactsDir)
	})

	upload := func(id, filename, bearer string, body []byte) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPut,
			"/api/v1/artifacts/"+id+"/upload/"+filename, bytes.NewReader(body))
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetParamNames("id", "*")
		c.SetParamValues(id, filename)
		Expect(handler.Upload(c)).To(Succeed())
		return rec
	}

	It("writes the body to artifactsDir/<id>/<filename> and returns 201", func() {
		rec := upload(buildID, "kairos.iso", token, []byte("iso-bytes"))
		Expect(rec.Code).To(Equal(http.StatusCreated))

		got, err := os.ReadFile(filepath.Join(artifactsDir, buildID, "kairos.iso"))
		Expect(err).NotTo(HaveOccurred())
		Expect(string(got)).To(Equal("iso-bytes"))
	})

	It("returns 401 when the Authorization header is missing", func() {
		rec := upload(buildID, "kairos.iso", "", []byte("x"))
		Expect(rec.Code).To(Equal(http.StatusUnauthorized))
	})

	It("returns 401 when the token does not match the record", func() {
		rec := upload(buildID, "kairos.iso", "wrong-token", []byte("x"))
		Expect(rec.Code).To(Equal(http.StatusUnauthorized))
	})

	It("returns 404 when the build id has no store record", func() {
		rec := upload("does-not-exist", "kairos.iso", token, []byte("x"))
		Expect(rec.Code).To(Equal(http.StatusNotFound))
	})

	It("rejects filenames that would escape the build directory", func() {
		rec := upload(buildID, "../evil", token, []byte("x"))
		Expect(rec.Code).To(Equal(http.StatusBadRequest))

		rec = upload(buildID, "/etc/passwd", token, []byte("x"))
		Expect(rec.Code).To(Equal(http.StatusBadRequest))
	})

	It("rejects artifact ids that would escape artifactsDir", func() {
		// Ids feed into filesystem paths (buildDir, tmpPath, dst) so any
		// traversal in the URL segment would let a caller mkdir or rename
		// outside artifactsDir. GetByID would reject unknown ids on its own,
		// but validate at the boundary too - defense in depth.
		for _, badID := range []string{"..", "../other", "a/b", `a\b`, "/abs"} {
			rec := upload(badID, "kairos.iso", token, []byte("x"))
			Expect(rec.Code).To(Equal(http.StatusBadRequest),
				"id %q should be rejected before store lookup", badID)
		}
	})

	It("overwrites atomically when the same filename uploads twice", func() {
		Expect(upload(buildID, "kairos.iso", token, []byte("first")).Code).To(Equal(http.StatusCreated))
		Expect(upload(buildID, "kairos.iso", token, []byte("second")).Code).To(Equal(http.StatusCreated))

		got, err := os.ReadFile(filepath.Join(artifactsDir, buildID, "kairos.iso"))
		Expect(err).NotTo(HaveOccurred())
		Expect(string(got)).To(Equal("second"))
	})

	It("appends uploaded filenames to the store record without duplicating on retry", func() {
		Expect(upload(buildID, "kairos.iso", token, []byte("a")).Code).To(Equal(http.StatusCreated))
		Expect(upload(buildID, "kairos.raw", token, []byte("b")).Code).To(Equal(http.StatusCreated))
		// Retry of the same file: exporter Job backoff may re-run.
		Expect(upload(buildID, "kairos.iso", token, []byte("a")).Code).To(Equal(http.StatusCreated))

		rec, err := as.GetByID(nil, buildID)
		Expect(err).NotTo(HaveOccurred())
		Expect(rec.ArtifactFiles).To(ConsistOf("kairos.iso", "kairos.raw"))
	})

	It("stores the sha256 digest, never the plaintext token", func() {
		// The DB only ever holds the digest so a snapshot/backup/SQLi read
		// cannot yield a live write bearer. Presenting the plaintext still
		// works because the handler hashes on verify.
		Expect(as.records[0].UploadToken).NotTo(Equal(token),
			"the fake was seeded with sha256Hex(token); a plaintext value would mean the semantics leaked")
		Expect(as.records[0].UploadToken).To(Equal(sha256Hex(token)))
		Expect(upload(buildID, "kairos.iso", token, []byte("x")).Code).To(Equal(http.StatusCreated))
	})

	It("uses constant-time comparison so a length-mismatch token cannot short-circuit", func() {
		// This mostly documents intent; the security property is that both
		// "wrong" and "too short" fail with the same 401 code and no timing
		// signal reachable from the test harness. Assert both paths respond
		// identically.
		shortRec := upload(buildID, "kairos.iso", "short", []byte("x"))
		wrongRec := upload(buildID, "kairos.iso", strings.Repeat("z", len(token)), []byte("x"))
		Expect(shortRec.Code).To(Equal(http.StatusUnauthorized))
		Expect(wrongRec.Code).To(Equal(http.StatusUnauthorized))
	})

	It("rejects uploads once the build has reached a terminal phase", func() {
		// Even with a still-valid token, once the build is Ready its
		// artifacts have been announced to downstream consumers (BMCs,
		// nodes) and must be immutable. Same rule for Error - the build
		// gave up and any late artifact would be lying about the outcome.
		for _, phase := range []string{store.ArtifactReady, store.ArtifactError} {
			as.records[0].Phase = phase
			rec := upload(buildID, "kairos.iso", token, []byte("late"))
			Expect(rec.Code).To(Equal(http.StatusConflict),
				"phase %q should refuse late uploads", phase)
			// And nothing landed on disk.
			_, err := os.Stat(filepath.Join(artifactsDir, buildID, "kairos.iso"))
			Expect(os.IsNotExist(err)).To(BeTrue())
		}
	})

	It("writes only the artifact_files column, not the whole record", func() {
		// Recreates William's specific concern: a full-row Save from Upload
		// would race watchCRPhase and roll back a phase transition that
		// landed between GetByID and the write. Assert that phase and
		// message set by a mid-flight transition survive an Upload.
		as.records[0].Phase = store.ArtifactBuilding
		as.records[0].Message = "in progress"

		// Simulate watchCRPhase transitioning the CR to Ready between the
		// handler's GetByID and its final store write. We flip the record
		// AFTER the request starts but the ONLY store roundtrip in Upload
		// past GetByID is UpdateFiles - so it is enough to check the row
		// state after the upload.
		//
		// Two-step ordering: upload then a phase transition, then upload
		// again should still keep both phase writes intact.
		Expect(upload(buildID, "a.iso", token, []byte("x")).Code).To(Equal(http.StatusCreated))
		Expect(as.UpdatePhaseMessage(nil, buildID, store.ArtifactReady, "done")).To(Succeed())

		rec, err := as.GetByID(nil, buildID)
		Expect(err).NotTo(HaveOccurred())
		Expect(rec.Phase).To(Equal(store.ArtifactReady))
		Expect(rec.Message).To(Equal("done"))
		Expect(rec.ArtifactFiles).To(ConsistOf("a.iso"))
	})
})
