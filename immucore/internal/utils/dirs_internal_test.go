package utils

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"golang.org/x/sys/unix"
)

var _ = Describe("EnforceRootOwnedDir", func() {
	var root string

	BeforeEach(func() {
		root = GinkgoT().TempDir()
	})

	It("tightens a directory that came with a looser mode", func() {
		// CreateIfNotExists creates with os.ModePerm, and a directory from an
		// older image keeps whatever mode it had, so the mode has to be set.
		dir := filepath.Join(root, "audit")
		Expect(os.Mkdir(dir, 0777)).To(Succeed())

		Expect(EnforceRootOwnedDir(dir, 0o700)).To(Succeed())

		info, err := os.Stat(dir)
		Expect(err).ToNot(HaveOccurred())
		Expect(info.Mode().Perm()).To(Equal(os.FileMode(0o700)))
	})

	It("errors on a directory that is not there", func() {
		Expect(EnforceRootOwnedDir(filepath.Join(root, "missing"), 0o700)).To(HaveOccurred())
	})
})

var _ = Describe("CopySELinuxLabel", func() {
	var root, src, dst string

	BeforeEach(func() {
		root = GinkgoT().TempDir()
		src = filepath.Join(root, "src")
		dst = filepath.Join(root, "dst")
		Expect(os.Mkdir(src, 0700)).To(Succeed())
		Expect(os.Mkdir(dst, 0700)).To(Succeed())
	})

	It("does nothing when the source carries no label", func() {
		Expect(CopySELinuxLabel(src, dst)).To(Succeed())

		label, err := ReadSELinuxLabel(dst)
		Expect(err).ToNot(HaveOccurred())
		Expect(label).To(BeEmpty())
	})

	It("copies the label of the source onto the destination", func() {
		want := "system_u:object_r:auditd_log_t:s0"
		if err := unix.Setxattr(src, seLinuxXattr, []byte(want), 0); err != nil {
			Skip("this filesystem does not take SELinux labels from an unprivileged process")
		}

		Expect(CopySELinuxLabel(src, dst)).To(Succeed())

		label, err := ReadSELinuxLabel(dst)
		Expect(err).ToNot(HaveOccurred())
		Expect(label).To(Equal(want))
	})
})

var _ = Describe("DirHasContent", func() {
	var root string

	BeforeEach(func() {
		root = GinkgoT().TempDir()
	})

	It("reports no content for a directory that is not there", func() {
		has, err := DirHasContent(filepath.Join(root, "missing"))

		Expect(err).ToNot(HaveOccurred())
		Expect(has).To(BeFalse())
	})

	It("reports no content for an empty directory", func() {
		has, err := DirHasContent(root)

		Expect(err).ToNot(HaveOccurred())
		Expect(has).To(BeFalse())
	})

	It("reports content for a directory with an entry in it", func() {
		Expect(os.WriteFile(filepath.Join(root, "audit.log"), []byte("type=DAEMON_START\n"), 0o600)).To(Succeed())

		has, err := DirHasContent(root)

		Expect(err).ToNot(HaveOccurred())
		Expect(has).To(BeTrue())
	})

	It("reports content for a directory that only holds a subdirectory", func() {
		Expect(os.Mkdir(filepath.Join(root, "old"), 0o700)).To(Succeed())

		has, err := DirHasContent(root)

		Expect(err).ToNot(HaveOccurred())
		Expect(has).To(BeTrue())
	})

	It("errors on a path that is not a directory", func() {
		file := filepath.Join(root, "audit.log")
		Expect(os.WriteFile(file, []byte("type=DAEMON_START\n"), 0o600)).To(Succeed())

		_, err := DirHasContent(file)

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(file))
	})
})
