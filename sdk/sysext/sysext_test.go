package sysext

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"syscall"
	"testing"
	"time"

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
	"golang.org/x/sys/unix"
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
		It("Fails with no layers image", func() {
			// An in-memory image: the Docker daemon refuses to export an
			// image with no layers, so a daemon image never gets this far.
			err = ExtractFilesFromLastLayer(empty.Image, dest, log, DefaultAllowListRegex)
			Expect(err).To(HaveOccurred())
			Expect(err).To(Equal(ErrorImageNoLayers))
		})
	})
})

var _ = Describe("ExtractFilesFromLastLayer", Label("sysext"), func() {
	var parent, dest string
	var logs *bytes.Buffer
	var log sdkLogger.KairosLogger

	BeforeEach(func() {
		var err error
		parent, err = os.MkdirTemp("", "")
		Expect(err).ToNot(HaveOccurred())
		dest = filepath.Join(parent, "dest")
		Expect(os.Mkdir(dest, 0o755)).To(Succeed())
		logs = &bytes.Buffer{}
		log = sdkLogger.NewBufferLogger(logs)
	})

	AfterEach(func() {
		Expect(os.RemoveAll(parent)).To(Succeed())
	})

	It("returns the error when the image cannot list its layers", func() {
		image := layersErrImage{Image: imageWithLayer(), err: errors.New("manifest unavailable")}
		err := ExtractFilesFromLastLayer(image, dest, log, DefaultAllowListRegex)
		Expect(err).To(MatchError(ContainSubstring("manifest unavailable")))
		Expect(err).ToNot(MatchError(ErrorImageNoLayers))
	})

	It("returns the error instead of panicking when a layer cannot be fetched", func() {
		image := layersErrImage{Image: imageWithLayer(), layerErr: errors.New("403 Forbidden")}
		var err error
		Expect(func() {
			err = ExtractFilesFromLastLayer(image, dest, log, DefaultAllowListRegex)
		}).ToNot(Panic())
		Expect(err).To(MatchError(ContainSubstring("403 Forbidden")))
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

	It("extracts a hard link as a hard link, not as a copy", func() {
		image := imageWithLayer(
			tar.Header{Typeflag: tar.TypeReg, Name: "usr/bin/git", Mode: 0o755},
			tar.Header{Typeflag: tar.TypeLink, Name: "usr/libexec/git-core/git-add", Linkname: "usr/bin/git", Mode: 0o755},
			tar.Header{Typeflag: tar.TypeLink, Name: "usr/libexec/git-core/git-log", Linkname: "/usr/bin/git", Mode: 0o755},
		)
		Expect(ExtractFilesFromLastLayer(image, dest, log, DefaultAllowListRegex)).To(Succeed())
		target, err := os.Lstat(filepath.Join(dest, "usr/bin/git"))
		Expect(err).ToNot(HaveOccurred())
		for _, helper := range []string{"git-add", "git-log"} {
			info, err := os.Lstat(filepath.Join(dest, "usr/libexec/git-core", helper))
			Expect(err).ToNot(HaveOccurred(), helper)
			// A copy would pass an existence check, so compare the inode.
			Expect(os.SameFile(target, info)).To(BeTrue(), helper)
		}
	})

	It("extracts a hard link whose target carries the leading dot GNU tar writes", func() {
		// "tar -C rootfs -c ." names every entry "./usr/...", including the
		// target a hard link points at.
		image := imageWithLayer(
			tar.Header{Typeflag: tar.TypeReg, Name: "./usr/bin/git", Mode: 0o755},
			tar.Header{Typeflag: tar.TypeLink, Name: "./usr/libexec/git-core/git-add", Linkname: "./usr/bin/git", Mode: 0o755},
		)
		Expect(ExtractFilesFromLastLayer(image, dest, log, DefaultAllowListRegex)).To(Succeed())
		target, err := os.Lstat(filepath.Join(dest, "usr/bin/git"))
		Expect(err).ToNot(HaveOccurred())
		info, err := os.Lstat(filepath.Join(dest, "usr/libexec/git-core/git-add"))
		Expect(err).ToNot(HaveOccurred())
		Expect(os.SameFile(target, info)).To(BeTrue())
	})

	It("refuses a hard link whose target climbs out of the destination", func() {
		image := imageWithLayer(
			tar.Header{Typeflag: tar.TypeLink, Name: "usr/escaped", Linkname: "usr/../../outside", Mode: 0o644},
		)
		err := ExtractFilesFromLastLayer(image, dest, log, DefaultAllowListRegex)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("usr/escaped"))
	})

	It("drops a hard link the allow list excluded, and keeps the rest of the layer", func() {
		image := imageWithLayer(
			tar.Header{Typeflag: tar.TypeReg, Name: "usr/bin/tool", Mode: 0o755},
			tar.Header{Typeflag: tar.TypeLink, Name: "usr/bin/ls", Linkname: "bin/busybox", Mode: 0o755},
		)
		Expect(ExtractFilesFromLastLayer(image, dest, log, DefaultAllowListRegex)).To(Succeed())
		Expect(filepath.Join(dest, "usr/bin/tool")).To(BeAnExistingFile())
		Expect(filepath.Join(dest, "usr/bin/ls")).ToNot(BeAnExistingFile())
		Expect(logs.String()).To(ContainSubstring("usr/bin/ls"))
		Expect(logs.String()).To(ContainSubstring("bin/busybox"))
	})

	It("skips whiteout markers instead of writing them as files, and warns about the dropped deletion", func() {
		image := imageWithLayer(
			tar.Header{Typeflag: tar.TypeReg, Name: "usr/bin/.wh.env", Mode: 0o000},
			tar.Header{Typeflag: tar.TypeReg, Name: "usr/share/foo/.wh..wh..opq", Mode: 0o000},
			tar.Header{Typeflag: tar.TypeReg, Name: "usr/bin/newtool", Mode: 0o755},
		)
		Expect(ExtractFilesFromLastLayer(image, dest, log, DefaultAllowListRegex)).To(Succeed())
		Expect(filepath.Join(dest, "usr/bin/newtool")).To(BeAnExistingFile())
		Expect(filepath.Join(dest, "usr/bin/.wh.env")).ToNot(BeAnExistingFile())
		Expect(filepath.Join(dest, "usr/share/foo/.wh..wh..opq")).ToNot(BeAnExistingFile())
		Expect(logs.String()).To(ContainSubstring("usr/bin/env"))
		Expect(logs.String()).To(ContainSubstring("usr/share/foo"))
	})
})

// layersErrImage wraps an image so that Layers fails with err, or, when only
// layerErr is set, returns layers whose Uncompressed fails with layerErr, the
// way a remote image does when a blob download is refused.
type layersErrImage struct {
	v1.Image
	err      error
	layerErr error
}

func (i layersErrImage) Layers() ([]v1.Layer, error) {
	if i.err != nil {
		return nil, i.err
	}
	layers, err := i.Image.Layers()
	if err != nil {
		return nil, err
	}
	for n := range layers {
		layers[n] = uncompressedErrLayer{Layer: layers[n], err: i.layerErr}
	}
	return layers, nil
}

type uncompressedErrLayer struct {
	v1.Layer
	err error
}

func (l uncompressedErrLayer) Uncompressed() (io.ReadCloser, error) {
	return nil, l.err
}

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

var _ = Describe("ExtractFilesFromLastLayer metadata", Label("sysext"), func() {
	var parent, dest string
	var logs *bytes.Buffer
	var log sdkLogger.KairosLogger
	mtime := time.Date(2001, 2, 3, 4, 5, 6, 0, time.UTC)

	BeforeEach(func() {
		var err error
		// The default temporary directory can be a filesystem without user
		// xattrs, such as tmpfs on older kernels.
		parent, err = os.MkdirTemp(os.Getenv("KAIROS_TEST_TMPDIR"), "")
		Expect(err).ToNot(HaveOccurred())
		dest = filepath.Join(parent, "dest")
		Expect(os.Mkdir(dest, 0o755)).To(Succeed())
		logs = &bytes.Buffer{}
		log = sdkLogger.NewBufferLogger(logs)
	})

	AfterEach(func() {
		if CurrentSpecReport().Failed() {
			_, _ = GinkgoWriter.Write(logs.Bytes())
		}
		Expect(os.RemoveAll(parent)).To(Succeed())
	})

	extract := func(entries ...tar.Header) {
		Expect(ExtractFilesFromLastLayer(imageWithLayer(entries...), dest, log, DefaultAllowListRegex)).To(Succeed())
	}

	lstat := func(name string) *syscall.Stat_t {
		info, err := os.Lstat(filepath.Join(dest, name))
		Expect(err).ToNot(HaveOccurred())
		return info.Sys().(*syscall.Stat_t)
	}

	getxattr := func(name, attr string) string {
		buf := make([]byte, 64)
		n, err := unix.Lgetxattr(filepath.Join(dest, name), attr, buf)
		if errors.Is(err, unix.ENOTSUP) {
			Skip("filesystem has no user xattr support")
		}
		Expect(err).ToNot(HaveOccurred())
		return string(buf[:n])
	}

	It("restores group, xattrs and mtime of a PAX entry", func() {
		gid := layerGid()
		extract(
			tar.Header{Typeflag: tar.TypeDir, Name: "usr/", Mode: 0o755},
			tar.Header{
				Typeflag:   tar.TypeReg,
				Name:       "usr/file",
				Mode:       0o644,
				Gid:        gid,
				ModTime:    mtime,
				Format:     tar.FormatPAX,
				PAXRecords: map[string]string{"SCHILY.xattr.user.kairos": "5886"},
			},
		)
		info, err := os.Stat(filepath.Join(dest, "usr/file"))
		Expect(err).ToNot(HaveOccurred())
		Expect(info.ModTime().Equal(mtime)).To(BeTrue(), "mtime is %s", info.ModTime())
		Expect(int(lstat("usr/file").Gid)).To(Equal(gid))
		Expect(getxattr("usr/file", "user.kairos")).To(Equal("5886"))
	})

	It("keeps the setuid bit of a file whose owner is changed", func() {
		uid, gid := layerUid(), layerGid()
		extract(
			tar.Header{Typeflag: tar.TypeReg, Name: "usr/bin/tool", Mode: 0o4755, Uid: uid, Gid: gid},
		)
		info, err := os.Stat(filepath.Join(dest, "usr/bin/tool"))
		Expect(err).ToNot(HaveOccurred())
		Expect(info.Mode() & os.ModeSetuid).ToNot(BeZero())
		st := info.Sys().(*syscall.Stat_t)
		Expect(int(st.Uid)).To(Equal(uid))
		Expect(int(st.Gid)).To(Equal(gid))
	})

	It("keeps the mtime of a directory after entries are written into it", func() {
		usrTime := mtime.Add(time.Hour)
		extract(
			tar.Header{Typeflag: tar.TypeDir, Name: "usr/", Mode: 0o755, ModTime: usrTime},
			tar.Header{Typeflag: tar.TypeDir, Name: "usr/share/", Mode: 0o755, ModTime: mtime},
			tar.Header{Typeflag: tar.TypeReg, Name: "usr/share/file", Mode: 0o644},
			tar.Header{Typeflag: tar.TypeDir, Name: "usr/share/sub/", Mode: 0o755},
		)
		for name, want := range map[string]time.Time{"usr": usrTime, "usr/share": mtime} {
			info, err := os.Stat(filepath.Join(dest, name))
			Expect(err).ToNot(HaveOccurred())
			Expect(info.ModTime().Equal(want)).To(BeTrue(), "%s mtime is %s", name, info.ModTime())
		}
	})

	It("restores the xattrs of a directory", func() {
		extract(
			tar.Header{
				Typeflag:   tar.TypeDir,
				Name:       "usr/share/",
				Mode:       0o755,
				Format:     tar.FormatPAX,
				PAXRecords: map[string]string{"SCHILY.xattr.user.kairos": "dir"},
			},
		)
		Expect(getxattr("usr/share", "user.kairos")).To(Equal("dir"))
	})

	It("sets the owner of a symlink, not of its target", func() {
		uid, gid := layerUid(), layerGid()
		if os.Geteuid() != 0 && gid == os.Getgid() {
			Skip("not root and in no group besides the primary one, so a symlink owner cannot be told apart from the default")
		}
		extract(
			tar.Header{Typeflag: tar.TypeReg, Name: "usr/lib/real", Mode: 0o644},
			tar.Header{Typeflag: tar.TypeSymlink, Name: "usr/lib/link", Linkname: "real", Uid: uid, Gid: gid},
		)
		link := lstat("usr/lib/link")
		Expect(int(link.Uid)).To(Equal(uid))
		Expect(int(link.Gid)).To(Equal(gid))
		target := lstat("usr/lib/real")
		Expect(int(target.Uid)).To(Equal(os.Geteuid()))
		Expect(int(target.Gid)).To(Equal(os.Getgid()))
	})

	It("extracts an entry whose owner and xattr namespaces a regular user cannot set", func() {
		if os.Geteuid() == 0 {
			Skip("root can set any owner and the trusted and security namespaces")
		}
		extract(
			tar.Header{
				Typeflag: tar.TypeReg,
				Name:     "usr/bin/tool",
				Mode:     0o755,
				Uid:      0,
				Gid:      unownedGid(),
				ModTime:  mtime,
				Format:   tar.FormatPAX,
				PAXRecords: map[string]string{
					"SCHILY.xattr.trusted.kairos":  "5886",
					"SCHILY.xattr.security.kairos": "5886",
				},
			},
		)
		info, err := os.Stat(filepath.Join(dest, "usr/bin/tool"))
		Expect(err).ToNot(HaveOccurred())
		Expect(info.ModTime().Equal(mtime)).To(BeTrue(), "mtime is %s", info.ModTime())
		Expect(logs.String()).To(ContainSubstring("Could not restore the owner or extended attributes"))
	})

	It("extracts an entry carrying an xattr namespace the kernel does not know, as root too", func() {
		extract(
			tar.Header{
				Typeflag:   tar.TypeReg,
				Name:       "usr/bin/tool",
				Mode:       0o755,
				ModTime:    mtime,
				Format:     tar.FormatPAX,
				PAXRecords: map[string]string{"SCHILY.xattr.kairos.unknown": "5886"},
			},
		)
		info, err := os.Stat(filepath.Join(dest, "usr/bin/tool"))
		Expect(err).ToNot(HaveOccurred())
		Expect(info.ModTime().Equal(mtime)).To(BeTrue(), "mtime is %s", info.ModTime())
		Expect(logs.String()).To(ContainSubstring("Could not restore the owner or extended attributes"))
	})
})

// layerUid returns a uid the process can give a file: one that is not its
// own as root, so that a chown is observable, and its own otherwise.
func layerUid() int {
	if os.Geteuid() == 0 {
		return otherID(os.Geteuid())
	}
	return os.Geteuid()
}

// layerGid returns a gid the process can give a file, other than its primary
// one where it can: any gid as root, otherwise a supplementary group. It
// falls back to the primary gid for a user in a single group.
func layerGid() int {
	if os.Geteuid() == 0 {
		return otherID(os.Getgid())
	}
	groups, _ := os.Getgroups()
	for _, g := range groups {
		if g != os.Getgid() {
			return g
		}
	}
	return os.Getgid()
}

// unownedGid returns a gid the process is not a member of.
func unownedGid() int {
	groups, _ := os.Getgroups()
	gid := 0
	for gid == os.Getgid() || slices.Contains(groups, gid) {
		if gid == 0 {
			gid = 4242
		} else {
			gid++
		}
	}
	return gid
}

func otherID(id int) int {
	if id == 4242 {
		return 4243
	}
	return 4242
}
