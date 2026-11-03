package ops

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// freeAddr asks the OS for a currently-free TCP port and returns it as a
// host:port string, so two ServeArtifacts instances can bind distinct ports.
func freeAddr() string {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	Expect(err).NotTo(HaveOccurred())
	addr := l.Addr().String()
	Expect(l.Close()).To(Succeed())
	return addr
}

var _ = Describe("noListingFS", func() {
	var dir string

	BeforeEach(func() {
		dir = GinkgoT().TempDir()
		Expect(os.WriteFile(filepath.Join(dir, "artifact.txt"), []byte("data"), 0o644)).To(Succeed())
		Expect(os.Mkdir(filepath.Join(dir, "sub"), 0o755)).To(Succeed())
	})

	It("serves a regular file by its exact path", func() {
		f, err := (noListingFS{http.Dir(dir)}).Open("/artifact.txt")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(f.Close)
		info, err := f.Stat()
		Expect(err).NotTo(HaveOccurred())
		Expect(info.IsDir()).To(BeFalse())
	})

	It("hides the root directory as not-exist", func() {
		_, err := (noListingFS{http.Dir(dir)}).Open("/")
		Expect(err).To(MatchError(os.ErrNotExist))
	})

	It("hides a subdirectory as not-exist", func() {
		_, err := (noListingFS{http.Dir(dir)}).Open("/sub")
		Expect(err).To(MatchError(os.ErrNotExist))
	})

	It("passes through the underlying error for a missing path", func() {
		_, err := (noListingFS{http.Dir(dir)}).Open("/does-not-exist")
		Expect(err).To(HaveOccurred())
		Expect(os.IsNotExist(err)).To(BeTrue())
	})
})

var _ = Describe("ServeArtifacts", Label("network"), func() {
	// ServeArtifacts must register its file handler on a private mux, not the
	// global http.DefaultServeMux: registering "/" globally panics on the second
	// call. Starting two instances proves no global state is shared.
	It("starts two instances without panicking and serves the configured dir", func() {
		dirA := GinkgoT().TempDir()
		dirB := GinkgoT().TempDir()
		Expect(os.WriteFile(filepath.Join(dirA, "a.txt"), []byte("from-A"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(dirB, "b.txt"), []byte("from-B"), 0o644)).To(Succeed())

		addrA := freeAddr()
		addrB := freeAddr()

		ctx, cancel := context.WithCancel(context.Background())
		DeferCleanup(cancel)

		start := func(addr, dir string) {
			fn := ServeArtifacts(addr, func() string { return dir })
			go func() {
				defer GinkgoRecover()
				// ListenAndServe returns ErrServerClosed on Shutdown; both
				// instances registering "/" must NOT panic on a shared mux.
				_ = fn(ctx)
			}()
		}
		start(addrA, dirA)
		start(addrB, dirB)

		get := func(addr, path string) (int, string) {
			var code int
			var body string
			Eventually(func() error {
				resp, err := http.Get(fmt.Sprintf("http://%s/%s", addr, path))
				if err != nil {
					return err
				}
				defer resp.Body.Close()
				b, _ := io.ReadAll(resp.Body)
				code = resp.StatusCode
				body = string(b)
				return nil
			}, 5*time.Second, 50*time.Millisecond).Should(Succeed())
			return code, body
		}

		codeA, bodyA := get(addrA, "a.txt")
		Expect(codeA).To(Equal(http.StatusOK))
		Expect(bodyA).To(Equal("from-A"))

		codeB, bodyB := get(addrB, "b.txt")
		Expect(codeB).To(Equal(http.StatusOK))
		Expect(bodyB).To(Equal("from-B"))
	})

	It("serves files by path but does not list the directory", func() {
		dir := GinkgoT().TempDir()
		Expect(os.WriteFile(filepath.Join(dir, "secret-artifact.iso"), []byte("payload"), 0o644)).To(Succeed())

		addr := freeAddr()
		ctx, cancel := context.WithCancel(context.Background())
		DeferCleanup(cancel)
		go func() {
			defer GinkgoRecover()
			_ = ServeArtifacts(addr, func() string { return dir })(ctx)
		}()

		get := func(path string) (int, string) {
			var code int
			var body string
			Eventually(func() error {
				resp, err := http.Get(fmt.Sprintf("http://%s/%s", addr, path))
				if err != nil {
					return err
				}
				defer resp.Body.Close()
				b, _ := io.ReadAll(resp.Body)
				code, body = resp.StatusCode, string(b)
				return nil
			}, 5*time.Second, 50*time.Millisecond).Should(Succeed())
			return code, body
		}

		// A known file is still served by its exact path.
		code, body := get("secret-artifact.iso")
		Expect(code).To(Equal(http.StatusOK))
		Expect(body).To(Equal("payload"))

		// The directory root is not browsable: it 404s and never leaks the
		// filenames it contains.
		rootCode, rootBody := get("")
		Expect(rootCode).To(Equal(http.StatusNotFound))
		Expect(rootBody).NotTo(ContainSubstring("secret-artifact.iso"))
	})
})
