/*
Copyright © 2026 Kairos authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package imageextractor_test

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/kairos-io/kairos-agent/v2/pkg/implementations/imageextractor"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// newTarLayer builds a tarball layer containing the given files, owned by the
// current user so extraction does not require root privileges.
func newTarLayer(files map[string]string) ([]byte, error) {
	buf := &bytes.Buffer{}
	tw := tar.NewWriter(buf)
	for fileName, content := range files {
		hdr := &tar.Header{
			Name:     fileName,
			Mode:     0644,
			Size:     int64(len(content)),
			Typeflag: tar.TypeReg,
			Uid:      os.Getuid(),
			Gid:      os.Getgid(),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return nil, err
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			return nil, err
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

var _ = Describe("OCIImageExtractor", Label("imageextractor"), func() {
	var extractor imageextractor.OCIImageExtractor

	BeforeEach(func() {
		extractor = imageextractor.OCIImageExtractor{}
	})

	Describe("error paths (no network)", func() {
		It("fails to extract an unparseable image reference", func() {
			err := extractor.ExtractImage("Not A Valid Reference", GinkgoT().TempDir(), "")
			Expect(err).To(HaveOccurred())
		})

		It("falls back to the current platform on an invalid platform and still fails on a bad reference", func() {
			err := extractor.ExtractImage("Not A Valid Reference", GinkgoT().TempDir(), "this/has/too/many/slashes/to/be/valid")
			Expect(err).To(HaveOccurred())
		})

		It("fails to extract a bad reference with a valid platform", func() {
			err := extractor.ExtractImage("Not A Valid Reference", GinkgoT().TempDir(), "linux/amd64")
			Expect(err).To(HaveOccurred())
		})

		It("fails to get the size of an unparseable image reference", func() {
			_, err := extractor.GetOCIImageSize("Not A Valid Reference", "")
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("with a local registry", func() {
		var server *httptest.Server
		var imageRef string

		BeforeEach(func() {
			server = httptest.NewServer(registry.New(registry.Logger(log.New(io.Discard, "", 0))))

			serverURL, err := url.Parse(server.URL)
			Expect(err).ToNot(HaveOccurred())
			imageRef = fmt.Sprintf("%s/kairos/test-image:latest", serverURL.Host)

			layerBytes, err := newTarLayer(map[string]string{
				"keep.txt":    "keep me",
				"exclude.txt": "exclude me",
			})
			Expect(err).ToNot(HaveOccurred())

			layer, err := tarball.LayerFromOpener(func() (io.ReadCloser, error) {
				return io.NopCloser(bytes.NewReader(layerBytes)), nil
			})
			Expect(err).ToNot(HaveOccurred())

			img, err := mutate.AppendLayers(empty.Image, layer)
			Expect(err).ToNot(HaveOccurred())

			ref, err := name.ParseReference(imageRef)
			Expect(err).ToNot(HaveOccurred())
			Expect(remote.Write(ref, img)).To(Succeed())
		})

		AfterEach(func() {
			server.Close()
		})

		It("extracts the image contents to the destination", func() {
			dest := GinkgoT().TempDir()
			Expect(extractor.ExtractImage(imageRef, dest, "")).To(Succeed())

			content, err := os.ReadFile(filepath.Join(dest, "keep.txt"))
			Expect(err).ToNot(HaveOccurred())
			Expect(string(content)).To(Equal("keep me"))

			content, err = os.ReadFile(filepath.Join(dest, "exclude.txt"))
			Expect(err).ToNot(HaveOccurred())
			Expect(string(content)).To(Equal("exclude me"))
		})

		It("honours excludes during extraction", func() {
			dest := GinkgoT().TempDir()
			Expect(extractor.ExtractImage(imageRef, dest, "", "exclude.txt")).To(Succeed())

			_, err := os.Stat(filepath.Join(dest, "keep.txt"))
			Expect(err).ToNot(HaveOccurred())

			_, err = os.Stat(filepath.Join(dest, "exclude.txt"))
			Expect(os.IsNotExist(err)).To(BeTrue())
		})

		It("returns the size of the image", func() {
			size, err := extractor.GetOCIImageSize(imageRef, "")
			Expect(err).ToNot(HaveOccurred())
			Expect(size).To(BeNumerically(">", 0))
		})
	})
})
