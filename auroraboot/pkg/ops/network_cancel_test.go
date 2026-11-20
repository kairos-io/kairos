package ops

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("download canceled mid-transfer", Label("network"), func() {
	It("stops the transfer and removes the partial file", func() {
		// The GET sends its headers at once, then dribbles the body until the
		// client goes away or the spec ends. stopped closes when the server
		// sees the client hang up.
		release := make(chan struct{})
		stopped := make(chan struct{})
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodHead {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Length", "100000000")
			w.WriteHeader(http.StatusOK)
			w.(http.Flusher).Flush()
			chunk := make([]byte, 4096)
			for {
				select {
				case <-r.Context().Done():
					close(stopped)
					return
				case <-release:
					return
				case <-time.After(10 * time.Millisecond):
					if _, err := w.Write(chunk); err != nil {
						close(stopped)
						return
					}
					w.(http.Flusher).Flush()
				}
			}
		}))
		defer srv.Close()
		// Registered after srv.Close, so it runs first and lets Close return.
		defer close(release)

		dst := filepath.Join(GinkgoT().TempDir(), "kairos.iso")
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() {
			defer GinkgoRecover()
			Eventually(func() int64 {
				fi, err := os.Stat(dst)
				if err != nil {
					return 0
				}
				return fi.Size()
			}, 5*time.Second, 10*time.Millisecond).Should(BeNumerically(">", 0))
			cancel()
		}()

		_, err := download(ctx, srv.URL+"/kairos.iso", dst)
		Expect(err).To(HaveOccurred())

		Eventually(stopped, 2*time.Second).Should(BeClosed(), "the transfer kept running after cancel")
		Expect(dst).NotTo(BeAnExistingFile())
		Consistently(func() bool {
			_, err := os.Stat(dst)
			return os.IsNotExist(err)
		}, 500*time.Millisecond, 50*time.Millisecond).Should(BeTrue(), "the partial file came back")
	})
})

var _ = Describe("download canceled before the response headers", Label("network"), func() {
	It("returns the cancellation instead of panicking on a missing response", func() {
		// The server never answers: it holds every request until the client
		// goes away, so grab has no HTTP response when ctx is canceled.
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			<-r.Context().Done()
		}))
		defer srv.Close()

		dst := filepath.Join(GinkgoT().TempDir(), "kairos.iso")
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		time.AfterFunc(100*time.Millisecond, cancel)

		var err error
		Expect(func() {
			_, err = download(ctx, srv.URL+"/kairos.iso", dst)
		}).NotTo(Panic())
		Expect(err).To(HaveOccurred())
		Expect(ctx.Err()).To(MatchError(context.Canceled))
		Expect(dst).NotTo(BeAnExistingFile())
	})
})
