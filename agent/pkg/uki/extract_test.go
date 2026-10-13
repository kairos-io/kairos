package uki

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// cpioHelperT is the subset of testing.T + ginkgo.GinkgoTInterface that
// buildRealCpio needs. Defined as its own interface so callers can pass
// either a stdlib *testing.T or Ginkgo's GinkgoT(); Ginkgo's variant
// does not satisfy testing.TB because of a sealed private() method.
type cpioHelperT interface {
	Helper()
	Fatalf(format string, args ...any)
	TempDir() string
}

var _ = Describe("initrd cpio extractor", func() {
	It("extracts named regular files and skips symlinks against an upstream-written cpio", func() {
		// Docker + busybox cpio, so the archive under test is produced
		// by an actual newc writer we do not maintain. A round-trip
		// against our own writer proves only that reader and writer
		// agree with each other, not that either agrees with the
		// format: this test closes that gap.
		archive := buildRealCpio(GinkgoT(), map[string]cpioFile{
			"usr/bin/kairos":                           {mode: 0o100755, data: []byte("multi-call-binary-body")},
			"usr/bin/kairos-agent":                     {mode: 0o120777, link: "/usr/bin/kairos"},
			"etc/kairos/capabilities/upgrade-finalize": {mode: 0o100644},
			"etc/passwd":                               {mode: 0o100644, data: []byte("root:x:0:0:root:/root:/bin/bash\n")},
		})

		dir, err := os.MkdirTemp("", "extract-cpio-*")
		Expect(err).NotTo(HaveOccurred())
		defer os.RemoveAll(dir)

		wantAgent := filepath.Join(dir, "kairos")
		wantMarker := filepath.Join(dir, "marker")
		found, err := extractFromCpio(bytes.NewReader(archive), map[string]string{
			"/usr/bin/kairos":                           wantAgent,
			"/usr/bin/kairos-agent":                     filepath.Join(dir, "should-not-appear"),
			"/etc/kairos/capabilities/upgrade-finalize": wantMarker,
		})
		Expect(err).NotTo(HaveOccurred())

		Expect(found).To(ConsistOf("/usr/bin/kairos", "/etc/kairos/capabilities/upgrade-finalize"))

		bin, err := os.ReadFile(wantAgent)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(bin)).To(Equal("multi-call-binary-body"))

		info, err := os.Stat(wantMarker)
		Expect(err).NotTo(HaveOccurred())
		Expect(info.Size()).To(BeZero())

		_, err = os.Stat(filepath.Join(dir, "should-not-appear"))
		Expect(os.IsNotExist(err)).To(BeTrue(), "symlink entries must not be extracted as regular files")
	})

	It("stops at TRAILER!!! and returns cleanly on an archive with no matches", func() {
		// cpio -o always emits TRAILER!!! after the last entry, so any
		// real archive exercises the sentinel path the reader relies on
		// to stop; the test just picks a file the caller does not ask
		// for so the "no match" branch is what gets returned.
		archive := buildRealCpio(GinkgoT(), map[string]cpioFile{
			"some/other/file": {mode: 0o100644, data: []byte("nope")},
		})

		found, err := extractFromCpio(bytes.NewReader(archive), map[string]string{
			"/not/in/the/archive": "/tmp/never-created",
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeEmpty())
	})

	It("normalizes ./-prefixed archive names against /-prefixed caller keys", func() {
		// `find . | cpio -o` names entries "./usr/bin/kairos", so this
		// test exercises the ./-prefix case the reader has to normalize
		// against the leading-slash keys callers pass. No hand-shaped
		// entry needed: it is the shape upstream cpio produces by
		// default.
		archive := buildRealCpio(GinkgoT(), map[string]cpioFile{
			"usr/bin/kairos": {mode: 0o100755, data: []byte("body")},
		})

		dir, err := os.MkdirTemp("", "extract-cpio-*")
		Expect(err).NotTo(HaveOccurred())
		defer os.RemoveAll(dir)

		dst := filepath.Join(dir, "kairos")
		found, err := extractFromCpio(bytes.NewReader(archive), map[string]string{
			"/usr/bin/kairos": dst,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(ConsistOf("/usr/bin/kairos"))
	})
})

// cpioFile describes a single entry for buildRealCpio: a regular file
// carries data + a 0o100xxx mode, a symlink carries a link target + a
// 0o120xxx mode. buildRealCpio materializes each entry on the host
// filesystem so upstream cpio walks them like it would any other files.
type cpioFile struct {
	mode uint32
	data []byte
	link string
}

// buildRealCpio produces a newc-format cpio archive by writing the given
// files into a temp directory and running busybox cpio inside a container
// against it. The archive bytes are captured from the container's stdout.
//
// Docker is required. On its absence the test fails hard (t.Fatalf)
// rather than skipping: a silent skip in CI would let format-compliance
// coverage disappear without anyone noticing.
func buildRealCpio(t cpioHelperT, files map[string]cpioFile) []byte {
	t.Helper()
	docker, err := exec.LookPath("docker")
	if err != nil {
		t.Fatalf("docker not on PATH; buildRealCpio needs docker so extractFromCpio is exercised against an upstream newc writer: %v", err)
	}

	dir := t.TempDir()
	for name, f := range files {
		full := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(full), err)
		}
		if f.mode&0o170000 == 0o120000 {
			if err := os.Symlink(f.link, full); err != nil {
				t.Fatalf("symlink %s -> %s: %v", full, f.link, err)
			}
			continue
		}
		if err := os.WriteFile(full, f.data, os.FileMode(f.mode&0o777)); err != nil {
			t.Fatalf("write %s: %v", full, err)
		}
	}

	// find -mindepth 1 avoids the "." entry cpio would otherwise emit
	// for the tempdir root, which is not something extractFromCpio
	// callers ever ask for. busybox cpio supports -H newc natively.
	cmd := exec.Command(docker, "run", "--rm", "-i",
		"-v", dir+":/src:ro", "-w", "/src",
		"busybox:1.36",
		"sh", "-c", "find . -mindepth 1 | cpio -H newc -o 2>/dev/null",
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("docker cpio: %v (stderr: %s)", err, stderr.String())
	}
	return stdout.Bytes()
}

