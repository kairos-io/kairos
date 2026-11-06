package diffupgrade_test

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/kairos/v4/sdk/diffupgrade"
	"github.com/kairos-io/kairos/v4/sdk/utils/image"
)

// osFS satisfies what Apply needs from the host filesystem.
type osFS struct{}

func (osFS) ReadFile(name string) ([]byte, error) { return os.ReadFile(name) }
func (osFS) RemoveAll(path string) error          { return os.RemoveAll(path) }
func (osFS) Remove(path string) error             { return os.Remove(path) }
func (osFS) Stat(name string) (os.FileInfo, error) {
	return os.Stat(name)
}
func (osFS) WriteFile(name string, data []byte, perm os.FileMode) error {
	return os.WriteFile(name, data, perm)
}

func writeTree(root string, files map[string]string) {
	for p, content := range files {
		full := filepath.Join(root, p)
		Expect(os.MkdirAll(filepath.Dir(full), 0o755)).To(Succeed())
		Expect(os.WriteFile(full, []byte(content), 0o644)).To(Succeed())
	}
}

// tree records what a root filesystem holds, in the terms a diff has to keep.
func tree(root string) map[string]string {
	out := map[string]string{}
	Expect(filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		Expect(err).ToNot(HaveOccurred())
		rel, _ := filepath.Rel(root, path)
		info, err := d.Info()
		Expect(err).ToNot(HaveOccurred())
		desc := info.Mode().String()
		switch {
		case info.Mode()&fs.ModeSymlink != 0:
			link, err := os.Readlink(path)
			Expect(err).ToNot(HaveOccurred())
			desc += " -> " + link
		case info.Mode().IsRegular():
			data, err := os.ReadFile(path)
			Expect(err).ToNot(HaveOccurred())
			desc += " " + string(data)
		}
		out[rel] = desc
		return nil
	})).To(Succeed())
	return out
}

