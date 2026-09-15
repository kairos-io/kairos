package image_test

import (
	"archive/tar"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/go-containerregistry/pkg/name"
	ggcrregistry "github.com/google/go-containerregistry/pkg/registry"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"

	"github.com/kairos-io/kairos/v4/sdk/utils/image"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var errLayerRead = errors.New("injected layer read failure")

type failingLayer struct {
	v1.Layer
	contents []byte
}

func (l failingLayer) Uncompressed() (io.ReadCloser, error) {
	return io.NopCloser(io.MultiReader(bytes.NewReader(l.contents), errorReader{})), nil
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) {
	return 0, errLayerRead
}

// currentUserImage builds a single-layer image whose one file is owned by the
// current uid/gid, so ExtractOCIImage's chown succeeds when the suite runs
// rootless (v1random.Image writes uid/gid 0, which a non-root lchown rejects).
func currentUserImage() (v1.Image, error) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	content := []byte("hello from the insecure registry")
	if err := tw.WriteHeader(&tar.Header{
		Name:     "hello.txt",
		Typeflag: tar.TypeReg,
		Mode:     0o644,
		Size:     int64(len(content)),
		Uid:      os.Getuid(),
		Gid:      os.Getgid(),
	}); err != nil {
		return nil, err
	}
	if _, err := tw.Write(content); err != nil {
		return nil, err
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	layer, err := tarball.LayerFromOpener(func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(buf.Bytes())), nil
	})
	if err != nil {
		return nil, err
	}
	return mutate.AppendLayers(empty.Image, layer)
}

var _ = Describe("OCIImageExtractor", func() {
	It("returns a layer read error after applying partial tar content", func() {
		var partialTar bytes.Buffer
		tw := tar.NewWriter(&partialTar)
		content := []byte("partial content")
		Expect(tw.WriteHeader(&tar.Header{
			Name:     "partial.txt",
			Typeflag: tar.TypeReg,
			Mode:     0o644,
			Size:     int64(len(content)),
			Uid:      os.Getuid(),
			Gid:      os.Getgid(),
		})).To(Succeed())
		_, err := tw.Write(content)
		Expect(err).ToNot(HaveOccurred())
		Expect(tw.Flush()).To(Succeed())

		baseImage, err := currentUserImage()
		Expect(err).ToNot(HaveOccurred())
		layers, err := baseImage.Layers()
		Expect(err).ToNot(HaveOccurred())
		img, err := mutate.AppendLayers(empty.Image, failingLayer{
			Layer:    layers[0],
			contents: partialTar.Bytes(),
		})
		Expect(err).ToNot(HaveOccurred())

		destDir, err := os.MkdirTemp("", "sdk-partial-extractor-*")
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(os.RemoveAll, destDir)

		err = image.ExtractOCIImage(img, destDir)
		Expect(err).To(MatchError(ContainSubstring(errLayerRead.Error())))
	})

	Describe("against a registry with an untrusted (self-signed) TLS certificate", func() {
		var (
			server   *httptest.Server
			imageRef string
			destDir  string
		)

		BeforeEach(func() {
			server = httptest.NewTLSServer(ggcrregistry.New())

			u, err := url.Parse(server.URL)
			Expect(err).ToNot(HaveOccurred())
			imageRef = u.Host + "/test/extract:latest"

			img, err := currentUserImage()
			Expect(err).ToNot(HaveOccurred())
			ref, err := name.ParseReference(imageRef, name.Insecure)
			Expect(err).ToNot(HaveOccurred())
			Expect(remote.Write(ref, img, remote.WithTransport(insecureTransport()))).To(Succeed())

			destDir, err = os.MkdirTemp("", "sdk-extractor-*")
			Expect(err).ToNot(HaveOccurred())
		})

		AfterEach(func() {
			server.Close()
			Expect(os.RemoveAll(destDir)).To(Succeed())
		})

		It("ExtractImage fails when Insecure is false", func() {
			start := time.Now()
			err := image.OCIImageExtractor{}.ExtractImage(imageRef, destDir, "linux/amd64")
			elapsed := time.Since(start)

			Expect(err).To(HaveOccurred())
			// Must fail on certificate verification, not because the image is missing.
			Expect(strings.ToLower(err.Error())).To(Or(
				ContainSubstring("certificate"),
				ContainSubstring("tls"),
				ContainSubstring("x509"),
			))
			// An untrusted certificate fails identically every time, so the pull
			// must report it straight away rather than sit through the retry
			// backoff. The first backoff wait alone is a second.
			Expect(elapsed).To(BeNumerically("<", time.Second))
		})

		It("ExtractImage succeeds and unpacks files when Insecure is true", func() {
			err := image.OCIImageExtractor{Insecure: true}.ExtractImage(imageRef, destDir, "linux/amd64")
			Expect(err).ToNot(HaveOccurred())

			// Assert the known file from currentUserImage was extracted verbatim,
			// not merely that something landed in the directory.
			content, err := os.ReadFile(filepath.Join(destDir, "hello.txt"))
			Expect(err).ToNot(HaveOccurred())
			Expect(string(content)).To(Equal("hello from the insecure registry"))
		})

		It("GetOCIImageSize requires Insecure for this registry", func() {
			_, err := image.OCIImageExtractor{}.GetOCIImageSize(imageRef, "linux/amd64")
			Expect(err).To(HaveOccurred())

			size, err := image.OCIImageExtractor{Insecure: true}.GetOCIImageSize(imageRef, "linux/amd64")
			Expect(err).ToNot(HaveOccurred())
			Expect(size).To(BeNumerically(">", 0))
		})
	})
})

