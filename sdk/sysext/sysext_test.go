package sysext

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/daemon"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	sdkLogger "github.com/kairos-io/kairos/v4/sdk/types/logger"
	"github.com/kairos-io/kairos/v4/sdk/utils"
	imageUtils "github.com/kairos-io/kairos/v4/sdk/utils/image"
	"github.com/moby/moby/client"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestSuite(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "sysext Test Suite")
}

var _ = Describe("sysext", Label("sysext"), Ordered, func() {
	var dest string
	var image v1.Image
	var imageTag string
	var buf bytes.Buffer
	var log sdkLogger.KairosLogger
	var err error

	BeforeEach(func() {
		buf = bytes.Buffer{}
		log = sdkLogger.NewBufferLogger(&buf)
		dest, err = os.MkdirTemp("", "")
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		if CurrentSpecReport().Failed() {
			_, _ = GinkgoWriter.Write(buf.Bytes())
		}
		Expect(os.RemoveAll(dest)).To(Succeed())
	})

	When("Using a normal image", func() {
		BeforeEach(func() {
			imageTag = createTestDockerImage()
			By(fmt.Sprintf("Created image %s", imageTag))
			image, err = imageUtils.GetImage(imageTag, utils.GetCurrentPlatform(), nil, nil)
			Expect(err).ToNot(HaveOccurred())
		})
		AfterEach(func() {
			cli, _ := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
			_, _ = cli.ImageRemove(context.Background(), imageTag, client.ImageRemoveOptions{Force: true})
			By(fmt.Sprintf("Removed image %s", imageTag))
		})
		It("should extract the files into the dir", func() {
			err = ExtractFilesFromLastLayer(image, dest, log, DefaultAllowListRegex)
			Expect(err).ToNot(HaveOccurred())
			_, err := os.Stat(filepath.Join(dest, "usr", "yes"))
			Expect(err).ToNot(HaveOccurred())
			_, err = os.Stat(filepath.Join(dest, "etc", "yes"))
			Expect(err).ToNot(HaveOccurred())
			_, err = os.Stat(filepath.Join(dest, "opt", "nope"))
			Expect(err).To(HaveOccurred())
			_, err = os.Stat(filepath.Join(dest, "var", "nope"))
			Expect(err).To(HaveOccurred())
		})
		It("properly uses the allowList", func() {
			allowList := regexp.MustCompile(`^var|^/var`)
			err = ExtractFilesFromLastLayer(image, dest, log, allowList)
			Expect(err).ToNot(HaveOccurred())
			_, err := os.Stat(filepath.Join(dest, "usr", "yes"))
			Expect(err).To(HaveOccurred())
			_, err = os.Stat(filepath.Join(dest, "etc", "yes"))
			Expect(err).To(HaveOccurred())
			_, err = os.Stat(filepath.Join(dest, "opt", "nope"))
			Expect(err).To(HaveOccurred())
			_, err = os.Stat(filepath.Join(dest, "var", "nope"))
			Expect(err).ToNot(HaveOccurred())
		})
	})

	When("Using an empty image", func() {
		BeforeEach(func() {
			imageTag = createEmptyDockerImage()
			By(fmt.Sprintf("Created image %s", imageTag))
			image, err = imageUtils.GetImage(imageTag, utils.GetCurrentPlatform(), nil, nil)
			Expect(err).ToNot(HaveOccurred())
		})
		AfterEach(func() {
			cli, _ := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
			_, _ = cli.ImageRemove(context.Background(), imageTag, client.ImageRemoveOptions{Force: true})
			By(fmt.Sprintf("Removed image %s", imageTag))
		})
		It("Fails with no layers image", func() {
			// Cleanup existing image before creating a new one

			err = ExtractFilesFromLastLayer(image, dest, log, DefaultAllowListRegex)
			Expect(err).To(HaveOccurred())
			Expect(err).To(Equal(ErrorImageNoLayers))
		})
	})
})

