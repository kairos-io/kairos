package op_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/kairos-io/kairos/v4/immucore/internal/constants"
	"github.com/kairos-io/kairos/v4/immucore/pkg/op"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("BindStateDir", func() {
	It("mangles the path the way the bind mount does", func() {
		Expect(op.BindStateDir("/var/log/audit", "/sysroot", "/usr/local/.state")).
			To(Equal("/sysroot/usr/local/.state/var-log-audit.bind"))
	})

	It("does not care about the leading slash", func() {
		Expect(op.BindStateDir("var/log/audit", "/sysroot", "/usr/local/.state")).
			To(Equal(op.BindStateDir("/var/log/audit", "/sysroot", "/usr/local/.state")))
	})
})

var _ = Describe("MountBindWithMode", func() {
	var root string

	BeforeEach(func() {
		root = GinkgoT().TempDir()
		if _, err := exec.LookPath("rsync"); err != nil {
			Skip("the bind mount syncs state with rsync, which is not installed here")
		}
	})

	It("mounts the state directory over the path", func() {
		operation := op.MountBindWithMode("/var/log/audit", root, "/usr/local/.state", 0o700)

		Expect(operation.Target).To(Equal(filepath.Join(root, "var/log/audit")))
		Expect(operation.MountOption.Source).To(Equal(op.BindStateDir("/var/log/audit", root, "/usr/local/.state")))
		Expect(operation.MountOption.Options).To(ContainElement("bind"))
	})

	It("pins the mode of both sides of the bind", func() {
		// The mountpoint is what an image with a looser /var/log/audit gives
		// us, and the state directory is the inode the bind actually exposes.
		mountpoint := filepath.Join(root, "var/log/audit")
		Expect(os.MkdirAll(mountpoint, 0o777)).To(Succeed())

		operation := op.MountBindWithMode("/var/log/audit", root, "/usr/local/.state", 0o700)
		Expect(operation.PrepareCallback()).To(Succeed())

		for _, dir := range []string{mountpoint, op.BindStateDir("/var/log/audit", root, "/usr/local/.state")} {
			info, err := os.Stat(dir)
			Expect(err).ToNot(HaveOccurred())
			Expect(info.Mode().Perm()).To(Equal(os.FileMode(0o700)), dir)
		}
	})

	It("carries the contents of the path into the state directory", func() {
		mountpoint := filepath.Join(root, "var/log/audit")
		Expect(os.MkdirAll(mountpoint, 0o700)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(mountpoint, "audit.log"), []byte("type=DAEMON_START\n"), 0o600)).To(Succeed())

		operation := op.MountBindWithMode("/var/log/audit", root, "/usr/local/.state", 0o700)
		Expect(operation.PrepareCallback()).To(Succeed())

		synced := filepath.Join(op.BindStateDir("/var/log/audit", root, "/usr/local/.state"), "audit.log")
		content, err := os.ReadFile(synced)
		Expect(err).ToNot(HaveOccurred())
		Expect(string(content)).To(Equal("type=DAEMON_START\n"))
	})

	It("creates a path that the image does not ship", func() {
		operation := op.MountBindWithMode("/var/log/audit", root, "/usr/local/.state", 0o700)

		Expect(operation.PrepareCallback()).To(Succeed())

		Expect(filepath.Join(root, "var/log/audit")).To(BeADirectory())
		Expect(op.BindStateDir("/var/log/audit", root, "/usr/local/.state")).To(BeADirectory())
	})

	It("does not bring a file back that was removed from the state directory", func() {
		// The second boot of a machine that migrated: the mountpoint the
		// rsync reads from is itself persistent storage (/var/log/audit sits
		// under the /var/log bind), so a snapshot of the trail is still
		// sitting there while the live copy is in the state directory. rsync
		// has no --delete, so repeating the sync would put the rotated away
		// file back on every boot.
		mountpoint := filepath.Join(root, "var/log/audit")
		stateDir := op.BindStateDir("/var/log/audit", root, "/usr/local/.state")
		Expect(os.MkdirAll(mountpoint, 0o700)).To(Succeed())
		Expect(os.MkdirAll(stateDir, 0o700)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(mountpoint, "audit.log.1"), []byte("rotated away\n"), 0o600)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(stateDir, "audit.log"), []byte("live\n"), 0o600)).To(Succeed())

		operation := op.MountBindWithMode("/var/log/audit", root, "/usr/local/.state", 0o700)
		Expect(operation.PrepareCallback()).To(Succeed())

		Expect(filepath.Join(stateDir, "audit.log.1")).ToNot(BeAnExistingFile())
		Expect(filepath.Join(stateDir, "audit.log")).To(BeAnExistingFile())
	})

	It("migrates into a state directory that exists but is empty", func() {
		// A fresh install, or an upgrade to the first image that has this
		// mount: the bind directory can already be there from the mount that
		// failed halfway, and the trail still has to move.
		mountpoint := filepath.Join(root, "var/log/audit")
		stateDir := op.BindStateDir("/var/log/audit", root, "/usr/local/.state")
		Expect(os.MkdirAll(mountpoint, 0o700)).To(Succeed())
		Expect(os.MkdirAll(stateDir, 0o700)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(mountpoint, "audit.log"), []byte("type=DAEMON_START\n"), 0o600)).To(Succeed())

		operation := op.MountBindWithMode("/var/log/audit", root, "/usr/local/.state", 0o700)
		Expect(operation.PrepareCallback()).To(Succeed())

		Expect(filepath.Join(stateDir, "audit.log")).To(BeAnExistingFile())
	})

	It("still pins the mode when the sync is skipped", func() {
		stateDir := op.BindStateDir("/var/log/audit", root, "/usr/local/.state")
		Expect(os.MkdirAll(stateDir, 0o777)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(stateDir, "audit.log"), []byte("live\n"), 0o600)).To(Succeed())
		Expect(os.Chmod(stateDir, 0o777)).To(Succeed())

		operation := op.MountBindWithMode("/var/log/audit", root, "/usr/local/.state", 0o700)
		Expect(operation.PrepareCallback()).To(Succeed())

		info, err := os.Stat(stateDir)
		Expect(err).ToNot(HaveOccurred())
		Expect(info.Mode().Perm()).To(Equal(os.FileMode(0o700)))
	})
})

var _ = Describe("MountWithBaseOverlay", func() {
	var root, base string

	BeforeEach(func() {
		root = GinkgoT().TempDir()
		base = GinkgoT().TempDir()
	})

	It("creates the lowerdir when it is missing from the image", func() {
		operation := op.MountWithBaseOverlay("mnt", root, base)

		Expect(operation.PrepareCallback()).ToNot(HaveOccurred())
		Expect(filepath.Join(root, "mnt")).To(BeADirectory())
	})

	It("reports ErrMountTargetMissing when the lowerdir cannot be created", func() {
		if os.Geteuid() == 0 {
			Skip("root bypasses directory write permissions")
		}
		// Stands in for the real case: the path is absent from the OS image and the
		// rootfs is still mounted read-only, so MkdirAll cannot create it either.
		readOnly := filepath.Join(root, "readonly")
		Expect(os.Mkdir(readOnly, 0500)).ToNot(HaveOccurred())

		operation := op.MountWithBaseOverlay("mnt", readOnly, base)

		err := operation.PrepareCallback()
		Expect(err).To(HaveOccurred())
		Expect(errors.Is(err, constants.ErrMountTargetMissing)).To(BeTrue())
		Expect(err.Error()).To(ContainSubstring(filepath.Join(readOnly, "mnt")))
	})
})
