package ops

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/tls"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/google/go-containerregistry/pkg/name"
	ggcrregistry "github.com/google/go-containerregistry/pkg/registry"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/kairos-io/AuroraBoot/internal"
	"github.com/kairos-io/AuroraBoot/pkg/constants"
	"github.com/kairos-io/kairos/v4/sdk/types/logger"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// testImage builds a single-layer image containing one regular file owned by
// the current user, so it can be extracted without root (random.Image uses
// UID/GID 0, which makes the lchown during extraction fail when running
// rootless).
func testImage() (v1.Image, error) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	content := []byte("hello insecure registry")
	if err := tw.WriteHeader(&tar.Header{
		Name:     "hello.txt",
		Typeflag: tar.TypeReg,
		Mode:     0644,
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

var _ = Describe("DumpSource against an insecure registry", Label("ops"), func() {
	var (
		server   *httptest.Server
		imageRef string
		destDir  string
	)

	BeforeEach(func() {
		internal.Log = logger.NewKairosLogger("test", "info", false)

		// A registry served over HTTPS with a self-signed certificate, like a
		// company-internal registry without a publicly trusted cert.
		server = httptest.NewTLSServer(ggcrregistry.New())

		u, err := url.Parse(server.URL)
		Expect(err).ToNot(HaveOccurred())
		imageRef = u.Host + "/test/img:latest"

		// Seed it with a small, extractable image using an insecure transport.
		img, err := testImage()
		Expect(err).ToNot(HaveOccurred())
		ref, err := name.ParseReference(imageRef, name.Insecure)
		Expect(err).ToNot(HaveOccurred())
		tr := http.DefaultTransport.(*http.Transport).Clone()
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec
		Expect(remote.Write(ref, img, remote.WithTransport(tr))).To(Succeed())

		destDir, err = os.MkdirTemp("", "auroraboot-dumpsource-*")
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		server.Close()
		Expect(os.RemoveAll(destDir)).To(Succeed())
	})

	It("fails without allow-insecure-registries", func() {
		err := DumpSource("docker://"+imageRef, func() string { return destDir }, "", false)(context.Background())
		Expect(err).To(HaveOccurred())
		// It must fail because of TLS verification, not because the ref is
		// unparseable or the image is missing.
		Expect(strings.ToLower(err.Error())).To(Or(
			ContainSubstring("certificate"),
			ContainSubstring("tls"),
			ContainSubstring("x509"),
		))
	})

	It("succeeds with allow-insecure-registries and extracts the image", func() {
		err := DumpSource("docker://"+imageRef, func() string { return destDir }, "", true)(context.Background())
		Expect(err).ToNot(HaveOccurred())

		entries, err := os.ReadDir(destDir)
		Expect(err).ToNot(HaveOccurred())
		Expect(entries).ToNot(BeEmpty())
	})
})

// blobCounter counts blob downloads per path, so a spec can tell a refetch of
// one blob from the several distinct blobs a single pull reads.
type blobCounter struct {
	mu     sync.Mutex
	counts map[string]int
}

// count records a download of path and returns how many times it has now been
// asked for.
func (b *blobCounter) count(path string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.counts == nil {
		b.counts = map[string]int{}
	}
	b.counts[path]++
	return b.counts[path]
}

// max returns the highest number of downloads any single blob received.
func (b *blobCounter) max() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	var m int
	for _, c := range b.counts {
		if c > m {
			m = c
		}
	}
	return m
}

func (b *blobCounter) reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.counts = map[string]int{}
}

