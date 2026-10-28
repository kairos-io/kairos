package diffupgrade_test

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"

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
		Expect(os.Chmod(filepath.Join(target, "usr/bin/unchanged"), 0o755)).To(Succeed())
	})

	It("lists only the topmost deleted paths", func() {
		m, err := diffupgrade.Generate(base, target, &discard{})
		Expect(err).ToNot(HaveOccurred())
		Expect(m.Deleted).To(Equal([]string{"usr/lib/gone", "usr/share/olddir"}))
		expected, err := diffupgrade.Fingerprint(osFS{}, base)
		Expect(err).ToNot(HaveOccurred())
		Expect(m.Base).To(Equal(expected))
	})

	// unpackOntoBase generates the diff image, unpacks it onto a copy of the
	// base the way a full image is unpacked, and returns the copy along with
	// the fingerprint it had before.
	unpackOntoBase := func() (string, string) {
		out := filepath.Join(GinkgoT().TempDir(), "diff.tar")
		_, err := diffupgrade.GenerateImage(base, target, out, "example.org/kairos/test:v1.1.0.diff")
		Expect(err).ToNot(HaveOccurred())

		img, err := tarball.ImageFromPath(out, nil)
		Expect(err).ToNot(HaveOccurred())
		cfg, err := img.ConfigFile()
		Expect(err).ToNot(HaveOccurred())
		Expect(cfg.Config.Labels).To(HaveKey(diffupgrade.BaseLabel))

		work := filepath.Join(GinkgoT().TempDir(), "root")
		Expect(exec.Command("cp", "-a", base, work).Run()).To(Succeed())
		fingerprint, err := diffupgrade.Fingerprint(osFS{}, work)
		Expect(err).ToNot(HaveOccurred())

		Expect(image.ExtractOCIImage(img, work)).To(Succeed())
		return work, fingerprint
	}

	It("turns a copy of the base into the target", func() {
		work, fingerprint := unpackOntoBase()
		Expect(diffupgrade.Apply(osFS{}, work, fingerprint)).To(Succeed())
		Expect(tree(work)).To(Equal(tree(target)))
	})

	It("refuses a diff computed against another release", func() {
		work, _ := unpackOntoBase()
		err := diffupgrade.Apply(osFS{}, work, "sha256:somethingelse")
		Expect(err).To(MatchError(diffupgrade.ErrBaseMismatch))
		// Nothing was deleted on a refusal.
		Expect(filepath.Join(work, "usr/lib/gone")).To(BeAnExistingFile())
	})

	It("refuses a manifest that reaches outside the root", func() {
		root := GinkgoT().TempDir()
		writeTree(root, map[string]string{
			diffupgrade.ManifestPath: `{"version":1,"base":"b","deleted":["../escape"]}`,
		})
		Expect(diffupgrade.Apply(osFS{}, root, "b")).To(MatchError(ContainSubstring("outside the root")))
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

type discard struct{}

func (*discard) Write(p []byte) (int, error) { return len(p), nil }
