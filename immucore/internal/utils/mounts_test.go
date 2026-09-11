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
	if _, err := syscall.Getxattr(path, name, buf); err != nil {
		return nil, err
	}
	return buf, nil
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
