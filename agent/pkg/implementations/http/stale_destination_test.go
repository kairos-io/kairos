package http_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"time"

	kairoshttp "github.com/kairos-io/kairos/v4/agent/pkg/implementations/http"
	"github.com/kairos-io/kairos/v4/sdk/types/logger"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// serveArtifact serves body under any path, honouring Range requests the way a
// real artifact host does, so a resuming client can actually resume.
func serveArtifact(body []byte) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "artifact.raw", time.Time{}, bytes.NewReader(body))
	}))
}

var _ = Describe("GetURL over an existing destination", Label("http"), func() {
	var client *kairoshttp.Client
	var log logger.KairosLogger
	var destDir, dest string

	BeforeEach(func() {
		client = kairoshttp.NewClient()
		log = logger.NewNullLogger()
		destDir = GinkgoT().TempDir()
		dest = filepath.Join(destDir, "artifact.raw")
	})

	// A stale file shorter than the remote one used to be treated as a partial
	// download and appended to, leaving a hybrid of both artifacts on disk and
	// reporting success.
	It("replaces a shorter stale file instead of appending to it", func() {
		remote := []byte("NEW-CONTENT-THAT-IS-LONGER-THAN-THE-OLD-ONE")
		Expect(os.WriteFile(dest, []byte("OLD-CONTENT"), 0644)).To(Succeed())

		srv := serveArtifact(remote)
		defer srv.Close()

		Expect(client.GetURL(log, srv.URL+"/artifact.raw", dest)).To(Succeed())
		Expect(os.ReadFile(dest)).To(Equal(remote))
	})

	It("replaces a longer stale file instead of failing with ErrBadLength", func() {
		remote := []byte("SHORT")
		Expect(os.WriteFile(dest, []byte("A-MUCH-LONGER-STALE-FILE-LEFT-BEHIND"), 0644)).To(Succeed())

		srv := serveArtifact(remote)
		defer srv.Close()

		Expect(client.GetURL(log, srv.URL+"/artifact.raw", dest)).To(Succeed())
		Expect(os.ReadFile(dest)).To(Equal(remote))
	})

	It("replaces a same-length stale file instead of keeping it", func() {
		remote := []byte("BBBBBBBBBB")
		Expect(os.WriteFile(dest, []byte("AAAAAAAAAA"), 0644)).To(Succeed())

		srv := serveArtifact(remote)
		defer srv.Close()

		Expect(client.GetURL(log, srv.URL+"/artifact.raw", dest)).To(Succeed())
		Expect(os.ReadFile(dest)).To(Equal(remote))
	})

	It("replaces a stale file when the destination is a directory", func() {
		remote := []byte("NEW-CONTENT-THAT-IS-LONGER-THAN-THE-OLD-ONE")
		Expect(os.WriteFile(dest, []byte("OLD-CONTENT"), 0644)).To(Succeed())

		srv := serveArtifact(remote)
		defer srv.Close()

		Expect(client.GetURL(log, srv.URL+"/artifact.raw", destDir)).To(Succeed())
		Expect(os.ReadFile(dest)).To(Equal(remote))
	})
})
