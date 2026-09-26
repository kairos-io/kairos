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

var _ = Describe("MountBind", func() {
	var root string

	BeforeEach(func() {
		root = GinkgoT().TempDir()
		if _, err := exec.LookPath("rsync"); err != nil {
			Skip("the bind mount syncs state with rsync, which is not installed here")
		}
	})

	It("mounts the state directory over the path", func() {
		operation := op.MountBind("/var/log/audit", root, "/usr/local/.state")

		Expect(operation.Target).To(Equal(filepath.Join(root, "var/log/audit")))
		Expect(operation.MountOption.Source).To(Equal(op.BindStateDir("/var/log/audit", root, "/usr/local/.state")))
		Expect(operation.MountOption.Options).To(ContainElement("bind"))
	})

	It("gives the state directory the mode of the path it backs", func() {
		// The bind exposes the inode of the state directory, so a path the
		// image keeps at 0700 (/var/log/audit) has to find the same mode there
		// or the mount is what loosened it.
		mountpoint := filepath.Join(root, "var/log/audit")
		Expect(os.MkdirAll(mountpoint, 0o700)).To(Succeed())
		Expect(os.Chmod(mountpoint, 0o700)).To(Succeed())

		operation := op.MountBind("/var/log/audit", root, "/usr/local/.state")
		Expect(operation.PrepareCallback()).To(Succeed())

		info, err := os.Stat(op.BindStateDir("/var/log/audit", root, "/usr/local/.state"))
		Expect(err).ToNot(HaveOccurred())
		Expect(info.Mode().Perm()).To(Equal(os.FileMode(0o700)))
	})

	It("leaves the mode of a state directory that is already there alone", func() {
		// Every boot after the first one. The mode of the state directory is
		// the mode of the mountpoint by then, and the data in it is live.
		mountpoint := filepath.Join(root, "var/log/audit")
		stateDir := op.BindStateDir("/var/log/audit", root, "/usr/local/.state")
		Expect(os.MkdirAll(mountpoint, 0o700)).To(Succeed())
		Expect(os.MkdirAll(stateDir, 0o700)).To(Succeed())
		Expect(os.Chmod(stateDir, 0o700)).To(Succeed())

		operation := op.MountBind("/var/log/audit", root, "/usr/local/.state")
		Expect(operation.PrepareCallback()).To(Succeed())

		info, err := os.Stat(stateDir)
		Expect(err).ToNot(HaveOccurred())
		Expect(info.Mode().Perm()).To(Equal(os.FileMode(0o700)))
	})

	It("carries the contents of the path into the state directory", func() {
		mountpoint := filepath.Join(root, "var/log/audit")
		Expect(os.MkdirAll(mountpoint, 0o700)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(mountpoint, "audit.log"), []byte("type=DAEMON_START\n"), 0o600)).To(Succeed())

		operation := op.MountBind("/var/log/audit", root, "/usr/local/.state")
		Expect(operation.PrepareCallback()).To(Succeed())

		synced := filepath.Join(op.BindStateDir("/var/log/audit", root, "/usr/local/.state"), "audit.log")
		content, err := os.ReadFile(synced)
		Expect(err).ToNot(HaveOccurred())
		Expect(string(content)).To(Equal("type=DAEMON_START\n"))
	})

	It("creates a path that the image does not ship", func() {
		operation := op.MountBind("/var/log/audit", root, "/usr/local/.state")

		Expect(operation.PrepareCallback()).To(Succeed())

		Expect(filepath.Join(root, "var/log/audit")).To(BeADirectory())
		Expect(op.BindStateDir("/var/log/audit", root, "/usr/local/.state")).To(BeADirectory())
	})

	It("gives the state directory of a path the image does not ship the mode the path asks for", func() {
		// The case on every fresh install: nothing has created the mountpoint,
		// so there is no mode to read off it and the directory that ends up
		// backing the bind would take whatever default created it first. What
		// the path declares is the only thing that knows the mode it needs.
		mode, ok := constants.BindMountMode("/var/log/audit")
		Expect(ok).To(BeTrue())
		Expect(mode).To(Equal(os.FileMode(0o700)))

		Expect(filepath.Join(root, "var/log/audit")).ToNot(BeADirectory())

		operation := op.MountBind("/var/log/audit", root, "/usr/local/.state")
		Expect(operation.PrepareCallback()).To(Succeed())

		// The mountpoint, which is what the state directory copies from.
		info, err := os.Stat(filepath.Join(root, "var/log/audit"))
		Expect(err).ToNot(HaveOccurred())
		Expect(info.Mode().Perm()).To(Equal(os.FileMode(0o700)))

		// The state directory, whose inode is the one the bind exposes.
		info, err = os.Stat(op.BindStateDir("/var/log/audit", root, "/usr/local/.state"))
		Expect(err).ToNot(HaveOccurred())
		Expect(info.Mode().Perm()).To(Equal(os.FileMode(0o700)))
	})

	It("keeps the mode the image ships over the one the path asks for", func() {
		// An image that grew its own /var/log/audit is the authority on the
		// mode of it, the declared one is only there for the images that do
		// not ship the path at all.
		mountpoint := filepath.Join(root, "var/log/audit")
		Expect(os.MkdirAll(mountpoint, 0o750)).To(Succeed())
		Expect(os.Chmod(mountpoint, 0o750)).To(Succeed())

		operation := op.MountBind("/var/log/audit", root, "/usr/local/.state")
		Expect(operation.PrepareCallback()).To(Succeed())

		info, err := os.Stat(op.BindStateDir("/var/log/audit", root, "/usr/local/.state"))
		Expect(err).ToNot(HaveOccurred())
		Expect(info.Mode().Perm()).To(Equal(os.FileMode(0o750)))
	})

	It("does not tighten a path that asks for no mode of its own", func() {
		// Every other entry of the bind list. Nothing declares a mode for it,
		// so the directories keep the mode they have always been created with.
		_, ok := constants.BindMountMode("/var/lib/rancher")
		Expect(ok).To(BeFalse())

		// What a directory created with no mode in mind looks like here, so
		// that the umask of whoever runs the suite does not decide the result.
		reference := filepath.Join(root, "reference")
		Expect(os.MkdirAll(reference, os.ModePerm)).To(Succeed())
		referenceInfo, err := os.Stat(reference)
		Expect(err).ToNot(HaveOccurred())

		operation := op.MountBind("/var/lib/rancher", root, "/usr/local/.state")
		Expect(operation.PrepareCallback()).To(Succeed())

		info, err := os.Stat(op.BindStateDir("/var/lib/rancher", root, "/usr/local/.state"))
		Expect(err).ToNot(HaveOccurred())
		Expect(info.Mode().Perm()).To(Equal(referenceInfo.Mode().Perm()))
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

var _ = Describe("MountBind onto a symlink", func() {
	var root string

	BeforeEach(func() {
		root = GinkgoT().TempDir()
		Expect(os.MkdirAll(filepath.Join(root, "etc/ssl"), 0o755)).To(Succeed())
	})

	// No rsync is needed in here: the refusal happens before the state is
	// synced, which is the point of refusing.

	It("refuses an absolute symlink, the shape the Red Hat and SUSE images ship", func() {
		// rockylinux 9 and fedora 40 ship /etc/ssl/certs as a symlink into
		// /etc/pki, opensuse leap into /var/lib/ca-certificates. The target is
		// absolute, so it resolves against the initramfs root immucore runs in
		// and not against the sysroot.
		Expect(os.Symlink("/etc/pki/tls/certs", filepath.Join(root, "etc/ssl/certs"))).To(Succeed())

		err := op.MountBind("/etc/ssl/certs", root, "/usr/local/.state").PrepareCallback()

		Expect(errors.Is(err, constants.ErrMountTargetIsSymlink)).To(BeTrue(), "got %v", err)
		Expect(err.Error()).To(ContainSubstring(filepath.Join(root, "etc/ssl/certs")))
		Expect(err.Error()).To(ContainSubstring("/etc/pki/tls/certs"))

		// The mountpoint is left as it was found, rather than replaced by a
		// directory, and nothing is set up to back a bind that will not happen.
		info, lerr := os.Lstat(filepath.Join(root, "etc/ssl/certs"))
		Expect(lerr).ToNot(HaveOccurred())
		Expect(info.Mode() & os.ModeSymlink).ToNot(BeZero())
		Expect(op.BindStateDir("/etc/ssl/certs", root, "/usr/local/.state")).ToNot(BeADirectory())
	})

	It("refuses a relative symlink instead of persisting the image content it resolves to", func() {
		// The damaging one. Without the guard os.Stat follows the link, so
		// every check passes, the image certificate directory is copied into
		// the persistent state and mount(2) then binds the copy over it. rsync
		// runs with no --delete, so a CA the image later distrusts would
		// outlive every upgrade.
		Expect(os.MkdirAll(filepath.Join(root, "etc/pki/tls/certs"), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(root, "etc/pki/tls/certs/ca-bundle.crt"), []byte("image CA\n"), 0o644)).To(Succeed())
		Expect(os.Symlink("../pki/tls/certs", filepath.Join(root, "etc/ssl/certs"))).To(Succeed())

		operation := op.MountBind("/etc/ssl/certs", root, "/usr/local/.state")
		err := operation.PrepareCallback()

		Expect(errors.Is(err, constants.ErrMountTargetIsSymlink)).To(BeTrue(), "got %v", err)
		Expect(op.BindStateDir("/etc/ssl/certs", root, "/usr/local/.state")).ToNot(BeADirectory())

		// Where the bind would have landed, which is not the path that was asked for.
		resolved, rerr := filepath.EvalSymlinks(operation.Target)
		Expect(rerr).ToNot(HaveOccurred())
		Expect(resolved).To(Equal(filepath.Join(root, "etc/pki/tls/certs")))
		Expect(resolved).ToNot(Equal(operation.Target))
	})

	It("does not refuse a mountpoint that is a real directory", func() {
		// The guard has to be about symlinks and nothing else. This asserts
		// only that, so that it does not need the rsync the sync step calls.
		Expect(os.MkdirAll(filepath.Join(root, "etc/ssl/certs"), 0o755)).To(Succeed())

		err := op.MountBind("/etc/ssl/certs", root, "/usr/local/.state").PrepareCallback()

		Expect(errors.Is(err, constants.ErrMountTargetIsSymlink)).To(BeFalse(), "got %v", err)
	})

	It("does not refuse a mountpoint the image does not ship at all", func() {
		err := op.MountBind("/var/lib/rancher", root, "/usr/local/.state").PrepareCallback()

		Expect(errors.Is(err, constants.ErrMountTargetIsSymlink)).To(BeFalse(), "got %v", err)
	})
})
