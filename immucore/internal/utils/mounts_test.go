package utils_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"github.com/kairos-io/kairos/v4/immucore/internal/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// getxattr is a small Getxattr wrapper that tells "absent" apart from "empty".
func getxattr(path, name string) ([]byte, error) {
	size, err := syscall.Getxattr(path, name, nil)
	if err != nil {
		return nil, err
	}
	buf := make([]byte, size)
	size, err = syscall.Getxattr(path, name, buf)
	if err != nil {
		return nil, err
	}
	return buf[:size], nil
}

var _ = Describe("SyncState", func() {
	var src, dst string

	// The label immucore has to keep. Tests run unprivileged, and only root may
	// write the security namespace, so the spec uses a user attribute. rsync
	// applies the same rule to every namespace it handles: the destination's set
	// is made to match the source's, which deletes what the source lacks.
	const attr = "user.immucore-selinux-test"
	const label = "system_u:object_r:container_var_lib_t:s0"

	BeforeEach(func() {
		if _, err := exec.LookPath("rsync"); err != nil {
			Skip("rsync is not installed")
		}

		dir, err := os.MkdirTemp("", "immucore-syncstate")
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() { _ = os.RemoveAll(dir) })

		src = filepath.Join(dir, "var-lib-rook")
		dst = filepath.Join(dir, "var-lib-rook.bind")
		Expect(os.MkdirAll(src, 0755)).To(Succeed())
		Expect(os.MkdirAll(dst, 0755)).To(Succeed())

		// TMPDIR can land on a filesystem that stores no extended attributes,
		// tmpfs on a kernel older than 6.6 among them.
		if err := syscall.Setxattr(dst, attr, []byte(label), 0); err != nil {
			Skip("filesystem does not support extended attributes: " + err.Error())
		}
	})

	// The bug: MountBind creates the source itself when the OS image does not
	// ship the path, so it carries no label, and the sync then takes the label
	// off the state dir on the second and every later boot.
	It("keeps an attribute the source does not have", func() {
		Expect(utils.SyncState(utils.AppendSlash(src), utils.AppendSlash(dst))).To(Succeed())

		got, err := getxattr(dst, attr)
		Expect(err).ToNot(HaveOccurred(), "the sync removed %s from the state dir", attr)
		Expect(string(got)).To(Equal(label))
	})

	It("lets the source's own value win", func() {
		Expect(syscall.Setxattr(src, attr, []byte("from_the_image"), 0)).To(Succeed())

		Expect(utils.SyncState(utils.AppendSlash(src), utils.AppendSlash(dst))).To(Succeed())

		got, err := getxattr(dst, attr)
		Expect(err).ToNot(HaveOccurred())
		Expect(string(got)).To(Equal("from_the_image"))
	})

	It("still copies content", func() {
		Expect(os.WriteFile(filepath.Join(src, "conf"), []byte("hi"), 0644)).To(Succeed())

		Expect(utils.SyncState(utils.AppendSlash(src), utils.AppendSlash(dst))).To(Succeed())

		Expect(os.ReadFile(filepath.Join(dst, "conf"))).To(Equal([]byte("hi")))
	})
})

// Cost. The snapshot reads the destination root and nothing else, so what it
// costs does not depend on how much data the bind mount holds. That matters for
// the state dirs that carry terabytes across millions of files: rsync's own -A
// and -X already walk every one of them, and have since before this change, but
// the restore added here stays at one listxattr plus one getxattr per attribute
// on a single directory.
var _ = Describe("SyncState xattr snapshot cost", func() {
	var src, dst string

	const attr = "user.immucore-selinux-test"
	const label = "system_u:object_r:container_var_lib_t:s0"

	BeforeEach(func() {
		if _, err := exec.LookPath("rsync"); err != nil {
			Skip("rsync is not installed")
		}

		dir, err := os.MkdirTemp("", "immucore-syncstate-cost")
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() { _ = os.RemoveAll(dir) })

		src = filepath.Join(dir, "var-lib-rook")
		dst = filepath.Join(dir, "var-lib-rook.bind")
		Expect(os.MkdirAll(src, 0755)).To(Succeed())
		Expect(os.MkdirAll(dst, 0755)).To(Succeed())
	})

	// A host with no SELinux has nothing on the directory to save, and the
	// restore then has nothing to look up either. Nothing is read below the root
	// in the first place, so this is the whole cost on such a system.
	It("adds no attribute when the destination has none", func() {
		if err := syscall.Setxattr(dst, attr, []byte(label), 0); err != nil {
			Skip("filesystem does not support extended attributes: " + err.Error())
		}
		Expect(syscall.Removexattr(dst, attr)).To(Succeed())

		Expect(utils.SyncState(utils.AppendSlash(src), utils.AppendSlash(dst))).To(Succeed())

		size, err := syscall.Listxattr(dst, nil)
		Expect(err).ToNot(HaveOccurred())
		Expect(size).To(Equal(0), "the restore invented an attribute on an unlabelled directory")
	})

	// The reason the cost is flat: the snapshot is not a walk. Give the tree a
	// file whose own attribute the source does not carry and it is rsync that
	// decides its fate, exactly as before this change. If the snapshot were
	// recursive this file would come back labelled.
	It("does not save or restore anything below the destination root", func() {
		if err := syscall.Setxattr(dst, attr, []byte(label), 0); err != nil {
			Skip("filesystem does not support extended attributes: " + err.Error())
		}
		child := filepath.Join(dst, "conf")
		Expect(os.WriteFile(child, []byte("hi"), 0644)).To(Succeed())
		Expect(syscall.Setxattr(child, attr, []byte(label), 0)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(src, "conf"), []byte("hi"), 0644)).To(Succeed())

		Expect(utils.SyncState(utils.AppendSlash(src), utils.AppendSlash(dst))).To(Succeed())

		By("the root keeps its label, which is the fix")
		got, err := getxattr(dst, attr)
		Expect(err).ToNot(HaveOccurred())
		Expect(string(got)).To(Equal(label))

		By("the child is left to rsync, so the snapshot cannot be recursive")
		_, err = getxattr(child, attr)
		Expect(err).To(HaveOccurred(), "the snapshot walked into the tree; its cost is no longer flat")
	})
})
