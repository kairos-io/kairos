package sysext

import (
	"archive/tar"
	"bytes"
	"io"
	"os"
	"path/filepath"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	sdkLogger "github.com/kairos-io/kairos/v4/sdk/types/logger"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// imageWithLayer builds a single layer image in memory from the tar entries
// write produces. The image under test is attacker controlled input, so these
// specs need to ship entries that no image builder would emit, which rules out
// building the image with docker the way the specs above do.
func imageWithLayer(write func(tw *tar.Writer)) v1.Image {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	write(tw)
	Expect(tw.Close()).To(Succeed())

	layer, err := tarball.LayerFromReader(io.NopCloser(bytes.NewReader(buf.Bytes())))
	Expect(err).ToNot(HaveOccurred())
	image, err := mutate.AppendLayers(empty.Image, layer)
	Expect(err).ToNot(HaveOccurred())
	return image
}

func writeDir(tw *tar.Writer, name string) {
	writeDirMode(tw, name, 0755)
}

func writeDirMode(tw *tar.Writer, name string, mode int64) {
	Expect(tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: name, Mode: mode})).To(Succeed())
}

func writeFile(tw *tar.Writer, name, content string) {
	writeFileMode(tw, name, content, 0644)
}

func writeFileMode(tw *tar.Writer, name, content string, mode int64) {
	Expect(tw.WriteHeader(&tar.Header{
		Typeflag: tar.TypeReg,
		Name:     name,
		Mode:     mode,
		Size:     int64(len(content)),
	})).To(Succeed())
	_, err := tw.Write([]byte(content))
	Expect(err).ToNot(HaveOccurred())
}

func writeSymlink(tw *tar.Writer, name, target string) {
	Expect(tw.WriteHeader(&tar.Header{
		Typeflag: tar.TypeSymlink,
		Name:     name,
		Linkname: target,
		Mode:     0777,
	})).To(Succeed())
}

var _ = Describe("extracting a layer", Label("sysext"), func() {
	var dst string
	var log sdkLogger.KairosLogger
	var buf bytes.Buffer

	BeforeEach(func() {
		var err error
		dst, err = os.MkdirTemp("", "sysext-dst-")
		Expect(err).ToNot(HaveOccurred())
		buf = bytes.Buffer{}
		log = sdkLogger.NewBufferLogger(&buf)
	})

	AfterEach(func() {
		Expect(os.RemoveAll(dst)).To(Succeed())
	})

	It("keeps the files it extracts inside the destination", func() {
		outside, err := os.MkdirTemp("", "sysext-outside-")
		Expect(err).ToNot(HaveOccurred())
		defer os.RemoveAll(outside)

		victim := filepath.Join(outside, "victim")
		Expect(os.WriteFile(victim, []byte("original"), 0644)).To(Succeed())

		// A symlink under /usr that leaves the destination, then a file that
		// the extraction would reach through it.
		image := imageWithLayer(func(tw *tar.Writer) {
			writeDir(tw, "usr/")
			writeSymlink(tw, "usr/escape", outside)
			writeFile(tw, "usr/escape/victim", "overwritten")
		})

		err = ExtractFilesFromLastLayer(image, dst, log, DefaultAllowListRegex)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("usr/escape/victim"))

		Expect(os.ReadFile(victim)).To(Equal([]byte("original")))
	})

	It("refuses a symlink whose relative target climbs above the destination", func() {
		image := imageWithLayer(func(tw *tar.Writer) {
			writeDir(tw, "usr/")
			writeSymlink(tw, "usr/escape", "../../../../etc/shadow")
		})

		err := ExtractFilesFromLastLayer(image, dst, log, DefaultAllowListRegex)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("points outside the extension root"))
	})

	It("does not leave the tail of a shorter rewrite behind", func() {
		image := imageWithLayer(func(tw *tar.Writer) {
			writeDir(tw, "usr/")
			writeFile(tw, "usr/config", "a-long-first-version")
			writeFile(tw, "usr/config", "short")
		})

		Expect(ExtractFilesFromLastLayer(image, dst, log, DefaultAllowListRegex)).To(Succeed())
		Expect(os.ReadFile(filepath.Join(dst, "usr", "config"))).To(Equal([]byte("short")))
	})

	It("extracts directories, files and symlinks, and keeps an absolute target absolute", func() {
		image := imageWithLayer(func(tw *tar.Writer) {
			writeDir(tw, "usr/")
			writeDir(tw, "usr/bin/")
			writeFile(tw, "usr/bin/vim", "binary")
			writeSymlink(tw, "usr/bin/vi", "/usr/bin/vim")
			writeSymlink(tw, "usr/bin/view", "vim")
			writeFile(tw, "var/nope", "ignored")
		})

		Expect(ExtractFilesFromLastLayer(image, dst, log, DefaultAllowListRegex)).To(Succeed())

		Expect(os.ReadFile(filepath.Join(dst, "usr", "bin", "vim"))).To(Equal([]byte("binary")))
		Expect(os.Readlink(filepath.Join(dst, "usr", "bin", "vi"))).To(Equal("/usr/bin/vim"))
		Expect(os.Readlink(filepath.Join(dst, "usr", "bin", "view"))).To(Equal("vim"))

		_, err := os.Stat(filepath.Join(dst, "var", "nope"))
		Expect(err).To(HaveOccurred())
	})

	It("keeps the setuid, setgid and sticky bits the layer ships", func() {
		// Every image that installs sudo, util-linux or passwd carries a
		// setuid or setgid binary, so an extraction that cannot write one is
		// broken for most real images.
		image := imageWithLayer(func(tw *tar.Writer) {
			writeDir(tw, "usr/")
			writeDir(tw, "usr/bin/")
			writeFileMode(tw, "usr/bin/sudo", "binary", 0o4755)
			writeFileMode(tw, "usr/bin/write", "binary", 0o2755)
			writeDirMode(tw, "usr/lib/shared/", 0o2775)
			writeDirMode(tw, "usr/tmp/", 0o1777)
		})

		Expect(ExtractFilesFromLastLayer(image, dst, log, DefaultAllowListRegex)).To(Succeed())

		sudo, err := os.Stat(filepath.Join(dst, "usr", "bin", "sudo"))
		Expect(err).ToNot(HaveOccurred())
		Expect(sudo.Mode() & os.ModeSetuid).ToNot(BeZero())
		Expect(sudo.Mode().Perm()).To(Equal(os.FileMode(0o755)))

		write, err := os.Stat(filepath.Join(dst, "usr", "bin", "write"))
		Expect(err).ToNot(HaveOccurred())
		Expect(write.Mode() & os.ModeSetgid).ToNot(BeZero())

		shared, err := os.Stat(filepath.Join(dst, "usr", "lib", "shared"))
		Expect(err).ToNot(HaveOccurred())
		Expect(shared.Mode() & os.ModeSetgid).ToNot(BeZero())
		Expect(shared.Mode().Perm()).To(Equal(os.FileMode(0o775)))

		tmp, err := os.Stat(filepath.Join(dst, "usr", "tmp"))
		Expect(err).ToNot(HaveOccurred())
		Expect(tmp.Mode() & os.ModeSticky).ToNot(BeZero())
		Expect(tmp.Mode().Perm()).To(Equal(os.FileMode(0o777)))
	})
})
