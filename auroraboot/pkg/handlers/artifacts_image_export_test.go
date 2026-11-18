package handlers_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"

	"github.com/kairos-io/AuroraBoot/pkg/handlers"
	"github.com/kairos-io/AuroraBoot/pkg/store"
	"github.com/labstack/echo/v4"
)

// fakeImageExporter stands in for the docker pipeline. It counts calls per
// artifact, records the context it ran with and, when gate is set, blocks
// until gate is closed.
type fakeImageExporter struct {
	calls   atomic.Int32
	mu      sync.Mutex
	perID   map[string]int
	ctxs    []context.Context
	gate    chan struct{}
	started chan string
	body    func(id string) string
	fail    func(id string, w io.Writer) error
}

func (f *fakeImageExporter) export(ctx context.Context, id, containerImage string, w io.Writer) error {
	f.mu.Lock()
	if f.perID == nil {
		f.perID = map[string]int{}
	}
	f.perID[id]++
	f.ctxs = append(f.ctxs, ctx)
	f.mu.Unlock()
	// Counted after the context is recorded, so a spec that waits on calls can
	// read ctxs.
	f.calls.Add(1)
	if f.started != nil {
		f.started <- id
	}
	if f.gate != nil {
		<-f.gate
	}
	if f.fail != nil {
		if err := f.fail(id, w); err != nil {
			return err
		}
	}
	_, err := io.WriteString(w, f.body(id))
	return err
}

func (f *fakeImageExporter) callsFor(id string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.perID[id]
}

// pausingArtifactStore holds one GetByID call after it has read the record, so
// a spec can delete the artifact between an export request's lookup and its
// build.
type pausingArtifactStore struct {
	*fakeArtifactStore
	mu      sync.Mutex
	reached chan struct{}
	release chan struct{}
}

func (s *pausingArtifactStore) pauseNextGet() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reached = make(chan struct{})
	s.release = make(chan struct{})
}

func (s *pausingArtifactStore) GetByID(ctx context.Context, id string) (*store.ArtifactRecord, error) {
	rec, err := s.fakeArtifactStore.GetByID(ctx, id)
	s.mu.Lock()
	reached, release := s.reached, s.release
	s.reached, s.release = nil, nil
	s.mu.Unlock()
	if reached != nil {
		close(reached)
		<-release
	}
	return rec, err
}

