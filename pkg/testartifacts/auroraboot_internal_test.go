package testartifacts

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const fakeBinary = "#!/bin/sh\necho fake\n"

func fakeTarball() []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	files := []struct {
		name, body string
		mode       int64
	}{
		{"LICENSE", "license\n", 0o644},
		{"README.md", "readme\n", 0o644},
		{"auroraboot", fakeBinary, 0o755},
	}
	for _, f := range files {
		Expect(tw.WriteHeader(&tar.Header{Name: f.name, Mode: f.mode, Size: int64(len(f.body)), Typeflag: tar.TypeReg})).To(Succeed())
		_, err := tw.Write([]byte(f.body))
		Expect(err).ToNot(HaveOccurred())
	}
	Expect(tw.Close()).To(Succeed())
	Expect(gz.Close()).To(Succeed())
	return buf.Bytes()
}

var _ = Describe("AuroraBootBinary", func() {
	var (
		server   *httptest.Server
		requests atomic.Int32
		cacheDir string
		asset    string
		tarball  []byte
		sums     func() string
	)

	BeforeEach(func() {
		asset = fmt.Sprintf("auroraboot_%s_linux_%s.tar.gz", AuroraBootVersion[1:], runtime.GOARCH)
		tarball = fakeTarball()
		requests.Store(0)
		sum := sha256.Sum256(tarball)
		sums = func() string { return hex.EncodeToString(sum[:]) + "  " + asset + "\n" }

		mux := http.NewServeMux()
		mux.HandleFunc("/"+AuroraBootVersion+"/checksums.txt", func(w http.ResponseWriter, r *http.Request) {
			requests.Add(1)
			_, _ = w.Write([]byte(sums()))
		})
		mux.HandleFunc("/"+AuroraBootVersion+"/"+asset, func(w http.ResponseWriter, r *http.Request) {
			requests.Add(1)
			_, _ = w.Write(tarball)
		})
		server = httptest.NewServer(mux)
		DeferCleanup(server.Close)

		cacheDir = GinkgoT().TempDir()
		origURL, origCache := auroraBootReleaseURL, auroraBootCacheDir
		DeferCleanup(func() { auroraBootReleaseURL, auroraBootCacheDir = origURL, origCache })
		auroraBootReleaseURL = server.URL
		auroraBootCacheDir = func() (string, error) { return cacheDir, nil }
		GinkgoT().Setenv("KAIROS_TEST_AURORABOOT_BINARY", "")
	})

	It("downloads, verifies and caches the binary", func() {
		path, err := AuroraBootBinary(GinkgoT().Context())
		Expect(err).ToNot(HaveOccurred())
		Expect(path).To(HavePrefix(cacheDir))
		info, err := os.Stat(path)
		Expect(err).ToNot(HaveOccurred())
		Expect(info.Mode().Perm() & 0o111).ToNot(BeZero())
		data, err := os.ReadFile(path)
		Expect(err).ToNot(HaveOccurred())
		Expect(string(data)).To(Equal(fakeBinary))
	})

	It("uses the cached binary without downloading again", func() {
		first, err := AuroraBootBinary(GinkgoT().Context())
		Expect(err).ToNot(HaveOccurred())
		n := requests.Load()
		Expect(n).To(BeNumerically(">", 0))
		second, err := AuroraBootBinary(GinkgoT().Context())
		Expect(err).ToNot(HaveOccurred())
		Expect(second).To(Equal(first))
		Expect(requests.Load()).To(Equal(n))
	})

	It("refuses a tarball whose checksum does not match", func() {
		sums = func() string { return fmt.Sprintf("%064d  %s\n", 1, asset) }
		_, err := AuroraBootBinary(GinkgoT().Context())
		Expect(err).To(MatchError(ContainSubstring("checksum")))
		matches, _ := filepath.Glob(filepath.Join(cacheDir, "*", "auroraboot"))
		Expect(matches).To(BeEmpty())
	})

	It("refuses a release without a checksum for this architecture", func() {
		sums = func() string { return fmt.Sprintf("%064d  other.tar.gz\n", 1) }
		_, err := AuroraBootBinary(GinkgoT().Context())
		Expect(err).To(HaveOccurred())
	})

	It("uses KAIROS_TEST_AURORABOOT_BINARY as is", func() {
		bin := filepath.Join(GinkgoT().TempDir(), "my-auroraboot")
		Expect(os.WriteFile(bin, []byte(fakeBinary), 0o755)).To(Succeed())
		GinkgoT().Setenv("KAIROS_TEST_AURORABOOT_BINARY", bin)
		path, err := AuroraBootBinary(GinkgoT().Context())
		Expect(err).ToNot(HaveOccurred())
		Expect(path).To(Equal(bin))
		Expect(requests.Load()).To(BeZero())
	})
})
