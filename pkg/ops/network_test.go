package ops

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("download", Label("network"), func() {
	var origDelay time.Duration

	BeforeEach(func() {
		origDelay = downloadRetryBaseDelay
		downloadRetryBaseDelay = 200 * time.Millisecond
	})

	AfterEach(func() {
		downloadRetryBaseDelay = origDelay
	})

	// grab issues a HEAD probe before every GET; a HEAD response other than
	// 200 is not an error to grab (it just proceeds to the GET), so these
	// tests fail/succeed only the GET to control the download-attempt count.
	notFoundOnHead := func(w http.ResponseWriter, r *http.Request) bool {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusNotFound)
			return true
		}
		return false
	}

	It("retries a transient failure and succeeds on a later attempt", func() {
		var gets atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if notFoundOnHead(w, r) {
				return
			}
			if gets.Add(1) == 1 {
				w.WriteHeader(http.StatusBadGateway)
				return
			}
			w.Write([]byte("payload"))
		}))
		defer srv.Close()

		// download() is always given a destination *file* path, not a
		// directory (its only caller passes deployer.defaultIsoFile()'s full
		// ".../kairos.iso" path).
		dst := filepath.Join(GinkgoT().TempDir(), "testfile.bin")
		_, err := download(context.Background(), srv.URL+"/testfile.bin", dst)
		Expect(err).NotTo(HaveOccurred())
		Expect(gets.Load()).To(Equal(int32(2)))

		content, readErr := os.ReadFile(dst)
		Expect(readErr).NotTo(HaveOccurred())
		Expect(string(content)).To(Equal("payload"))
	})

	It("stops retrying and reports ctx's error when canceled after an attempt", func() {
		var gets atomic.Int32
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Cancel through the hook rather than off a wall-clock sleep: this
		// pins the case where the attempt already failed with a transient
		// error of its own, which is the one that used to be reported
		// instead of the cancellation. A sleep raced the in-flight case and
		// made this spec flaky on a loaded runner.
		afterDownloadAttempt = cancel
		DeferCleanup(func() { afterDownloadAttempt = nil })

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if notFoundOnHead(w, r) {
				return
			}
			gets.Add(1)
			w.WriteHeader(http.StatusBadGateway)
		}))
		defer srv.Close()

		dst := filepath.Join(GinkgoT().TempDir(), "testfile.bin")
		_, err := download(ctx, srv.URL+"/testfile.bin", dst)
		Expect(errors.Is(err, context.Canceled)).To(BeTrue(),
			"a caller checking for cancellation must see it, got %v", err)
		Expect(err.Error()).To(ContainSubstring("502"),
			"the attempt's own error must survive in the message, got %v", err)
		Expect(gets.Load()).To(Equal(int32(1)))
	})

	It("reports ctx's error when canceled while the transfer is in flight", func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// The response headers go out first and the body is held back, so the
		// cancellation lands while downloadOnce is still waiting on the
		// transfer rather than between two attempts.
		//
		// Holding the headers back too is what made this flaky. grab's
		// Client.Do returns once the headers arrive, so a handler that sends
		// nothing until after cancel() lets Do return with ctx already
		// canceled AND the whole 7-byte body already copied, which closes
		// resp.Done. Then both arms of downloadOnce's select are ready and Go
		// picks one at random: when resp.Done wins, download returns a nil
		// error and this spec fails. Forcing that interleaving (an 80ms sleep
		// before the select) failed 21 of 60 runs.
		//
		// Flushing the headers and blocking the body instead means resp.Done
		// can never close, so ctx.Done is the only arm that can fire.
		inFlight := make(chan struct{})
		release := make(chan struct{})
		var once sync.Once
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if notFoundOnHead(w, r) {
				return
			}
			w.WriteHeader(http.StatusOK)
			flusher, ok := w.(http.Flusher)
			Expect(ok).To(BeTrue(), "the test server must support flushing headers")
			flusher.Flush()
			once.Do(func() { close(inFlight) })
			<-release
			w.Write([]byte("payload"))
		}))
		defer srv.Close()
		// Registered after srv.Close, so it runs before it: the handler is
		// let go first, then srv.Close waits for it. DeferCleanup would run
		// after this function's defers and deadlock srv.Close.
		defer close(release)

		go func() {
			defer GinkgoRecover()
			<-inFlight
			cancel()
		}()

		dst := filepath.Join(GinkgoT().TempDir(), "testfile.bin")
		_, err := download(ctx, srv.URL+"/testfile.bin", dst)
		Expect(errors.Is(err, context.Canceled)).To(BeTrue(),
			"a caller checking for cancellation must see it, got %v", err)
	})

	It("reports a host it cannot reach instead of panicking", func() {
		// grab's Client.Do leaves Response.HTTPResponse nil when the HTTP
		// request itself never completed (DNS failure, connection refused,
		// TLS error): doHTTPRequest returns (nil, err) and the state machine
		// goes straight to closeResponse. Reading .Status off it is a nil
		// dereference, which takes the whole process down and defeats the
		// retry loop above.
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		url := srv.URL + "/testfile.bin"
		srv.Close() // nothing listens on that port any more

		dst := filepath.Join(GinkgoT().TempDir(), "testfile.bin")
		_, err := download(context.Background(), url, dst)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("connect"),
			"the connection failure must reach the caller, got %v", err)
	})

	It("reports a URL it cannot parse instead of panicking", func() {
		// grab.NewRequest returns a nil Request with an error on a URL
		// net/http cannot parse. Passing that nil to Client.Do dereferences
		// it. The URL reaches here from --cloud-config/--source and from the
		// netboot and ISO fields of a deployment, so it is user input.
		dst := filepath.Join(GinkgoT().TempDir(), "testfile.bin")
		_, err := download(context.Background(), "http://%zz", dst)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("%zz"),
			"the unusable URL must be named, got %v", err)
	})

	// kairos#5775: grab keeps a local file that is already the remote size and
	// resumes onto a shorter one, so an ISO left in the state dir by an
	// earlier run was kept or spliced with the new download.
	DescribeTable("replaces a file already at the destination",
		func(existing []byte) {
			payload := []byte(strings.Repeat("N", 4096))
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.ServeContent(w, r, "kairos.iso", time.Time{}, bytes.NewReader(payload))
			}))
			defer srv.Close()

			dst := filepath.Join(GinkgoT().TempDir(), "kairos.iso")
			Expect(os.WriteFile(dst, existing, 0o644)).To(Succeed())

			Expect(DownloadArtifact(srv.URL+"/kairos.iso", func() string { return dst })(context.Background())).To(Succeed())

			content, err := os.ReadFile(dst)
			Expect(err).NotTo(HaveOccurred())
			Expect(content).To(Equal(payload))
		},
		Entry("shorter than the artifact", []byte(strings.Repeat("O", 1000))),
		Entry("the same size as the artifact", []byte(strings.Repeat("O", 4096))),
		Entry("longer than the artifact", []byte(strings.Repeat("O", 8192))),
	)
})