var _ = Describe("OCIImageExtractor pull retries", func() {
	var (
		server   *httptest.Server
		blobGETs *blobCounter
		imageRef string
		destDir  string
	)

	// truncateFirstBlob serves the registry normally except for the first
	// download of each blob, which it cuts off half-way through and then drops
	// the connection on. The client has been promised a Content-Length it will
	// never receive, so the read fails part-way through the blob: the same
	// shape of failure as a registry resetting the stream mid-download.
	truncateFirstBlob := func(reg http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			isBlob := r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/blobs/")
			if !isBlob || blobGETs.count(r.URL.Path) != 1 {
				reg.ServeHTTP(w, r)
				return
			}

			rec := httptest.NewRecorder()
			reg.ServeHTTP(rec, r)
			body := rec.Body.Bytes()

			w.Header().Set("Content-Length", strconv.Itoa(len(body)))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(body[:len(body)/2])
			// Flush before dropping the connection, so the client has the
			// response head and half a body in hand and fails on the read
			// rather than on the request. That is where the failure this
			// guards against lands, and the only place a request-level retry
			// cannot reach.
			w.(http.Flusher).Flush()
			panic(http.ErrAbortHandler)
		})
	}

	BeforeEach(func() {
		blobGETs = &blobCounter{}
		server = httptest.NewTLSServer(truncateFirstBlob(ggcrregistry.New()))

		u, err := url.Parse(server.URL)
		Expect(err).ToNot(HaveOccurred())
		imageRef = u.Host + "/test/retry:latest"

		img, err := currentUserImage()
		Expect(err).ToNot(HaveOccurred())
		ref, err := name.ParseReference(imageRef, name.Insecure)
		Expect(err).ToNot(HaveOccurred())
		Expect(remote.Write(ref, img, remote.WithTransport(insecureTransport()))).To(Succeed())
		// Only downloads are counted; the push above must not be.
		blobGETs.reset()

		destDir, err = os.MkdirTemp("", "sdk-retry-extractor-*")
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		server.Close()
		Expect(os.RemoveAll(destDir)).To(Succeed())
	})

	It("retries a pull that breaks part-way through a blob", func() {
		start := time.Now()
		Expect(image.OCIImageExtractor{Insecure: true}.ExtractImage(imageRef, destDir, "linux/amd64")).To(Succeed())
		elapsed := time.Since(start)

		// The first download of every blob was cut off, so the pull can only
		// have succeeded by fetching one of them a second time.
		Expect(blobGETs.max()).To(BeNumerically(">=", 2))
		// And the second fetch came from the retry loop here, which waits,
		// rather than from a retry somewhere below that does not.
		Expect(elapsed).To(BeNumerically(">=", time.Second))

		content, err := os.ReadFile(filepath.Join(destDir, "hello.txt"))
		Expect(err).ToNot(HaveOccurred())
		Expect(string(content)).To(Equal("hello from the insecure registry"))
	})

	It("gives up once the attempts are spent", func() {
		// Every download of every blob is cut off, so no attempt can finish.
		blobGETs.always = true

		err := image.OCIImageExtractor{Insecure: true}.ExtractImage(imageRef, destDir, "linux/amd64")
		Expect(err).To(HaveOccurred())
		Expect(blobGETs.attempts()).To(Equal(3))
	})
})

// blobCounter counts blob downloads per path, so a spec can tell a refetch
// from the several distinct blobs a single pull reads.
type blobCounter struct {
	mu     sync.Mutex
	counts map[string]int
	always bool
}

// count records a download of path and returns how many times it has now been
// asked for, or 1 every time when always is set.
func (b *blobCounter) count(path string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.counts == nil {
		b.counts = map[string]int{}
	}
	b.counts[path]++
	if b.always {
		return 1
	}
	return b.counts[path]
}

func (b *blobCounter) reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.counts = map[string]int{}
}

// max is the highest number of times any single blob was downloaded.
func (b *blobCounter) max() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	most := 0
	for _, n := range b.counts {
		if n > most {
			most = n
		}
	}
	return most
}

// attempts is how many times the pull got as far as the blob it dies on, which
// with always set is once per attempt.
func (b *blobCounter) attempts() int {
	return b.max()
}

var _ = Describe("transient network error classification", func() {
	DescribeTable("retryable",
		func(err error) {
			Expect(image.IsTransientNetworkError(err)).To(BeTrue())
		},
		// The literal message that aborted an upgrade ten minutes into the
		// pull in kairos-io/kairos#4491. net/http's bundled HTTP/2 stack keeps
		// this type to itself, so nothing but the text is reachable from here.
		Entry("an HTTP/2 stream reset from the registry",
			errors.New("stream error: stream ID 3; PROTOCOL_ERROR; received from peer")),
		Entry("a wrapped unexpected EOF", fmt.Errorf("reading layer: %w", io.ErrUnexpectedEOF)),
		Entry("a reset connection", fmt.Errorf("read tcp: %w", syscall.ECONNRESET)),
		Entry("a broken pipe", syscall.EPIPE),
		Entry("a closed connection", net.ErrClosed),
		Entry("a timeout", &net.OpError{Op: "read", Err: timeoutError{}}),
		Entry("a GOAWAY", errors.New("http2: server sent GOAWAY and closed the connection")),
	)

	DescribeTable("not retryable",
		func(err error) {
			Expect(image.IsTransientNetworkError(err)).To(BeFalse())
		},
		Entry("no error at all", nil),
		Entry("an untrusted certificate", errors.New("x509: certificate signed by unknown authority")),
		Entry("a rejected pull", errors.New("UNAUTHORIZED: authentication required")),
		Entry("a missing manifest", errors.New("MANIFEST_UNKNOWN: manifest unknown")),
	)
})

type timeoutError struct{}

func (timeoutError) Error() string { return "i/o timeout" }
func (timeoutError) Timeout() bool { return true }
