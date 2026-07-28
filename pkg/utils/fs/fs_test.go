package fsutils_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	fsutils "github.com/kairos-io/kairos-agent/v2/pkg/utils/fs"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/twpayne/go-vfs/v5"
	"github.com/twpayne/go-vfs/v5/vfst"
)

var _ = Describe("FsUtils", func() {
	var tfs *vfst.TestFS
	var cleanup func()
	var err error

	BeforeEach(func() {
		tfs, cleanup, err = vfst.NewTestFS(map[string]interface{}{})
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(cleanup)
	})

	Describe("Exists", func() {
		It("returns true for an existing file", func() {
			Expect(tfs.WriteFile("/file.txt", []byte("hello"), 0644)).To(Succeed())
			exists, err := fsutils.Exists(tfs, "/file.txt")
			Expect(err).ToNot(HaveOccurred())
			Expect(exists).To(BeTrue())
		})
		It("returns false for a missing file", func() {
			exists, err := fsutils.Exists(tfs, "/nope.txt")
			Expect(err).ToNot(HaveOccurred())
			Expect(exists).To(BeFalse())
		})
		It("returns the error for a stat failure that is not NotExist", func() {
			Expect(tfs.WriteFile("/file.txt", []byte("hello"), 0644)).To(Succeed())
			// Stating a path that traverses a regular file yields ENOTDIR.
			exists, err := fsutils.Exists(tfs, "/file.txt/sub")
			Expect(err).To(HaveOccurred())
			Expect(exists).To(BeFalse())
		})
	})

	Describe("IsDir", func() {
		It("returns true for a directory", func() {
			Expect(fsutils.MkdirAll(tfs, "/somedir", 0755)).To(Succeed())
			isDir, err := fsutils.IsDir(tfs, "/somedir")
			Expect(err).ToNot(HaveOccurred())
			Expect(isDir).To(BeTrue())
		})
		It("returns false for a file", func() {
			Expect(tfs.WriteFile("/file.txt", []byte("hi"), 0644)).To(Succeed())
			isDir, err := fsutils.IsDir(tfs, "/file.txt")
			Expect(err).ToNot(HaveOccurred())
			Expect(isDir).To(BeFalse())
		})
		It("returns an error for a missing path", func() {
			_, err := fsutils.IsDir(tfs, "/missing")
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("MkdirAll", func() {
		It("creates nested directories", func() {
			Expect(fsutils.MkdirAll(tfs, "/a/b/c", 0755)).To(Succeed())
			exists, err := fsutils.Exists(tfs, "/a/b/c")
			Expect(err).ToNot(HaveOccurred())
			Expect(exists).To(BeTrue())
			isDir, err := fsutils.IsDir(tfs, "/a/b/c")
			Expect(err).ToNot(HaveOccurred())
			Expect(isDir).To(BeTrue())
		})
		It("returns a permission error on a read-only fs", func() {
			rofs := vfs.NewReadOnlyFS(tfs)
			err := fsutils.MkdirAll(rofs, "/denied", 0755)
			Expect(err).To(HaveOccurred())
			Expect(err).To(MatchError(os.ErrPermission))
		})
		It("returns a path error when RawPath fails", func() {
			// PathFS refuses relative paths in RawPath, exercising the
			// RawPath error branch.
			pfs := vfs.NewPathFS(tfs, "/")
			err := fsutils.MkdirAll(pfs, "relative/path", 0755)
			Expect(err).To(HaveOccurred())
			var pathErr *os.PathError
			Expect(errors.As(err, &pathErr)).To(BeTrue())
			Expect(pathErr.Op).To(Equal("mkdir"))
		})
	})

	Describe("DirSize", func() {
		It("sums the size of all files in the tree", func() {
			Expect(fsutils.MkdirAll(tfs, "/data/sub", 0755)).To(Succeed())
			Expect(tfs.WriteFile("/data/a.txt", []byte("12345"), 0644)).To(Succeed())          // 5 bytes
			Expect(tfs.WriteFile("/data/sub/b.txt", []byte("1234567890"), 0644)).To(Succeed()) // 10 bytes
			size, err := fsutils.DirSize(tfs, "/data")
			Expect(err).ToNot(HaveOccurred())
			Expect(size).To(Equal(int64(15)))
		})
		It("returns an error for a missing path", func() {
			_, err := fsutils.DirSize(tfs, "/missing")
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("TempDir", func() {
		It("creates a predictable temp dir on the test fs", func() {
			name, err := fsutils.TempDir(tfs, "/tmp", "myprefix")
			Expect(err).ToNot(HaveOccurred())
			Expect(name).To(ContainSubstring("myprefix"))
			exists, err := fsutils.Exists(tfs, name)
			Expect(err).ToNot(HaveOccurred())
			Expect(exists).To(BeTrue())
			isDir, err := fsutils.IsDir(tfs, name)
			Expect(err).ToNot(HaveOccurred())
			Expect(isDir).To(BeTrue())
		})
		It("defaults to the system temp dir when dir is empty", func() {
			name, err := fsutils.TempDir(tfs, "", "emptydir")
			Expect(err).ToNot(HaveOccurred())
			Expect(name).To(Equal(filepath.Join(os.TempDir(), "emptydir")))
			exists, err := fsutils.Exists(tfs, name)
			Expect(err).ToNot(HaveOccurred())
			Expect(exists).To(BeTrue())
		})
		It("creates a random temp dir on a non-test fs", func() {
			// Use a PathFS over the OS fs rooted at a real temp dir so the
			// non-TestFS code path runs without touching the host outside it.
			realDir, err := os.MkdirTemp("", "kairos-fsutils-test")
			Expect(err).ToNot(HaveOccurred())
			DeferCleanup(func() { _ = os.RemoveAll(realDir) })

			pfs := vfs.NewPathFS(vfs.OSFS, realDir)
			name, err := fsutils.TempDir(pfs, "/sub", "rand")
			Expect(err).ToNot(HaveOccurred())
			Expect(filepath.Base(name)).To(HavePrefix("rand"))
			// The random suffix is appended on non-test filesystems.
			Expect(filepath.Base(name)).ToNot(Equal("rand"))
			info, err := os.Stat(filepath.Join(realDir, name))
			Expect(err).ToNot(HaveOccurred())
			Expect(info.IsDir()).To(BeTrue())
		})
		It("returns an error when the dir cannot be created", func() {
			rofs := vfs.NewReadOnlyFS(tfs)
			name, err := fsutils.TempDir(rofs, "/tmp", "denied")
			Expect(err).To(HaveOccurred())
			Expect(name).To(BeEmpty())
		})
	})

	Describe("TempFile", func() {
		// Note: on the vfst test fs, the returned *os.File is backed by a real
		// host file, so f.Name() is the host raw path (not the vfs-relative
		// path). We therefore assert on the basename pattern and verify the
		// backing file exists on the host fs via os.Stat.
		It("creates a temp file matching the pattern", func() {
			Expect(fsutils.MkdirAll(tfs, "/tmp", 0755)).To(Succeed())
			f, err := fsutils.TempFile(tfs, "/tmp", "pre-*.log")
			Expect(err).ToNot(HaveOccurred())
			Expect(f).ToNot(BeNil())
			name := f.Name()
			Expect(f.Close()).To(Succeed())
			Expect(filepath.Base(name)).To(HavePrefix("pre-"))
			Expect(name).To(HaveSuffix(".log"))
			info, err := os.Stat(name)
			Expect(err).ToNot(HaveOccurred())
			Expect(info.IsDir()).To(BeFalse())
		})
		It("creates a temp file when the pattern has no wildcard", func() {
			Expect(fsutils.MkdirAll(tfs, "/tmp", 0755)).To(Succeed())
			f, err := fsutils.TempFile(tfs, "/tmp", "noglob")
			Expect(err).ToNot(HaveOccurred())
			Expect(f).ToNot(BeNil())
			name := f.Name()
			Expect(f.Close()).To(Succeed())
			Expect(filepath.Base(name)).To(HavePrefix("noglob"))
		})
		It("defaults to the system temp dir when dir is empty", func() {
			Expect(fsutils.MkdirAll(tfs, os.TempDir(), 0755)).To(Succeed())
			f, err := fsutils.TempFile(tfs, "", "empty-*.txt")
			Expect(err).ToNot(HaveOccurred())
			Expect(f).ToNot(BeNil())
			Expect(f.Close()).To(Succeed())
		})
		It("returns an error when the dir does not exist", func() {
			f, err := fsutils.TempFile(tfs, "/does/not/exist", "fail-*.txt")
			Expect(err).To(HaveOccurred())
			Expect(f).To(BeNil())
		})
	})

	Describe("Copy", func() {
		It("copies the content of src to dst", func() {
			content := []byte("the quick brown fox")
			Expect(tfs.WriteFile("/src.txt", content, 0644)).To(Succeed())
			Expect(fsutils.Copy(tfs, "/src.txt", "/dst.txt")).To(Succeed())
			got, err := tfs.ReadFile("/dst.txt")
			Expect(err).ToNot(HaveOccurred())
			Expect(got).To(Equal(content))
		})
		It("returns ErrInvalid when src equals dst", func() {
			err := fsutils.Copy(tfs, "/same.txt", "/same.txt")
			Expect(err).To(MatchError(os.ErrInvalid))
		})
		It("errors when src does not exist", func() {
			err := fsutils.Copy(tfs, "/missing.txt", "/dst.txt")
			Expect(err).To(HaveOccurred())
		})
		It("errors when dst cannot be created", func() {
			Expect(tfs.WriteFile("/src.txt", []byte("data"), 0644)).To(Succeed())
			err := fsutils.Copy(tfs, "/src.txt", "/missing-dir/dst.txt")
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("GlobFs", func() {
		It("returns only the matching files in the directory", func() {
			Expect(fsutils.MkdirAll(tfs, "/glob/sub", 0755)).To(Succeed())
			Expect(tfs.WriteFile("/glob/a.txt", []byte("a"), 0644)).To(Succeed())
			Expect(tfs.WriteFile("/glob/b.txt", []byte("b"), 0644)).To(Succeed())
			Expect(tfs.WriteFile("/glob/c.log", []byte("c"), 0644)).To(Succeed())
			Expect(tfs.WriteFile("/glob/sub/d.txt", []byte("d"), 0644)).To(Succeed())

			matches, err := fsutils.GlobFs(tfs, "/glob/*.txt")
			Expect(err).ToNot(HaveOccurred())
			sort.Strings(matches)
			Expect(matches).To(Equal([]string{"/glob/a.txt", "/glob/b.txt"}))
		})
		It("errors on a malformed pattern", func() {
			matches, err := fsutils.GlobFs(tfs, "[")
			Expect(err).To(HaveOccurred())
			Expect(matches).To(BeNil())
		})
		It("errors when the directory cannot be read", func() {
			matches, err := fsutils.GlobFs(tfs, "/missing-dir/*.txt")
			Expect(err).To(HaveOccurred())
			Expect(matches).To(BeNil())
		})
		It("defaults to the current directory when the pattern has no dir", func() {
			// No dir part in the pattern, so it reads ".". The test fs only
			// accepts absolute paths, so this surfaces as a ReadDir error,
			// which still exercises the default-dir branch.
			matches, err := fsutils.GlobFs(tfs, "*.doesnotexist")
			Expect(err).To(HaveOccurred())
			Expect(matches).To(BeNil())
		})
	})

	Describe("WalkDirFs", func() {
		It("visits every entry in the tree", func() {
			Expect(fsutils.MkdirAll(tfs, "/walk/sub", 0755)).To(Succeed())
			Expect(tfs.WriteFile("/walk/a.txt", []byte("a"), 0644)).To(Succeed())
			Expect(tfs.WriteFile("/walk/sub/b.txt", []byte("b"), 0644)).To(Succeed())

			var visited []string
			err := fsutils.WalkDirFs(tfs, "/walk", func(path string, _ fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				visited = append(visited, path)
				return nil
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(visited).To(ContainElements("/walk", "/walk/a.txt", "/walk/sub", "/walk/sub/b.txt"))
		})
		It("honors SkipDir to prune subtrees", func() {
			Expect(fsutils.MkdirAll(tfs, "/walk2/skip", 0755)).To(Succeed())
			Expect(tfs.WriteFile("/walk2/keep.txt", []byte("k"), 0644)).To(Succeed())
			Expect(tfs.WriteFile("/walk2/skip/hidden.txt", []byte("h"), 0644)).To(Succeed())

			var visited []string
			err := fsutils.WalkDirFs(tfs, "/walk2", func(path string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if d.IsDir() && strings.HasSuffix(path, "/skip") {
					return filepath.SkipDir
				}
				visited = append(visited, path)
				return nil
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(visited).To(ContainElement("/walk2/keep.txt"))
			Expect(visited).ToNot(ContainElement("/walk2/skip/hidden.txt"))
		})
		It("exposes dir entry name, type and info for the root entry", func() {
			Expect(fsutils.MkdirAll(tfs, "/walkroot", 0755)).To(Succeed())
			err := fsutils.WalkDirFs(tfs, "/walkroot", func(path string, d fs.DirEntry, err error) error {
				Expect(err).ToNot(HaveOccurred())
				Expect(d.Name()).To(Equal("walkroot"))
				Expect(d.IsDir()).To(BeTrue())
				Expect(d.Type().IsDir()).To(BeTrue())
				info, infoErr := d.Info()
				Expect(infoErr).ToNot(HaveOccurred())
				Expect(info.IsDir()).To(BeTrue())
				return nil
			})
			Expect(err).ToNot(HaveOccurred())
		})
		It("reports the stat error on a missing root", func() {
			var gotErr error
			err := fsutils.WalkDirFs(tfs, "/missing-root", func(path string, d fs.DirEntry, err error) error {
				gotErr = err
				return err
			})
			Expect(err).To(HaveOccurred())
			Expect(gotErr).To(HaveOccurred())
		})
		It("swallows the stat error when the callback returns nil", func() {
			err := fsutils.WalkDirFs(tfs, "/missing-root", func(path string, d fs.DirEntry, err error) error {
				return nil
			})
			Expect(err).ToNot(HaveOccurred())
		})
		It("returns nil when the callback skips a non-dir root", func() {
			Expect(tfs.WriteFile("/rootfile.txt", []byte("x"), 0644)).To(Succeed())
			err := fsutils.WalkDirFs(tfs, "/rootfile.txt", func(path string, d fs.DirEntry, err error) error {
				return filepath.SkipDir
			})
			Expect(err).ToNot(HaveOccurred())
		})
		It("propagates a callback error on a subdirectory", func() {
			Expect(fsutils.MkdirAll(tfs, "/walk4/sub", 0755)).To(Succeed())
			bogus := errors.New("bogus")
			err := fsutils.WalkDirFs(tfs, "/walk4", func(path string, d fs.DirEntry, err error) error {
				if path == "/walk4/sub" {
					return bogus
				}
				return nil
			})
			Expect(err).To(MatchError(bogus))
		})
		It("stops walking the directory when a file callback returns SkipDir", func() {
			Expect(fsutils.MkdirAll(tfs, "/walk5", 0755)).To(Succeed())
			Expect(tfs.WriteFile("/walk5/a.txt", []byte("a"), 0644)).To(Succeed())
			Expect(tfs.WriteFile("/walk5/b.txt", []byte("b"), 0644)).To(Succeed())

			var visited []string
			err := fsutils.WalkDirFs(tfs, "/walk5", func(path string, d fs.DirEntry, err error) error {
				visited = append(visited, path)
				if path == "/walk5/a.txt" {
					return filepath.SkipDir
				}
				return nil
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(visited).To(ContainElement("/walk5/a.txt"))
			Expect(visited).ToNot(ContainElement("/walk5/b.txt"))
		})
		It("reports a ReadDir failure to the callback", func() {
			if os.Geteuid() == 0 {
				Skip("running as root, permissions are not enforced")
			}
			Expect(fsutils.MkdirAll(tfs, "/walk6/noperm", 0755)).To(Succeed())
			Expect(tfs.Chmod("/walk6/noperm", 0o000)).To(Succeed())
			DeferCleanup(func() { _ = tfs.Chmod("/walk6/noperm", 0o755) })

			var gotErr error
			err := fsutils.WalkDirFs(tfs, "/walk6", func(path string, d fs.DirEntry, err error) error {
				if err != nil {
					gotErr = err
				}
				return err
			})
			Expect(err).To(HaveOccurred())
			Expect(gotErr).To(HaveOccurred())
		})
		It("continues when the callback ignores a ReadDir failure", func() {
			if os.Geteuid() == 0 {
				Skip("running as root, permissions are not enforced")
			}
			Expect(fsutils.MkdirAll(tfs, "/walk7/noperm", 0755)).To(Succeed())
			Expect(tfs.WriteFile("/walk7/z.txt", []byte("z"), 0644)).To(Succeed())
			Expect(tfs.Chmod("/walk7/noperm", 0o000)).To(Succeed())
			DeferCleanup(func() { _ = tfs.Chmod("/walk7/noperm", 0o755) })

			var visited []string
			err := fsutils.WalkDirFs(tfs, "/walk7", func(path string, d fs.DirEntry, err error) error {
				visited = append(visited, path)
				return nil
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(visited).To(ContainElement("/walk7/z.txt"))
		})
	})
})
