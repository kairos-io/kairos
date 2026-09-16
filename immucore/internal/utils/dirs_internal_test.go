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

var _ = Describe("StateMigrated", func() {
	var root, stateDir string

	BeforeEach(func() {
		root = GinkgoT().TempDir()
		stateDir = filepath.Join(root, "var-log-audit.bind")
		Expect(os.Mkdir(stateDir, 0o700)).To(Succeed())
	})

	It("reports not migrated while there is no marker", func() {
		migrated, err := StateMigrated(stateDir)

		Expect(err).ToNot(HaveOccurred())
		Expect(migrated).To(BeFalse())
	})

	It("reports not migrated for a state directory holding content but no marker", func() {
		// A sync that died partway: the directory is populated and the
		// migration still has files left to move.
		Expect(os.WriteFile(filepath.Join(stateDir, "audit.log"), []byte("type=DAEMON_START\n"), 0o600)).To(Succeed())

		migrated, err := StateMigrated(stateDir)

		Expect(err).ToNot(HaveOccurred())
		Expect(migrated).To(BeFalse())
	})

	It("reports migrated once the marker is written", func() {
		Expect(MarkStateMigrated(stateDir)).To(Succeed())

		migrated, err := StateMigrated(stateDir)

		Expect(err).ToNot(HaveOccurred())
		Expect(migrated).To(BeTrue())
	})

	It("reports migrated for a state directory that was emptied afterwards", func() {
		// space_left_action cleanup on a full partition: the trail is gone but
		// the migration is not owed again, or the pre-migration snapshot under
		// the /var/log bind would come back.
		Expect(MarkStateMigrated(stateDir)).To(Succeed())

		migrated, err := StateMigrated(stateDir)

		Expect(err).ToNot(HaveOccurred())
		Expect(migrated).To(BeTrue())
		Expect(stateDir).To(BeADirectory())
	})
})

var _ = Describe("MarkStateMigrated", func() {
	var root, stateDir string

	BeforeEach(func() {
		root = GinkgoT().TempDir()
		stateDir = filepath.Join(root, "var-log-audit.bind")
		Expect(os.Mkdir(stateDir, 0o700)).To(Succeed())
	})

	It("writes the marker next to the state directory, not in it", func() {
		Expect(MarkStateMigrated(stateDir)).To(Succeed())

		Expect(stateDir + ".migrated").To(BeAnExistingFile())
		entries, err := os.ReadDir(stateDir)
		Expect(err).ToNot(HaveOccurred())
		Expect(entries).To(BeEmpty())
	})

	It("errors when the marker cannot be written", func() {
		_, err := StateMigrated(filepath.Join(root, "missing", "var-log-audit.bind"))
		Expect(err).ToNot(HaveOccurred())

		err = MarkStateMigrated(filepath.Join(root, "missing", "var-log-audit.bind"))

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("var-log-audit.bind.migrated"))
	})
})
