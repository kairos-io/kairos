package utils

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("CleanStaleUnitSymlinks", func() {
	var root string

	// unitDir is /etc/systemd/system, the directory that persists across an
	// image change because /etc/systemd is a persistent state bind.
	unitDir := func() string { return filepath.Join(root, "etc", "systemd", "system") }
	packagedDir := func() string { return filepath.Join(root, "usr", "lib", "systemd", "system") }

	BeforeEach(func() {
		root = GinkgoT().TempDir()
		Expect(os.MkdirAll(unitDir(), 0o755)).To(Succeed())
		Expect(os.MkdirAll(packagedDir(), 0o755)).To(Succeed())
	})

	It("removes a dangling unit symlink that shadows a packaged unit", func() {
		// kairos-io/kairos#4085: enabling Ubuntu's ssh.service leaves the
		// Alias=sshd.service symlink in persistent /etc, and on Hadron it
		// dangles and shadows the real sshd.service.
		Expect(os.WriteFile(filepath.Join(packagedDir(), "sshd.service"), []byte("[Unit]\n"), 0o644)).To(Succeed())
		stale := filepath.Join(unitDir(), "sshd.service")
		Expect(os.Symlink("/usr/lib/systemd/system/ssh.service", stale)).To(Succeed())

		removed, err := CleanStaleUnitSymlinks(root)

		Expect(err).ToNot(HaveOccurred())
		Expect(removed).To(ConsistOf("sshd.service"))
		Expect(stale).ToNot(BeAnExistingFile())
	})

	It("keeps a unit symlink whose target exists", func() {
		Expect(os.WriteFile(filepath.Join(packagedDir(), "sshd.service"), []byte("[Unit]\n"), 0o644)).To(Succeed())
		link := filepath.Join(unitDir(), "ssh.service")
		Expect(os.Symlink("/usr/lib/systemd/system/sshd.service", link)).To(Succeed())

		removed, err := CleanStaleUnitSymlinks(root)

		Expect(err).ToNot(HaveOccurred())
		Expect(removed).To(BeEmpty())
		_, lerr := os.Lstat(link)
		Expect(lerr).ToNot(HaveOccurred())
	})

	It("keeps a mask symlink even though /dev/null is absent from the sysroot", func() {
		Expect(os.WriteFile(filepath.Join(packagedDir(), "wicked.service"), []byte("[Unit]\n"), 0o644)).To(Succeed())
		mask := filepath.Join(unitDir(), "wicked.service")
		Expect(os.Symlink("/dev/null", mask)).To(Succeed())

		removed, err := CleanStaleUnitSymlinks(root)

		Expect(err).ToNot(HaveOccurred())
		Expect(removed).To(BeEmpty())
		_, lerr := os.Lstat(mask)
		Expect(lerr).ToNot(HaveOccurred())
	})

	It("keeps a dangling symlink when no packaged unit of that name exists", func() {
		// A unit shipped only by a sysext is not merged yet while immucore
		// runs, so removing its symlink would disable it for good.
		link := filepath.Join(unitDir(), "sysext-only.service")
		Expect(os.Symlink("/usr/lib/systemd/system/sysext-only.service", link)).To(Succeed())

		removed, err := CleanStaleUnitSymlinks(root)

		Expect(err).ToNot(HaveOccurred())
		Expect(removed).To(BeEmpty())
		_, lerr := os.Lstat(link)
		Expect(lerr).ToNot(HaveOccurred())
	})

	It("leaves enablement symlinks inside .wants directories alone", func() {
		// A dangling .wants entry shadows nothing, systemd just warns about it.
		wants := filepath.Join(unitDir(), "multi-user.target.wants")
		Expect(os.MkdirAll(wants, 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(packagedDir(), "ssh.service"), []byte("[Unit]\n"), 0o644)).To(Succeed())
		link := filepath.Join(wants, "ssh.service")
		Expect(os.Symlink("/usr/lib/systemd/system/nope.service", link)).To(Succeed())

		removed, err := CleanStaleUnitSymlinks(root)

		Expect(err).ToNot(HaveOccurred())
		Expect(removed).To(BeEmpty())
		_, lerr := os.Lstat(link)
		Expect(lerr).ToNot(HaveOccurred())
	})

	It("resolves a relative symlink target against the unit directory", func() {
		Expect(os.WriteFile(filepath.Join(unitDir(), "there.service"), []byte("[Unit]\n"), 0o644)).To(Succeed())
		Expect(os.Symlink("there.service", filepath.Join(unitDir(), "here.service"))).To(Succeed())
		Expect(os.WriteFile(filepath.Join(packagedDir(), "gone.service"), []byte("[Unit]\n"), 0o644)).To(Succeed())
		Expect(os.Symlink("missing.service", filepath.Join(unitDir(), "gone.service"))).To(Succeed())

		removed, err := CleanStaleUnitSymlinks(root)

		Expect(err).ToNot(HaveOccurred())
		Expect(removed).To(ConsistOf("gone.service"))
		Expect(filepath.Join(unitDir(), "here.service")).To(BeAnExistingFile())
	})

	It("finds the packaged unit under /lib on a non-merged-usr image", func() {
		libDir := filepath.Join(root, "lib", "systemd", "system")
		Expect(os.MkdirAll(libDir, 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(libDir, "sshd.service"), []byte("[Unit]\n"), 0o644)).To(Succeed())
		Expect(os.Symlink("/lib/systemd/system/ssh.service", filepath.Join(unitDir(), "sshd.service"))).To(Succeed())

		removed, err := CleanStaleUnitSymlinks(root)

		Expect(err).ToNot(HaveOccurred())
		Expect(removed).To(ConsistOf("sshd.service"))
	})

	It("does nothing when the unit directory is absent", func() {
		Expect(os.RemoveAll(unitDir())).To(Succeed())

		removed, err := CleanStaleUnitSymlinks(root)

		Expect(err).ToNot(HaveOccurred())
		Expect(removed).To(BeEmpty())
	})
})
