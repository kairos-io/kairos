package config_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/AuroraBoot/internal/config"
)

var _ = Describe("ReadCloudConfig", func() {
	// The cloud-config templates use [[[ ]]] delimiters (so they don't clash with
	// the YAML/Go the config itself may contain).
	const tmpl = "hostname: [[[ .name ]]]\n"

	It("reads a file and renders template values into it", func() {
		dir := GinkgoT().TempDir()
		path := filepath.Join(dir, "cc.yaml")
		Expect(os.WriteFile(path, []byte(tmpl), 0o644)).To(Succeed())

		out, err := config.ReadCloudConfig(path, map[string]interface{}{"name": "node-a"})
		Expect(err).NotTo(HaveOccurred())
		Expect(out).To(Equal("hostname: node-a\n"))
	})

	It("reads from a URL and renders it", func() {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(tmpl))
		}))
		DeferCleanup(srv.Close)

		out, err := config.ReadCloudConfig(srv.URL, map[string]interface{}{"name": "node-b"})
		Expect(err).NotTo(HaveOccurred())
		Expect(out).To(Equal("hostname: node-b\n"))
	})

	It("reads from STDIN when the source is '-'", func() {
		r, w, err := os.Pipe()
		Expect(err).NotTo(HaveOccurred())
		_, _ = w.WriteString(tmpl)
		Expect(w.Close()).To(Succeed())
		old := os.Stdin
		os.Stdin = r
		DeferCleanup(func() { os.Stdin = old })

		out, err := config.ReadCloudConfig("-", map[string]interface{}{"name": "node-c"})
		Expect(err).NotTo(HaveOccurred())
		Expect(out).To(Equal("hostname: node-c\n"))
	})

	It("errors when the file does not exist", func() {
		_, err := config.ReadCloudConfig("/no/such/cloud-config.yaml", nil)
		Expect(err).To(MatchError(ContainSubstring("not found")))
	})

	It("errors when the rendered content is empty", func() {
		dir := GinkgoT().TempDir()
		path := filepath.Join(dir, "empty.yaml")
		Expect(os.WriteFile(path, []byte(""), 0o644)).To(Succeed())

		_, err := config.ReadCloudConfig(path, nil)
		Expect(err).To(MatchError(ContainSubstring("empty")))
	})

	It("errors on a malformed template", func() {
		dir := GinkgoT().TempDir()
		path := filepath.Join(dir, "bad.yaml")
		// Unclosed action delimiter — the template parser rejects it.
		Expect(os.WriteFile(path, []byte("x: [[[ .name \n"), 0o644)).To(Succeed())

		_, err := config.ReadCloudConfig(path, map[string]interface{}{"name": "x"})
		Expect(err).To(HaveOccurred())
	})
})