var _ = Describe("ArtifactHandler: ExportImage", func() {
	var (
		e            *echo.Echo
		handler      *handlers.ArtifactHandler
		artifactsDir string
		exporter     *fakeImageExporter
		artifacts    *pausingArtifactStore
	)

	imagePath := func(id string) string {
		return filepath.Join(artifactsDir, id, "image-export", "image.tar")
	}

	BeforeEach(func() {
		e = echo.New()
		artifactsDir = GinkgoT().TempDir()
		as := &fakeArtifactStore{}
		artifacts = &pausingArtifactStore{fakeArtifactStore: as}
		for _, r := range []*store.ArtifactRecord{
			{ID: "a1", Phase: store.ArtifactReady, ContainerImage: "registry.local/a1:latest"},
			{ID: "a2", Phase: store.ArtifactReady, ContainerImage: "registry.local/a2:latest"},
			{ID: "noimage", Phase: store.ArtifactReady},
		} {
			Expect(as.Create(context.Background(), r)).To(Succeed())
		}
		exporter = &fakeImageExporter{body: func(id string) string { return "image-tar-of-" + id }}
		handler = handlers.NewArtifactHandler(&fakeBuilder{}, artifacts, nil, nil, nil, nil, artifactsDir, "reg-token", "http://localhost:8080").
			WithTestImageExporter(exporter.export)
	})

	requestCtx := func(ctx context.Context, id string, header http.Header) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/artifacts/"+id+"/image", nil).WithContext(ctx)
		for k, v := range header {
			req.Header[k] = v
		}
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetParamNames("id")
		c.SetParamValues(id)
		_ = handler.ExportImage(c)
		return rec
	}

	request := func(id string) *httptest.ResponseRecorder {
		return requestCtx(context.Background(), id, nil)
	}

	errorOf := func(rec *httptest.ResponseRecorder) string {
		var body map[string]string
		Expect(json.Unmarshal(rec.Body.Bytes(), &body)).To(Succeed())
		return body["error"]
	}

	It("builds the export once and serves later requests from the cached file", func() {
		rec := request("a1")
		Expect(rec.Code).To(Equal(http.StatusOK))
		Expect(rec.Body.String()).To(Equal("image-tar-of-a1"))
		Expect(rec.Header().Get("Content-Type")).To(Equal("application/octet-stream"))
		Expect(rec.Header().Get("Content-Disposition")).To(Equal("attachment; filename=a1.tar"))
		Expect(rec.Header().Get("Content-Length")).To(Equal(strconv.Itoa(len("image-tar-of-a1"))))
		Expect(imagePath("a1")).To(BeAnExistingFile())

		rec = request("a1")
		Expect(rec.Code).To(Equal(http.StatusOK))
		Expect(rec.Body.String()).To(Equal("image-tar-of-a1"))
		Expect(exporter.calls.Load()).To(BeEquivalentTo(1))
	})

	It("runs one build for concurrent requests of the same artifact", func() {
		exporter.gate = make(chan struct{})
		const n = 5
		results := make(chan *httptest.ResponseRecorder, n)
		for range n {
			go func() {
				defer GinkgoRecover()
				results <- request("a1")
			}()
		}
		Eventually(exporter.calls.Load).Should(BeEquivalentTo(1))
		Consistently(exporter.calls.Load, 200*time.Millisecond).Should(BeEquivalentTo(1))
		close(exporter.gate)
		for range n {
			var rec *httptest.ResponseRecorder
			Eventually(results).Should(Receive(&rec))
			Expect(rec.Code).To(Equal(http.StatusOK))
			Expect(rec.Body.String()).To(Equal("image-tar-of-a1"))
		}
		Expect(exporter.calls.Load()).To(BeEquivalentTo(1))
	})

	It("does not make one artifact's request wait for another artifact's build", func() {
		exporter.gate = make(chan struct{})
		exporter.started = make(chan string, 2)
		results := make(chan *httptest.ResponseRecorder, 2)
		for _, id := range []string{"a1", "a2"} {
			go func() {
				defer GinkgoRecover()
				results <- request(id)
			}()
		}
		Eventually(exporter.started).Should(Receive())
		Eventually(exporter.started).Should(Receive())
		Expect(exporter.callsFor("a1")).To(Equal(1))
		Expect(exporter.callsFor("a2")).To(Equal(1))
		close(exporter.gate)
		Eventually(results).Should(Receive())
		Eventually(results).Should(Receive())
	})

	It("returns 500 on a failed build, caches nothing and retries on the next request", func() {
		failing := true
		exporter.fail = func(string, io.Writer) error {
			if failing {
				return errors.New("docker create: exit status 1: no such image")
			}
			return nil
		}
		rec := request("a1")
		Expect(rec.Code).To(Equal(http.StatusInternalServerError))
		Expect(errorOf(rec)).To(Equal("exporting artifact image: docker create: exit status 1: no such image"))
		Expect(imagePath("a1")).NotTo(BeAnExistingFile())

		failing = false
		rec = request("a1")
		Expect(rec.Code).To(Equal(http.StatusOK))
		Expect(rec.Body.String()).To(Equal("image-tar-of-a1"))
		Expect(exporter.calls.Load()).To(BeEquivalentTo(2))
	})

	It("returns 500 when the build fails after writing part of the tar", func() {
		exporter.fail = func(_ string, w io.Writer) error {
			_, _ = io.WriteString(w, "partial")
			return errors.New("docker save: exit status 1")
		}
		rec := request("a1")
		Expect(rec.Code).To(Equal(http.StatusInternalServerError))
		Expect(errorOf(rec)).To(ContainSubstring("docker save: exit status 1"))
		Expect(rec.Body.String()).NotTo(ContainSubstring("partial"))
		Expect(imagePath("a1")).NotTo(BeAnExistingFile())
		entries, err := os.ReadDir(filepath.Dir(imagePath("a1")))
		Expect(err).NotTo(HaveOccurred())
		Expect(entries).To(BeEmpty())
	})

	It("keeps building after the requester disconnects", func() {
		exporter.gate = make(chan struct{})
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan *httptest.ResponseRecorder, 1)
		go func() {
			defer GinkgoRecover()
			done <- requestCtx(ctx, "a1", nil)
		}()
		Eventually(exporter.calls.Load).Should(BeEquivalentTo(1))
		cancel()
		Eventually(done).Should(Receive())

		exporter.mu.Lock()
		buildCtx := exporter.ctxs[0]
		exporter.mu.Unlock()
		Expect(buildCtx.Err()).NotTo(HaveOccurred())

		close(exporter.gate)
		Eventually(func() string { return imagePath("a1") }).Should(BeAnExistingFile())
		rec := request("a1")
		Expect(rec.Code).To(Equal(http.StatusOK))
		Expect(rec.Body.String()).To(Equal("image-tar-of-a1"))
		Expect(exporter.calls.Load()).To(BeEquivalentTo(1))
	})

	It("bounds the build with its own deadline", func() {
		Expect(request("a1").Code).To(Equal(http.StatusOK))

		exporter.mu.Lock()
		buildCtx := exporter.ctxs[0]
		exporter.mu.Unlock()
		deadline, ok := buildCtx.Deadline()
		Expect(ok).To(BeTrue())
		Expect(time.Until(deadline)).To(BeNumerically("~", handlers.ImageExportTimeout, time.Minute))
	})

	It("removes an export whose artifact was deleted while it was being built", func() {
		artifacts.pauseNextGet()
		reached := artifacts.reached
		release := artifacts.release
		done := make(chan *httptest.ResponseRecorder, 1)
		go func() {
			defer GinkgoRecover()
			done <- request("a1")
		}()
		Eventually(reached).Should(BeClosed())

		del := httptest.NewRecorder()
		c := e.NewContext(httptest.NewRequest(http.MethodDelete, "/api/v1/artifacts/a1", nil), del)
		c.SetParamNames("id")
		c.SetParamValues("a1")
		Expect(handler.Delete(c)).To(Succeed())
		Expect(del.Code).To(Equal(http.StatusNoContent))

		close(release)
		var rec *httptest.ResponseRecorder
		Eventually(done).Should(Receive(&rec))
		Expect(rec.Code).To(Equal(http.StatusInternalServerError))
		Expect(filepath.Join(artifactsDir, "a1")).NotTo(BeADirectory())
	})

	It("keeps the artifact's other files when the store lookup before publishing fails", func() {
		Expect(os.MkdirAll(filepath.Join(artifactsDir, "a1"), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(artifactsDir, "a1", "kairos.iso"), []byte("iso"), 0o644)).To(Succeed())
		exporter.fail = func(string, io.Writer) error {
			artifacts.getErr = errors.New("database is locked")
			return nil
		}

		rec := request("a1")
		Expect(rec.Code).To(Equal(http.StatusInternalServerError))
		Expect(errorOf(rec)).To(ContainSubstring("database is locked"))
		Expect(filepath.Join(artifactsDir, "a1", "kairos.iso")).To(BeAnExistingFile())
		Expect(imagePath("a1")).NotTo(BeAnExistingFile())
	})

	It("answers a Range request with 206 and the requested bytes", func() {
		rec := requestCtx(context.Background(), "a1", http.Header{"Range": {"bytes=6-8"}})
		Expect(rec.Code).To(Equal(http.StatusPartialContent))
		Expect(rec.Body.String()).To(Equal("tar"))
	})

	It("removes temp files a crashed build left behind and never serves them", func() {
		dir := filepath.Dir(imagePath("a1"))
		Expect(os.MkdirAll(dir, 0o755)).To(Succeed())
		stale := filepath.Join(dir, "image-123.tar.tmp")
		Expect(os.WriteFile(stale, []byte("stale"), 0o644)).To(Succeed())

		rec := request("a1")
		Expect(rec.Code).To(Equal(http.StatusOK))
		Expect(rec.Body.String()).To(Equal("image-tar-of-a1"))
		Expect(stale).NotTo(BeAnExistingFile())
		entries, err := os.ReadDir(dir)
		Expect(err).NotTo(HaveOccurred())
		Expect(entries).To(HaveLen(1))
		Expect(entries[0].Name()).To(Equal("image.tar"))
	})

	Describe("logging how a request got its image", func() {
		var logs *gbytes.Buffer

		BeforeEach(func() {
			logs = gbytes.NewBuffer()
			log.SetOutput(logs)
			DeferCleanup(func() { log.SetOutput(os.Stderr) })
		})

		It("logs the request that built the export", func() {
			Expect(request("a1").Code).To(Equal(http.StatusOK))

			Expect(logs).To(gbytes.Say(`image export for artifact a1: built in \S+`))
		})

		It("logs a later request as served from the cache", func() {
			Expect(request("a1").Code).To(Equal(http.StatusOK))
			Expect(logs).To(gbytes.Say(`image export for artifact a1: built`))

			Expect(request("a1").Code).To(Equal(http.StatusOK))

			Expect(logs).To(gbytes.Say(`image export for artifact a1: served from the cache`))
		})

		It("logs a request that waited for a build already running", func() {
			exporter.gate = make(chan struct{})
			results := make(chan *httptest.ResponseRecorder, 2)
			go func() {
				defer GinkgoRecover()
				results <- request("a1")
			}()
			Eventually(exporter.calls.Load).Should(BeEquivalentTo(1))
			go func() {
				defer GinkgoRecover()
				results <- request("a1")
			}()
			Consistently(results, 200*time.Millisecond).ShouldNot(Receive())
			close(exporter.gate)
			Eventually(results).Should(Receive())
			Eventually(results).Should(Receive())

			out := string(logs.Contents())
			Expect(strings.Count(out, "image export for artifact a1: built")).To(Equal(1))
			Expect(strings.Count(out, "image export for artifact a1: waited for the build of another request")).To(Equal(1))
			Expect(out).NotTo(ContainSubstring("served from the cache"))
		})
	})

	It("404s an unknown artifact without exporting", func() {
		Expect(request("missing").Code).To(Equal(http.StatusNotFound))
		Expect(exporter.calls.Load()).To(BeZero())
	})

	It("404s an artifact without a container image without exporting", func() {
		Expect(request("noimage").Code).To(Equal(http.StatusNotFound))
		Expect(exporter.calls.Load()).To(BeZero())
	})
})