var _ = Describe("DumpSource on a pull that breaks part-way through", Label("ops"), func() {
	var (
		server   *httptest.Server
		blobGETs *blobCounter
		imageRef string
		destDir  string
	)

	// truncateFirstBlob serves the registry normally except for the first
	// download of each blob, which it cuts off half-way through before dropping
	// the connection. The client has been promised a Content-Length it will
	// never receive, so it fails on a read part-way through the blob rather
	// than on the request: the shape of failure a registry resetting a stream
	// mid-download produces, and the one place a request-level retry inside the
	// registry client cannot reach.
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
			w.(http.Flusher).Flush()
			panic(http.ErrAbortHandler)
		})
	}

	BeforeEach(func() {
		internal.Log = logger.NewKairosLogger("test", "info", false)

		blobGETs = &blobCounter{}
		server = httptest.NewTLSServer(truncateFirstBlob(ggcrregistry.New()))

		u, err := url.Parse(server.URL)
		Expect(err).ToNot(HaveOccurred())
		imageRef = u.Host + "/test/retry:latest"

		img, err := testImage()
		Expect(err).ToNot(HaveOccurred())
		ref, err := name.ParseReference(imageRef, name.Insecure)
		Expect(err).ToNot(HaveOccurred())
		tr := http.DefaultTransport.(*http.Transport).Clone()
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec
		Expect(remote.Write(ref, img, remote.WithTransport(tr))).To(Succeed())
		// Only downloads are counted; the push above must not be.
		blobGETs.reset()

		destDir, err = os.MkdirTemp("", "auroraboot-dumpsource-retry-*")
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		server.Close()
		Expect(os.RemoveAll(destDir)).To(Succeed())
	})

	// kairos-io/kairos#4528: a build used to throw the whole download away on
	// one mid-blob error. This asserts the behaviour AuroraBoot relies on from
	// the SDK rather than the SDK's own code, so a pin that loses the retry
	// fails here instead of in an e2e ISO build ten minutes in.
	It("retries instead of discarding the whole download", func() {
		err := DumpSource("docker://"+imageRef, func() string { return destDir }, "", true)(context.Background())
		Expect(err).ToNot(HaveOccurred())

		// The first download of every blob was cut off, so the pull can only
		// have succeeded by fetching one of them again.
		Expect(blobGETs.max()).To(BeNumerically(">=", 2))

		content, err := os.ReadFile(filepath.Join(destDir, "hello.txt"))
		Expect(err).ToNot(HaveOccurred())
		Expect(string(content)).To(Equal("hello insecure registry"))
	})
})

// An option that fails used to make NewConfig return a nil *Config, which
// DumpSource then handed to elemental. herd runs the callback in a goroutine
// it never recovers, so the whole auroraboot process died on SIGSEGV with no
// message. kairos-io/kairos#5357.
var _ = Describe("DumpSource with an architecture the config layer rejects", Label("ops"), func() {
	It("returns the error instead of dereferencing a nil config", func() {
		dst := GinkgoT().TempDir()
		fn := DumpSource("docker://quay.io/kairos/fixture:latest",
			func() string { return dst }, "sparc", false)

		err := fn(context.Background())
		Expect(err).To(MatchError(ContainSubstring("invalid architecture sparc")))
	})
})

var _ = Describe("NewConfig", Label("ops"), func() {
	It("reports a failing option rather than returning a nil config", func() {
		cfg, err := NewConfig(WithArch("sparc"))
		Expect(err).To(MatchError(ContainSubstring("invalid architecture sparc")))
		Expect(cfg).To(BeNil())
	})

	// x86_64 is what GetArchFromRootfs reads out of KAIROS_ARCH, and what
	// the arch: key of a config file is likely to say.
	It("accepts x86_64, the name the rest of AuroraBoot uses for amd64", func() {
		cfg, err := NewConfig(WithArch("x86_64"))
		Expect(err).ToNot(HaveOccurred())
		Expect(cfg.Arch).To(Equal(constants.Archx86))
		Expect(cfg.Platform.String()).To(Equal("linux/amd64"))
	})

	It("builds a usable BuildConfig for every supported architecture", func() {
		for _, arch := range []string{"amd64", "x86_64", "arm64", "riscv64"} {
			cfg, err := NewBuildConfig(WithArch(arch))
			Expect(err).ToNot(HaveOccurred(), "arch %s", arch)
			Expect(cfg.Name).To(Equal(constants.BuildImgName))
		}
	})
})
