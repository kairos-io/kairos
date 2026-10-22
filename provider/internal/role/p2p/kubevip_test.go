package role

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"time"

	providerConfig "github.com/kairos-io/kairos/v4/provider/internal/provider/config"
	"github.com/kairos-io/kairos/v4/sdk/verify"
	"github.com/kube-vip/kube-vip/pkg/kubevip"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("generateKubeVIP image", func() {
	BeforeEach(func() {
		// generateKubeVIP mutates package-level state — reset between specs.
		initConfig = kubevip.Config{}
		initLoadBalancer = kubevip.LoadBalancer{}
	})

	It("uses the default image when KubeVIP.Image is unset", func() {
		cfg := &providerConfig.Config{KubeVIP: providerConfig.KubeVIP{}}

		out, err := generateKubeVIP("daemonset", "eth0", "192.168.1.1", cfg)
		Expect(err).NotTo(HaveOccurred())
		Expect(out).To(ContainSubstring(DefaultKubeVIPImage + ":" + DefaultKubeVIPVersion))
	})

	It("uses the configured image when KubeVIP.Image is set", func() {
		cfg := &providerConfig.Config{
			KubeVIP: providerConfig.KubeVIP{
				Image: "my-registry.example.com/kube-vip/kube-vip",
			},
		}

		out, err := generateKubeVIP("daemonset", "eth0", "192.168.1.1", cfg)
		Expect(err).NotTo(HaveOccurred())
		Expect(out).To(ContainSubstring("my-registry.example.com/kube-vip/kube-vip:" + DefaultKubeVIPVersion))
		Expect(out).NotTo(ContainSubstring(DefaultKubeVIPImage + ":"))
	})

	It("respects the configured Version alongside a custom Image", func() {
		cfg := &providerConfig.Config{
			KubeVIP: providerConfig.KubeVIP{
				Image:   "my-registry.example.com/kube-vip/kube-vip",
				Version: "v1.0.0",
			},
		}

		out, err := generateKubeVIP("pod", "eth0", "192.168.1.1", cfg)
		Expect(err).NotTo(HaveOccurred())
		Expect(out).To(ContainSubstring("my-registry.example.com/kube-vip/kube-vip:v1.0.0"))
	})
})

var _ = Describe("downloadFromURL", func() {
	var dir string

	BeforeEach(func() {
		var err error
		dir, err = os.MkdirTemp("", "kubevip-download-*")
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		Expect(os.RemoveAll(dir)).To(Succeed())
	})

	It("writes the response body to the destination on 200", func() {
		body := "apiVersion: v1\nkind: ConfigMap\n"
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(body))
		}))
		defer srv.Close()

		dst := filepath.Join(dir, "out.yaml")
		Expect(downloadFromURL(srv.URL, dst)).To(Succeed())

		got, err := os.ReadFile(dst)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(got)).To(Equal(body))
	})

	It("returns an error on non-2xx responses without creating the destination file", func() {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "not found", http.StatusNotFound)
		}))
		defer srv.Close()

		dst := filepath.Join(dir, "out.yaml")
		Expect(downloadFromURL(srv.URL, dst)).To(MatchError(ContainSubstring("404")))

		// The kubelet watches this directory; even an empty file at the
		// final path could be picked up. Assert nothing was created.
		_, err := os.Stat(dst)
		Expect(err).To(HaveOccurred())
		Expect(os.IsNotExist(err)).To(BeTrue(), "the HTTP error page must not leave a file at the manifest path")
	})

	It("times out instead of hanging forever when the server stalls", func() {
		block := make(chan struct{})
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			<-block
		}))
		// Release the blocked handler before Close(): httptest.Server.Close
		// waits for outstanding handlers to return, so ordering here matters.
		defer srv.Close()
		defer close(block)

		done := make(chan error, 1)
		go func() {
			done <- downloadFromURL(srv.URL, filepath.Join(dir, "stall.yaml"))
		}()

		select {
		case err := <-done:
			// net/http surfaces client-side timeouts as errors whose
			// message contains "Client.Timeout"; pin on that so a
			// different failure mode (e.g. connection refused) does
			// not silently satisfy the assertion.
			Expect(err).To(MatchError(ContainSubstring("Client.Timeout")))
		case <-time.After(30 * time.Second):
			Fail("downloadFromURL did not honor a request timeout within 30s")
		}
	})
})

func sha256Sum(b []byte) verify.SHA256Sum {
	sum := sha256.Sum256(b)
	return verify.SHA256Sum(hex.EncodeToString(sum[:]))
}

var _ = Describe("downloadVerifiedManifest", func() {
	var dir string

	BeforeEach(func() {
		var err error
		dir, err = os.MkdirTemp("", "kubevip-verified-download-*")
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		Expect(os.RemoveAll(dir)).To(Succeed())
	})

	It("writes the response body when its digest matches want", func() {
		body := []byte("apiVersion: v1\nkind: ConfigMap\n")
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(body)
		}))
		defer srv.Close()

		dst := filepath.Join(dir, "kubevipmanifest.yaml")
		Expect(downloadVerifiedManifest(srv.URL, dst, sha256Sum(body))).To(Succeed())

		got, err := os.ReadFile(dst)
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal(body))
	})

	It("refuses to write a manifest whose digest does not match want, an operator's own pin catching a compromised ManifestURL", func() {
		served := []byte("kind: ConfigMap\ndata: {malicious: true}\n")
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(served)
		}))
		defer srv.Close()

		dst := filepath.Join(dir, "kubevipmanifest.yaml")
		want := sha256Sum([]byte("kind: ConfigMap\ndata: {expected: true}\n"))
		Expect(downloadVerifiedManifest(srv.URL, dst, want)).NotTo(Succeed())

		// The kubelet watches this directory; a half-verified file here is as
		// bad as an unverified one.
		_, err := os.Stat(dst)
		Expect(os.IsNotExist(err)).To(BeTrue(), "a digest mismatch must not leave a file at the manifest path")
	})
})
