package uki

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/klauspost/compress/zstd"
	"github.com/u-root/u-root/pkg/cpio"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// readInitrd decompresses the zstd cpio "initrd" produced by createInitramfs
// and returns its records keyed by name, so tests can assert on the layout.
func readInitrd(initrdPath string) (map[string]cpio.Record, error) {
	f, err := os.Open(initrdPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	zr, err := zstd.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer zr.Close()

	raw, err := io.ReadAll(zr)
	if err != nil {
		return nil, err
	}

	archiver, err := cpio.Format("newc")
	if err != nil {
		return nil, err
	}
	records, err := cpio.ReadAllRecords(archiver.Reader(bytes.NewReader(raw)))
	if err != nil {
		return nil, err
	}

	byName := make(map[string]cpio.Record, len(records))
	for _, rec := range records {
		byName[rec.Name] = rec
	}
	return byName, nil
}

// recordContent reads the bytes of a regular-file record.
func recordContent(rec cpio.Record) ([]byte, error) {
	if rec.FileSize == 0 {
		return []byte{}, nil
	}
	buf := make([]byte, int(rec.FileSize))
	_, err := rec.ReadAt(buf, 0)
	return buf, err
}

var _ = Describe("createInitramfs", func() {
	var sourceDir, artifactsDir string
	var err error

	BeforeEach(func() {
		sourceDir, err = os.MkdirTemp("", "createInitramfs-src-")
		Expect(err).ToNot(HaveOccurred())
		artifactsDir, err = os.MkdirTemp("", "createInitramfs-art-")
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		os.RemoveAll(sourceDir)
		os.RemoveAll(artifactsDir)
	})

	It("records every entry under a path relative to sourceDir, rooted at \".\"", func() {
		Expect(os.MkdirAll(filepath.Join(sourceDir, "etc"), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(sourceDir, "etc", "os-release"), []byte("ID=kairos\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(sourceDir, "init"), []byte("#!/bin/sh\n"), 0o755)).To(Succeed())

		Expect(createInitramfs(sourceDir, artifactsDir)).To(Succeed())

		records, err := readInitrd(filepath.Join(artifactsDir, "initrd"))
		Expect(err).ToNot(HaveOccurred())

		// Names are relative to sourceDir (no leading slash, no temp-dir prefix)
		// and the rootfs root is recorded as ".".
		Expect(records).To(HaveKey("."))
		Expect(records).To(HaveKey("etc"))
		Expect(records).To(HaveKey("etc/os-release"))
		Expect(records).To(HaveKey("init"))
		for name := range records {
			Expect(name).ToNot(HavePrefix("/"), "record %q must not be absolute", name)
			Expect(name).ToNot(ContainSubstring(sourceDir), "record %q leaked the temp sourceDir prefix", name)
		}
	})

	It("round-trips regular file contents", func() {
		want := []byte("ID=kairos\nVERSION=v3\n")
		Expect(os.MkdirAll(filepath.Join(sourceDir, "etc"), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(sourceDir, "etc", "os-release"), want, 0o644)).To(Succeed())

		Expect(createInitramfs(sourceDir, artifactsDir)).To(Succeed())

		records, err := readInitrd(filepath.Join(artifactsDir, "initrd"))
		Expect(err).ToNot(HaveOccurred())

		rec, ok := records["etc/os-release"]
		Expect(ok).To(BeTrue())
		got, err := recordContent(rec)
		Expect(err).ToNot(HaveOccurred())
		Expect(got).To(Equal(want))
	})

	It("excludes the top-level virtual filesystem directories", func() {
		for _, dir := range []string{"sys", "run", "dev", "tmp", "proc"} {
			Expect(os.MkdirAll(filepath.Join(sourceDir, dir), 0o755)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(sourceDir, dir, "marker"), []byte("x"), 0o644)).To(Succeed())
		}
		// A real file we expect to keep.
		Expect(os.WriteFile(filepath.Join(sourceDir, "keep"), []byte("x"), 0o644)).To(Succeed())

		Expect(createInitramfs(sourceDir, artifactsDir)).To(Succeed())

		records, err := readInitrd(filepath.Join(artifactsDir, "initrd"))
		Expect(err).ToNot(HaveOccurred())

		Expect(records).To(HaveKey("keep"))
		for _, dir := range []string{"sys", "run", "dev", "tmp", "proc"} {
			Expect(records).ToNot(HaveKey(dir))
			Expect(records).ToNot(HaveKey(dir + "/marker"))
		}
	})

	It("only excludes those names at the top level, not nested ones", func() {
		// "etc/sys" must survive: the exclude list matches the full relative
		// path, not any path component.
		Expect(os.MkdirAll(filepath.Join(sourceDir, "etc", "sys"), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(sourceDir, "etc", "sys", "keep"), []byte("x"), 0o644)).To(Succeed())

		Expect(createInitramfs(sourceDir, artifactsDir)).To(Succeed())

		records, err := readInitrd(filepath.Join(artifactsDir, "initrd"))
		Expect(err).ToNot(HaveOccurred())

		Expect(records).To(HaveKey("etc/sys"))
		Expect(records).To(HaveKey("etc/sys/keep"))
	})

	// Regression guard for the removed os.Chdir: createInitramfs must capture
	// the sourceDir it is given, regardless of the process working directory
	// and regardless of other builds running concurrently. The old
	// chdir-into-sourceDir + Walk(".") approach raced on the process-global cwd
	// and could pack the wrong rootfs under concurrency.
	It("is independent of the process cwd and safe to run concurrently", func() {
		const n = 8
		srcDirs := make([]string, n)
		artDirs := make([]string, n)
		for i := 0; i < n; i++ {
			srcDirs[i], err = os.MkdirTemp("", fmt.Sprintf("createInitramfs-conc-src-%d-", i))
			Expect(err).ToNot(HaveOccurred())
			artDirs[i], err = os.MkdirTemp("", fmt.Sprintf("createInitramfs-conc-art-%d-", i))
			Expect(err).ToNot(HaveOccurred())
			// Each rootfs gets a uniquely named marker file.
			Expect(os.WriteFile(filepath.Join(srcDirs[i], fmt.Sprintf("marker-%d", i)), []byte("x"), 0o644)).To(Succeed())
		}
		defer func() {
			for i := 0; i < n; i++ {
				os.RemoveAll(srcDirs[i])
				os.RemoveAll(artDirs[i])
			}
		}()

		var wg sync.WaitGroup
		errs := make([]error, n)
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				errs[i] = createInitramfs(srcDirs[i], artDirs[i])
			}(i)
		}
		wg.Wait()

		for i := 0; i < n; i++ {
			Expect(errs[i]).ToNot(HaveOccurred())
			records, err := readInitrd(filepath.Join(artDirs[i], "initrd"))
			Expect(err).ToNot(HaveOccurred())
			// Each initrd must contain exactly its own marker and none of the
			// others — proving no cross-contamination via a shared cwd.
			Expect(records).To(HaveKey(fmt.Sprintf("marker-%d", i)))
			for j := 0; j < n; j++ {
				if j == i {
					continue
				}
				Expect(records).ToNot(HaveKey(fmt.Sprintf("marker-%d", j)))
			}
		}
	})
})

var _ = Describe("ZstdFile", func() {
	var workDir, src string

	BeforeEach(func() {
		var err error
		workDir, err = os.MkdirTemp("", "ZstdFile-")
		Expect(err).ToNot(HaveOccurred())
		src = filepath.Join(workDir, "input")
		Expect(os.WriteFile(src, []byte("kairos initramfs payload\n"), 0o644)).To(Succeed())
	})

	AfterEach(func() {
		os.RemoveAll(workDir)
	})

	It("round-trips the source contents", func() {
		dst := filepath.Join(workDir, "output.zst")
		Expect(ZstdFile(src, dst)).To(Succeed())

		f, err := os.Open(dst)
		Expect(err).ToNot(HaveOccurred())
		defer f.Close()
		zr, err := zstd.NewReader(f)
		Expect(err).ToNot(HaveOccurred())
		defer zr.Close()
		got, err := io.ReadAll(zr)
		Expect(err).ToNot(HaveOccurred())
		Expect(got).To(Equal([]byte("kairos initramfs payload\n")))
	})

	It("returns an error when the compressed output cannot be written", func() {
		if _, err := os.Stat("/dev/full"); err != nil {
			Skip("/dev/full is not available")
		}
		Expect(ZstdFile(src, "/dev/full")).ToNot(Succeed())
	})
})