var _ = Describe("ExtractFilesFromLastLayer", Label("sysext"), func() {
	var parent, dest string
	var log sdkLogger.KairosLogger

	BeforeEach(func() {
		var err error
		parent, err = os.MkdirTemp("", "")
		Expect(err).ToNot(HaveOccurred())
		dest = filepath.Join(parent, "dest")
		Expect(os.Mkdir(dest, 0o755)).To(Succeed())
		log = sdkLogger.NewBufferLogger(&bytes.Buffer{})
	})

	AfterEach(func() {
		Expect(os.RemoveAll(parent)).To(Succeed())
	})

	It("refuses to write through a symlink that points outside the destination", func() {
		outside := filepath.Join(parent, "outside")
		Expect(os.Mkdir(outside, 0o755)).To(Succeed())
		image := imageWithLayer(
			tar.Header{Typeflag: tar.TypeDir, Name: "usr/", Mode: 0o755},
			tar.Header{Typeflag: tar.TypeSymlink, Name: "usr/escape", Linkname: outside},
			tar.Header{Typeflag: tar.TypeReg, Name: "usr/escape/escaped", Mode: 0o644},
		)
		Expect(ExtractFilesFromLastLayer(image, dest, log, DefaultAllowListRegex)).ToNot(Succeed())
		Expect(filepath.Join(outside, "escaped")).ToNot(BeAnExistingFile())
	})

	It("refuses an entry that climbs out of the destination", func() {
		image := imageWithLayer(
			tar.Header{Typeflag: tar.TypeReg, Name: "../escaped", Mode: 0o644},
		)
		Expect(ExtractFilesFromLastLayer(image, dest, log, regexp.MustCompile(`.*`))).ToNot(Succeed())
		Expect(filepath.Join(parent, "escaped")).ToNot(BeAnExistingFile())
	})

	It("creates the parent directories a layer does not list", func() {
		image := imageWithLayer(
			tar.Header{Typeflag: tar.TypeReg, Name: "usr/lib/extension-release.d/extension-release.test", Mode: 0o644},
		)
		Expect(ExtractFilesFromLastLayer(image, dest, log, DefaultAllowListRegex)).To(Succeed())
		Expect(filepath.Join(dest, "usr/lib/extension-release.d/extension-release.test")).To(BeAnExistingFile())
	})

	It("preserves the setuid bit on an extracted file", func() {
		image := imageWithLayer(
			tar.Header{Typeflag: tar.TypeReg, Name: "usr/bin/tool", Mode: 0o4755},
		)
		Expect(ExtractFilesFromLastLayer(image, dest, log, DefaultAllowListRegex)).To(Succeed())
		info, err := os.Stat(filepath.Join(dest, "usr/bin/tool"))
		Expect(err).ToNot(HaveOccurred())
		Expect(info.Mode() & os.ModeSetuid).ToNot(BeZero())
	})

	It("truncates an existing file instead of leaving stale trailing bytes", func() {
		target := filepath.Join(dest, "usr", "data")
		Expect(os.MkdirAll(filepath.Dir(target), 0o755)).To(Succeed())
		Expect(os.WriteFile(target, []byte("the old content is longer"), 0o644)).To(Succeed())
		image := imageWithLayer(
			tar.Header{Typeflag: tar.TypeReg, Name: "usr/data", Mode: 0o644},
		)
		Expect(ExtractFilesFromLastLayer(image, dest, log, DefaultAllowListRegex)).To(Succeed())
		content, err := os.ReadFile(target)
		Expect(err).ToNot(HaveOccurred())
		Expect(content).To(BeEmpty())
	})

	It("keeps absolute symlinks, which resolve against the merged system", func() {
		image := imageWithLayer(
			tar.Header{Typeflag: tar.TypeDir, Name: "usr/", Mode: 0o755},
			tar.Header{Typeflag: tar.TypeSymlink, Name: "usr/link", Linkname: "/usr/lib/real"},
		)
		Expect(ExtractFilesFromLastLayer(image, dest, log, DefaultAllowListRegex)).To(Succeed())
		Expect(os.Readlink(filepath.Join(dest, "usr/link"))).To(Equal("/usr/lib/real"))
	})
})