var _ = Describe("diff upgrades", func() {
	var base, target string

	BeforeEach(func() {
		base = GinkgoT().TempDir()
		target = GinkgoT().TempDir()

		writeTree(base, map[string]string{
			"etc/kairos-release":      "KAIROS_VERSION=v1.0.0\n",
			"usr/bin/unchanged":       "same",
			"usr/bin/changed":         "old",
			"usr/lib/gone":            "bye",
			"usr/lib/kept":            "kept",
			"usr/share/olddir/a":      "a",
			"usr/share/olddir/sub/b":  "b",
			"opt/becomes-file/inside": "x",
		})
		Expect(os.Symlink("changed", filepath.Join(base, "usr/bin/link"))).To(Succeed())

		writeTree(target, map[string]string{
			"etc/kairos-release": "KAIROS_VERSION=v1.1.0\n",
			"usr/bin/unchanged":  "same",
			"usr/bin/changed":    "new contents",
			"usr/bin/added":      "hello",
			"usr/lib/kept":       "kept",
			"usr/share/newdir/c": "c",
			"opt/becomes-file":   "now a file",
		})
		Expect(os.Symlink("added", filepath.Join(target, "usr/bin/link"))).To(Succeed())
		Expect(os.Link(filepath.Join(target, "usr/bin/added"), filepath.Join(target, "usr/bin/added-too"))).To(Succeed())
		Expect(os.Chmod(filepath.Join(target, "usr/bin/unchanged"), 0o755)).To(Succeed())
	})

	It("lists only the topmost deleted paths", func() {
		m, err := diffupgrade.Generate(diffupgrade.Root{Dir: base, Ref: "a:v1"}, diffupgrade.Root{Dir: target, Ref: "a:v2"}, &discard{})
		Expect(err).ToNot(HaveOccurred())
		Expect(m.Deleted).To(Equal([]string{"usr/lib/gone", "usr/share/olddir"}))
		expected, err := diffupgrade.RootfsID(base)
		Expect(err).ToNot(HaveOccurred())
		Expect(m.Base).To(Equal(expected))
		expected, err = diffupgrade.RootfsID(target)
		Expect(err).ToNot(HaveOccurred())
		Expect(m.Target).To(Equal(expected))
		expected, err = diffupgrade.Fingerprint(osFS{}, target)
		Expect(err).ToNot(HaveOccurred())
		Expect(m.TargetRelease).To(Equal(expected))
		Expect(m.BaseRef).To(Equal("a:v1"))
		Expect(m.TargetRef).To(Equal("a:v2"))
	})

	// unpackOntoBase generates the diff image, unpacks it onto a copy of the
	// base the way a full image is unpacked, and returns the copy along with
	// the fingerprint it had before.
	unpackOntoBase := func() (string, string) {
		img, _, release, err := diffupgrade.NewImage(diffupgrade.Root{Dir: base}, diffupgrade.Root{Dir: target}, GinkgoT().TempDir())
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(release)
		cfg, err := img.ConfigFile()
		Expect(err).ToNot(HaveOccurred())
		Expect(cfg.Config.Labels).To(HaveKey(diffupgrade.BaseLabel))
		Expect(cfg.Config.Labels).To(HaveKey(diffupgrade.TargetLabel))

		work := filepath.Join(GinkgoT().TempDir(), "root")
		Expect(exec.Command("cp", "-a", base, work).Run()).To(Succeed())
		fingerprint, err := diffupgrade.RootfsID(work)
		Expect(err).ToNot(HaveOccurred())

		Expect(image.ExtractOCIImage(img, work)).To(Succeed())
		return work, fingerprint
	}

	It("turns a copy of the base into the target", func() {
		work, fingerprint := unpackOntoBase()
		m, err := diffupgrade.Apply(osFS{}, work, fingerprint)
		Expect(err).ToNot(HaveOccurred())
		Expect(tree(work)).To(Equal(tree(target)))
		a, err := os.Stat(filepath.Join(work, "usr/bin/added"))
		Expect(err).ToNot(HaveOccurred())
		b, err := os.Stat(filepath.Join(work, "usr/bin/added-too"))
		Expect(err).ToNot(HaveOccurred())
		Expect(os.SameFile(a, b)).To(BeTrue(), "hard links stay hard links")
		Expect(diffupgrade.RootfsID(work)).To(Equal(m.Target), "the result is the target release")
	})

	It("refuses a diff computed against another release", func() {
		work, _ := unpackOntoBase()
		_, err := diffupgrade.Apply(osFS{}, work, "sha256:somethingelse")
		Expect(err).To(MatchError(diffupgrade.ErrBaseMismatch))
		// Nothing was deleted on a refusal.
		Expect(filepath.Join(work, "usr/lib/gone")).To(BeAnExistingFile())
	})

	It("refuses a result that is not the target release", func() {
		work, fingerprint := unpackOntoBase()
		// Something else rewrote the release file after the diff landed.
		Expect(os.WriteFile(filepath.Join(work, "etc/kairos-release"), []byte("tampered"), 0o644)).To(Succeed())
		_, err := diffupgrade.Apply(osFS{}, work, fingerprint)
		Expect(err).To(MatchError(ContainSubstring("expected")))
	})

	It("tells an image archive from a root filesystem tarball", func() {
		dir := GinkgoT().TempDir()
		img, _, release, err := diffupgrade.NewImage(diffupgrade.Root{Dir: base}, diffupgrade.Root{Dir: target}, dir)
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(release)
		imageArchive := filepath.Join(dir, "image.tar")
		Expect(tarball.WriteToFile(imageArchive, name.MustParseReference("example.org/a:b").(name.Tag), img)).To(Succeed())

		rootfs := filepath.Join(dir, "rootfs.tar.gz")
		Expect(exec.Command("tar", "-czf", rootfs, "-C", target, ".").Run()).To(Succeed())

		Expect(diffupgrade.IsImageArchive(imageArchive)).To(BeTrue())
		Expect(diffupgrade.IsImageArchive(rootfs)).To(BeFalse())

		unpacked := filepath.Join(dir, "unpacked")
		Expect(os.Mkdir(unpacked, 0o755)).To(Succeed())
		Expect(diffupgrade.UnpackTarball(rootfs, unpacked)).To(Succeed())
		// The root directory takes its mode from the tarball, not from
		// where it is unpacked, so only what is under it is compared.
		got, want := tree(unpacked), tree(target)
		delete(got, ".")
		delete(want, ".")
		Expect(got).To(Equal(want))
	})

	It("refuses a manifest that reaches outside the root", func() {
		root := GinkgoT().TempDir()
		writeTree(root, map[string]string{
			diffupgrade.ManifestPath: `{"version":1,"base":"b","deleted":["../escape"]}`,
		})
		_, err := diffupgrade.Apply(osFS{}, root, "b")
		Expect(err).To(MatchError(ContainSubstring("outside the root")))
	})

	Describe("RootfsID", func() {
		copyOf := func(dir string) string {
			dst := filepath.Join(GinkgoT().TempDir(), "copy")
			Expect(exec.Command("cp", "-a", dir, dst).Run()).To(Succeed())
			return dst
		}

		It("is the same for a copy, whatever the times and empty directories", func() {
			dup := copyOf(base)
			later := time.Now().Add(time.Hour)
			Expect(os.Chtimes(filepath.Join(dup, "usr/bin/changed"), later, later)).To(Succeed())
			Expect(os.MkdirAll(filepath.Join(dup, "lost+found"), 0o700)).To(Succeed())
			Expect(os.MkdirAll(filepath.Join(dup, "proc"), 0o555)).To(Succeed())
			Expect(diffupgrade.RootfsID(dup)).To(Equal(must(diffupgrade.RootfsID(base))))
		})

		It("differs when a file the kairos-release fingerprint does not see changes", func() {
			dup := copyOf(base)
			Expect(os.WriteFile(filepath.Join(dup, "usr/bin/unchanged"), []byte("rebuilt"), 0o644)).To(Succeed())
			Expect(diffupgrade.Fingerprint(osFS{}, dup)).To(Equal(must(diffupgrade.Fingerprint(osFS{}, base))))
			Expect(diffupgrade.RootfsID(dup)).ToNot(Equal(must(diffupgrade.RootfsID(base))))
		})

		It("differs on permissions, link targets and added files", func() {
			id := must(diffupgrade.RootfsID(base))

			dup := copyOf(base)
			Expect(os.Chmod(filepath.Join(dup, "usr/bin/changed"), 0o755)).To(Succeed())
			Expect(diffupgrade.RootfsID(dup)).ToNot(Equal(id))

			dup = copyOf(base)
			Expect(os.Remove(filepath.Join(dup, "usr/bin/link"))).To(Succeed())
			Expect(os.Symlink("unchanged", filepath.Join(dup, "usr/bin/link"))).To(Succeed())
			Expect(diffupgrade.RootfsID(dup)).ToNot(Equal(id))

			dup = copyOf(base)
			Expect(os.WriteFile(filepath.Join(dup, "extra"), nil, 0o644)).To(Succeed())
			Expect(diffupgrade.RootfsID(dup)).ToNot(Equal(id))
		})
	})

	Describe("recorded IDs", func() {
		var img string

		BeforeEach(func() {
			img = filepath.Join(GinkgoT().TempDir(), "active.img")
			Expect(os.WriteFile(img, []byte("image"), 0o600)).To(Succeed())
		})

		It("returns what was recorded while the image is unchanged", func() {
			Expect(diffupgrade.RecordID(osFS{}, img, "sha256:abc")).To(Succeed())
			Expect(diffupgrade.RecordedID(osFS{}, img)).To(Equal("sha256:abc"))
			// A rename keeps it, as the ID travels with the image.
			moved := filepath.Join(filepath.Dir(img), "passive.img")
			Expect(os.Rename(img, moved)).To(Succeed())
			Expect(os.Rename(img+diffupgrade.SidecarSuffix, moved+diffupgrade.SidecarSuffix)).To(Succeed())
			Expect(diffupgrade.RecordedID(osFS{}, moved)).To(Equal("sha256:abc"))
		})

		It("refuses an ID recorded before the image was rewritten", func() {
			Expect(diffupgrade.RecordID(osFS{}, img, "sha256:abc")).To(Succeed())
			later := time.Now().Add(time.Minute)
			Expect(os.WriteFile(img, []byte("other"), 0o600)).To(Succeed())
			Expect(os.Chtimes(img, later, later)).To(Succeed())
			_, err := diffupgrade.RecordedID(osFS{}, img)
			Expect(err).To(MatchError(diffupgrade.ErrNoRecordedID))
		})

		It("has none when nothing was recorded or it was forgotten", func() {
			_, err := diffupgrade.RecordedID(osFS{}, img)
			Expect(err).To(MatchError(diffupgrade.ErrNoRecordedID))
			Expect(diffupgrade.RecordID(osFS{}, img, "sha256:abc")).To(Succeed())
			Expect(diffupgrade.ForgetID(osFS{}, img)).To(Succeed())
			Expect(diffupgrade.ForgetID(osFS{}, img)).To(Succeed(), "forgetting twice is fine")
			_, err = diffupgrade.RecordedID(osFS{}, img)
			Expect(err).To(MatchError(diffupgrade.ErrNoRecordedID))
		})
	})

	DescribeTable("names the diff of a full image",
		func(ref, expected string, fails bool) {
			got, err := diffupgrade.RefFor(ref)
			if fails {
				Expect(err).To(HaveOccurred())
				return
			}
			Expect(err).ToNot(HaveOccurred())
			Expect(got).To(Equal(expected))
		},
		Entry("tag", "quay.io/kairos/hadron:v1.2.0", "quay.io/kairos/hadron:v1.2.0.diff", false),
		Entry("no tag", "quay.io/kairos/hadron", "quay.io/kairos/hadron:latest.diff", false),
		Entry("digest", "quay.io/kairos/hadron@sha256:"+sixtyFourZeros, "", true),
	)
})

const sixtyFourZeros = "0000000000000000000000000000000000000000000000000000000000000000"

func must(s string, err error) string {
	ExpectWithOffset(1, err).ToNot(HaveOccurred())
	return s
}

type discard struct{}

func (*discard) Write(p []byte) (int, error) { return len(p), nil }