var _ = Describe("StderrTail", func() {
	It("is empty for empty output", func() {
		Expect(handlers.StderrTail(nil)).To(Equal(""))
		Expect(handlers.StderrTail([]byte(" \n"))).To(Equal(""))
	})

	It("trims a short message and prefixes it", func() {
		Expect(handlers.StderrTail([]byte("  Error: no such image\n"))).To(Equal(": Error: no such image"))
	})

	It("keeps the last 512 bytes of a long message", func() {
		long := strings.Repeat("a", 600) + strings.Repeat("b", 512)
		Expect(handlers.StderrTail([]byte(long))).To(Equal(": " + strings.Repeat("b", 512)))
	})
})

var _ = Describe("RemoveExportLeftovers", func() {
	It("gives up on a docker command that hangs", func() {
		bin := GinkgoT().TempDir()
		Expect(os.WriteFile(filepath.Join(bin, "docker"), []byte("#!/bin/sh\nexec sleep 30\n"), 0o755)).To(Succeed())
		GinkgoT().Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
		DeferCleanup(handlers.SetDockerCleanupTimeout(100 * time.Millisecond))

		start := time.Now()
		handlers.RemoveExportLeftovers("a1")
		Expect(time.Since(start)).To(BeNumerically("<", 5*time.Second))
	})
})
