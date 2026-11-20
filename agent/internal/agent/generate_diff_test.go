package agent

import (
	"archive/tar"
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	agentConfig "github.com/kairos-io/kairos/v4/agent/pkg/config"
	"github.com/kairos-io/kairos/v4/sdk/diffupgrade"
	sdkLogger "github.com/kairos-io/kairos/v4/sdk/types/logger"
)

// imageArchive writes a docker save archive whose single layer holds files.
// Entries are owned by the current user so the test unpacks them unprivileged.
func imageArchive(path string, files map[string]string) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for name, content := range files {
		Expect(tw.WriteHeader(&tar.Header{Name: filepath.Dir(name) + "/", Typeflag: tar.TypeDir, Mode: 0o755, Uid: os.Getuid(), Gid: os.Getgid()})).To(Succeed())
		Expect(tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(content)), Uid: os.Getuid(), Gid: os.Getgid()})).To(Succeed())
		_, err := tw.Write([]byte(content))
		Expect(err).ToNot(HaveOccurred())
	}
	Expect(tw.Close()).To(Succeed())
	data := buf.Bytes()
	layer, err := tarball.LayerFromOpener(func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(data)), nil })
	Expect(err).ToNot(HaveOccurred())
	img, err := mutate.AppendLayers(empty.Image, layer)
	Expect(err).ToNot(HaveOccurred())
	Expect(tarball.WriteToFile(path, name.MustParseReference("example.org/kairos/base:v1").(name.Tag), img)).To(Succeed())
}

var _ = Describe("GenerateDiff", func() {
	It("diffs an image archive against a root filesystem tarball", func() {
		dir := GinkgoT().TempDir()
		base := filepath.Join(dir, "base.tar")
		imageArchive(base, map[string]string{
			"etc/kairos-release": "KAIROS_VERSION=v1\n",
			"usr/bin/same":       "same",
			"usr/lib/gone":       "gone",
		})

		rootfs := filepath.Join(dir, "rootfs")
		for name, content := range map[string]string{
			"etc/kairos-release": "KAIROS_VERSION=v2\n",
			"usr/bin/same":       "same",
			"usr/bin/new":        "new",
			"usr/lib/kept":       "kept",
		} {
			Expect(os.MkdirAll(filepath.Join(rootfs, filepath.Dir(name)), 0o755)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(rootfs, name), []byte(content), 0o644)).To(Succeed())
		}
		target := filepath.Join(dir, "target.tar.gz")
		Expect(exec.Command("tar", "-czf", target, "-C", rootfs, ".").Run()).To(Succeed())

		c := agentConfig.NewConfig(agentConfig.WithLogger(sdkLogger.NewBufferLogger(&bytes.Buffer{})))
		output := filepath.Join(dir, "diff.tar")
		Expect(GenerateDiff(c, base, target, output, "", dir)).To(Succeed())

		img, err := tarball.ImageFromPath(output, nil)
		Expect(err).ToNot(HaveOccurred())
		layers, err := img.Layers()
		Expect(err).ToNot(HaveOccurred())
		Expect(layers).To(HaveLen(1))
		rc, err := layers[0].Uncompressed()
		Expect(err).ToNot(HaveOccurred())
		defer rc.Close()

		names := []string{}
		var manifest []byte
		tr := tar.NewReader(rc)
		for {
			hdr, err := tr.Next()
			if err == io.EOF {
				break
			}
			Expect(err).ToNot(HaveOccurred())
			names = append(names, hdr.Name)
			if hdr.Name == diffupgrade.ManifestPath {
				manifest, err = io.ReadAll(tr)
				Expect(err).ToNot(HaveOccurred())
			}
		}
		Expect(names).To(ContainElements("etc/kairos-release", "usr/bin/new", "usr/lib/kept"))
		Expect(names).ToNot(ContainElement("usr/bin/same"), "unchanged files are left out")
		Expect(string(manifest)).To(ContainSubstring(`"usr/lib/gone"`))
		Expect(string(manifest)).To(ContainSubstring(`"base-ref": "base.tar"`))
		Expect(string(manifest)).To(ContainSubstring(`"target-ref": "target.tar.gz"`))

		entries, err := os.ReadDir(dir)
		Expect(err).ToNot(HaveOccurred())
		for _, e := range entries {
			Expect(e.Name()).ToNot(HavePrefix("kairos-diff-"), "the work directory is removed")
		}
	})
})
