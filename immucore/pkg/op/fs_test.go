package op_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/kairos-io/kairos/v4/immucore/internal/constants"
	"github.com/kairos-io/kairos/v4/immucore/internal/mount"
	"github.com/kairos-io/kairos/v4/immucore/pkg/op"
	"github.com/kairos-io/kairos/v4/immucore/pkg/schema"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"golang.org/x/sys/unix"
)

var _ = Describe("BaseOverlay", func() {
	var base string

	BeforeEach(func() {
		base = filepath.Join(GinkgoT().TempDir(), "overlay")
	})

	It("mounts a tmpfs backing base 0755, nosuid and nodev", func() {
		operation, err := op.BaseOverlay(schema.Overlay{Base: base, BackingBase: "tmpfs:20%"})
		Expect(err).ToNot(HaveOccurred())

		Expect(operation.Target).To(Equal(base))
		Expect(operation.MountOption.Type).To(Equal("tmpfs"))
		Expect(operation.MountOption.Source).To(Equal("tmpfs"))
		Expect(operation.MountOption.Options).To(ConsistOf("nosuid", "nodev", "mode=0755", "size=20%"))

		flags, data := mount.ParseOptions(operation.MountOption.Options)
		Expect(flags).To(Equal(uintptr(unix.MS_NOSUID | unix.MS_NODEV)))
		Expect(data).To(ConsistOf("mode=0755", "size=20%"))
	})

	It("writes the same options to the fstab entry", func() {
		operation, err := op.BaseOverlay(schema.Overlay{Base: base, BackingBase: "tmpfs:20%"})
		Expect(err).ToNot(HaveOccurred())

		Expect(operation.FstabEntry.Spec).To(Equal("tmpfs"))
		Expect(operation.FstabEntry.VfsType).To(Equal("tmpfs"))
		Expect(operation.FstabEntry.MntOps).To(Equal(map[string]string{
			"nosuid": "",
			"nodev":  "",
			"mode":   "0755",
			"size":   "20%",
		}))
		Expect(strings.Split(operation.FstabEntry.MntOpsString(), ",")).
			To(ConsistOf("nosuid", "nodev", "mode=0755", "size=20%"))
	})

	It("rejects a backing base that is neither a tmpfs nor a device", func() {
		_, err := op.BaseOverlay(schema.Overlay{Base: base, BackingBase: "tmpfs"})
		Expect(err).To(HaveOccurred())
	})
})

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

	It("mounts /home nodev (CIS 1.1.14) and leaves other binds alone", func() {
		home := op.MountBind("/home", root, "/usr/local/.state")
		Expect(home.MountOption.Options).To(ContainElements("bind", "nodev"))
		Expect(home.FstabEntry.MntOps).To(HaveKey("nodev"))

		other := op.MountBind("/var/lib/rancher", root, "/usr/local/.state")
		Expect(other.MountOption.Options).ToNot(ContainElement("nodev"))
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

var _ = Describe("BaseOverlay", func() {
	It("writes the mount option defaults for a device-backed overlay", func() {
		base := filepath.Join(GinkgoT().TempDir(), "run", "overlay")

		operation, err := op.BaseOverlay(schema.Overlay{Base: base, BackingBase: "LABEL=COS_OVERLAY"})
		Expect(err).ToNot(HaveOccurred())

		Expect(operation.FstabEntry.MntOps).To(Equal(map[string]string{"defaults": ""}))
		Expect(operation.FstabEntry.String()).To(ContainSubstring(" defaults 0 0"))
	})
})