// imageWithLayer returns an in-memory image whose only layer holds the given
// entries, each regular file empty.
func imageWithLayer(entries ...tar.Header) v1.Image {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for i := range entries {
		Expect(tw.WriteHeader(&entries[i])).To(Succeed())
	}
	Expect(tw.Close()).To(Succeed())
	layer, err := tarball.LayerFromOpener(func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(buf.Bytes())), nil
	})
	Expect(err).ToNot(HaveOccurred())
	image, err := mutate.AppendLayers(empty.Image, layer)
	Expect(err).ToNot(HaveOccurred())
	return image
}

func createEmptyDockerImage() string {
	var letterRunes = []rune("abcdefghijklmnopqrstuvwxyz0123456789")

	b := make([]rune, 8)
	for i := range b {
		b[i] = letterRunes[rand.Intn(len(letterRunes))]
	}

	img, err := mutate.AppendLayers(empty.Image)
	Expect(err).ToNot(HaveOccurred())

	// Set the platform to AMD64
	cfg, err := img.ConfigFile()
	Expect(err).ToNot(HaveOccurred())
	cfg.Architecture = "amd64"
	cfg.OS = "linux"
	img, err = mutate.ConfigFile(img, cfg)
	Expect(err).ToNot(HaveOccurred())

	tag, err := name.NewTag(fmt.Sprintf("kairos-empty-%s:latest", string(b)))
	Expect(err).ToNot(HaveOccurred())
	_, err = daemon.Write(tag, img)
	Expect(err).ToNot(HaveOccurred())

	return tag.String()
}

func createTestDockerImage() string {
	var letterRunes = []rune("abcdefghijklmnopqrstuvwxyz0123456789")

	b := make([]rune, 8)
	for i := range b {
		b[i] = letterRunes[rand.Intn(len(letterRunes))]
	}

	// We don't care about this layer so make it a bit fake
	fistLayer, _ := crane.Layer(map[string][]byte{
		"/etc/one":     []byte("hello"),
		"/etc/another": []byte("world"),
	})

	secondLayer, err := sysextLayer()
	Expect(err).ToNot(HaveOccurred())
	img, err := mutate.AppendLayers(empty.Image, fistLayer, secondLayer)
	Expect(err).ToNot(HaveOccurred())

	// Set the platform to AMD64
	cfg, err := img.ConfigFile()
	Expect(err).ToNot(HaveOccurred())
	cfg.Architecture = "amd64"
	cfg.OS = "linux"
	img, err = mutate.ConfigFile(img, cfg)
	Expect(err).ToNot(HaveOccurred())

	tag, err := name.NewTag(fmt.Sprintf("kairos-test-%s:latest", string(b)))
	Expect(err).ToNot(HaveOccurred())
	_, err = daemon.Write(tag, img)
	Expect(err).ToNot(HaveOccurred())

	return tag.String()
}

// sysextLayer returns a layer with an empty file in each of /usr and /etc,
// which are sysext hierarchies, and in each of /var and /opt, which are not.
// Every directory has its own entry, as in a layer built by docker.
func sysextLayer() (v1.Layer, error) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, dir := range []string{"usr", "etc", "var", "opt"} {
		if err := tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: "./" + dir + "/", Mode: 0o755}); err != nil {
			return nil, err
		}
	}
	for _, file := range []string{"usr/yes", "etc/yes", "var/nope", "opt/nope"} {
		if err := tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "./" + file, Mode: 0o644}); err != nil {
			return nil, err
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	return tarball.LayerFromOpener(func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(buf.Bytes())), nil
	})
}
