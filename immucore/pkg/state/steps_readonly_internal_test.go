package state

import (
	"context"
	"errors"
	"path/filepath"

	cnst "github.com/kairos-io/kairos/v4/immucore/internal/constants"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spectrocloud-labs/herd"
)

var _ = Describe("isPersistentVolume", func() {
	// The predicate keys off the mountpoint because that is the only half of a
	// VOLUMES entry that survives every way of declaring the partition. The
	// first attempt at read-only support matched the device by string, which
	// silently half-applied the layout for anyone declaring it by UUID.

	It("recognises the default mountpoint", func() {
		s := &State{StateDir: cnst.PersistentStateTarget}
		Expect(s.isPersistentVolume("", "/usr/local")).To(BeTrue())
	})

	It("does not care how the device was declared", func() {
		s := &State{StateDir: cnst.PersistentStateTarget}

		// All of these are the same VOLUMES entry, written differently.
		// ParseMount rewrites the label and UUID forms into device paths, and
		// the UUID form keeps no trace of the label at all.
		for _, device := range []string{
			"/dev/disk/by-label/COS_PERSISTENT",
			"/dev/disk/by-uuid/0a1b2c3d-4e5f-6071-8293-a4b5c6d7e8f9",
			"/dev/sda5",
			"/dev/mapper/COS_PERSISTENT",
		} {
			s.CustomMounts = map[string]string{device: "/usr/local"}
			what, where, ok := s.persistentVolume()
			Expect(ok).To(BeTrue(), "did not recognise the persistent volume declared as %s", device)
			Expect(what).To(Equal(device))
			Expect(where).To(Equal("/usr/local"))
		}
	})

	It("follows a moved PERSISTENT_STATE_TARGET", func() {
		s := &State{StateDir: "/data/.state"}
		Expect(s.isPersistentVolume("", "/data")).To(BeTrue())
		Expect(s.isPersistentVolume("", "/usr/local")).To(BeFalse())
	})

	It("falls back to the default when nothing has read cos-layout.env yet", func() {
		s := &State{}
		Expect(s.isPersistentVolume("", "/usr/local")).To(BeTrue())
	})

	It("accepts the state target itself as the mountpoint", func() {
		s := &State{StateDir: cnst.PersistentStateTarget}
		Expect(s.isPersistentVolume("", "/usr/local/.state")).To(BeTrue())
	})

	It("does not match an ancestor above the parent", func() {
		// A VOLUMES entry at /usr is not the persistent volume, and an RW_PATHS
		// entry /usr must not be dropped from the ephemeral overlays for it.
		s := &State{StateDir: cnst.PersistentStateTarget}
		Expect(s.isPersistentVolume("", "/usr")).To(BeFalse())
		Expect(s.isPersistentVolume("/dev/sdb1", "/")).To(BeFalse())
	})

	It("still recognises the persistent label at an unusual mountpoint", func() {
		// LABEL=COS_PERSISTENT:/data was mounted read-write before the predicate
		// moved to the mountpoint, and must stay that way. The label is the
		// second signal.
		s := &State{StateDir: cnst.PersistentStateTarget}
		Expect(s.isPersistentVolume("/dev/disk/by-label/COS_PERSISTENT", "/data")).To(BeTrue())
		Expect(s.isPersistentVolume("/dev/disk/by-label/COS_PERSISTENT_LUKS", "/data")).To(BeTrue())
	})

	It("ignores an unrelated volume", func() {
		s := &State{
			StateDir:     cnst.PersistentStateTarget,
			CustomMounts: map[string]string{"/dev/sdb1": "/data"},
		}
		Expect(s.isPersistentVolume("/dev/sdb1", "/data")).To(BeFalse())

		_, _, ok := s.persistentVolume()
		Expect(ok).To(BeFalse())
	})

	It("does not match a volume whose device path merely says persistent", func() {
		// The mirror of the UUID bug: the first attempt matched any device path
		// containing "persistent", so an unrelated volume was force-remapped.
		s := &State{
			StateDir:     cnst.PersistentStateTarget,
			CustomMounts: map[string]string{"/dev/disk/by-label/my-persistent-data": "/data"},
		}
		_, _, ok := s.persistentVolume()
		Expect(ok).To(BeFalse())
	})

	It("tolerates a trailing slash on the mountpoint", func() {
		s := &State{StateDir: cnst.PersistentStateTarget}
		Expect(s.isPersistentVolume("", "/usr/local/")).To(BeTrue())
	})
})

var _ = Describe("customMountPlan", func() {
	// The decision the #4405 review asked to see tested: where the persistent
	// volume lands and with which options, for every way of declaring it, with
	// the read-only layout on and off.
	root := "/sysroot"

	DescribeTable("the persistent volume on write-protected media is parked read-only out of the way",
		func(device string) {
			s := &State{HardwareRO: true, Rootdir: root, StateDir: cnst.PersistentStateTarget}
			target, options := s.customMountPlan(device, "/usr/local")
			Expect(target).To(Equal(cnst.PersistentROMount))
			Expect(options("ext4")).To(Equal([]string{"ro", "noload"}))
			Expect(options("xfs")).To(Equal([]string{"ro", "norecovery"}))
		},
		Entry("declared by label", "/dev/disk/by-label/COS_PERSISTENT"),
		Entry("declared by UUID", "/dev/disk/by-uuid/0a1b2c3d-4e5f-6071-8293-a4b5c6d7e8f9"),
		Entry("declared by device path", "/dev/sda5"),
		Entry("already resolved to a mapper", "/dev/mapper/COS_PERSISTENT"),
	)

	DescribeTable("the persistent volume on writable media is mounted read-write in place",
		func(device string) {
			s := &State{HardwareRO: false, Rootdir: root, StateDir: cnst.PersistentStateTarget}
			target, options := s.customMountPlan(device, "/usr/local")
			Expect(target).To(Equal(filepath.Join(root, "usr/local")))
			Expect(options("ext4")).To(Equal([]string{"rw"}))
		},
		Entry("declared by label", "/dev/disk/by-label/COS_PERSISTENT"),
		Entry("declared by UUID", "/dev/disk/by-uuid/0a1b2c3d-4e5f-6071-8293-a4b5c6d7e8f9"),
		Entry("declared by device path", "/dev/sda5"),
	)

	It("mounts any other volume read-only in place, whatever the media", func() {
		for _, ro := range []bool{true, false} {
			s := &State{HardwareRO: ro, Rootdir: root, StateDir: cnst.PersistentStateTarget}
			target, options := s.customMountPlan("/dev/sdb1", "/data")
			Expect(target).To(Equal(filepath.Join(root, "data")))
			Expect(options("ext4")).To(Equal([]string{"ro"}))
		}
	})

	It("keeps a label-declared persistent volume at an unusual mountpoint read-write", func() {
		s := &State{HardwareRO: false, Rootdir: root, StateDir: cnst.PersistentStateTarget}
		_, options := s.customMountPlan("/dev/disk/by-label/COS_PERSISTENT", "/data")
		Expect(options("ext4")).To(Equal([]string{"rw"}))
	})
})

var _ = Describe("readOnlyOrMode", func() {
	It("keeps the configured mode on writable media", func() {
		s := &State{RootMountMode: "ro"}
		Expect(s.readOnlyOrMode("ext4")).To(Equal([]string{"ro"}))

		s = &State{RootMountMode: "rw"}
		Expect(s.readOnlyOrMode("ext4")).To(Equal([]string{"rw"}))
	})

	It("adds the no-recovery option on write-protected media", func() {
		// COS_STATE and the root image are mounted before anything knows about
		// the persistent partition. A bare ro there still replays a dirty ext4
		// journal, and the replay is a write.
		s := &State{HardwareRO: true, RootMountMode: "ro"}
		Expect(s.readOnlyOrMode("ext4")).To(Equal([]string{"ro", "noload"}))
		Expect(s.readOnlyOrMode("xfs")).To(Equal([]string{"ro", "norecovery"}))
	})
})

var _ = Describe("buildOverlayOn", func() {
	It("produces the overlay fstab entry the booted system needs", func() {
		s := &State{Rootdir: "/sysroot"}
		operation := s.buildOverlayOn("/usr/local", cnst.PersistentROMount)

		Expect(operation.Target).To(Equal("/sysroot/usr/local"))
		Expect(operation.FstabEntry.File).To(Equal("/usr/local"))
		Expect(operation.FstabEntry.VfsType).To(Equal("overlay"))
		Expect(operation.FstabEntry.MntOps).To(HaveKeyWithValue("lowerdir", cnst.PersistentROMount))
		// Ordering insurance for systemd, which cannot see that lowerdir names
		// a mount. On the fstab entry only, never on the mount options.
		Expect(operation.FstabEntry.MntOps).To(HaveKeyWithValue("x-systemd.requires", cnst.PersistentROMount))
		Expect(operation.MountOption.Options).ToNot(ContainElement(ContainSubstring("x-systemd")))
		// And nothing that would make systemd remount it read-only.
		Expect(operation.FstabEntry.MntOps).ToNot(HaveKey("ro"))
	})

	It("adds no systemd ordering for the tmpfs-only fallback", func() {
		s := &State{Rootdir: "/sysroot"}
		operation := s.buildOverlayOn("/usr/local", "/sysroot/usr/local")
		Expect(operation.FstabEntry.MntOps).ToNot(HaveKey("x-systemd.requires"))
	})
})

var _ = Describe("MountPersistentROOverlayDagStep", func() {
	newState := func() *State {
		return &State{
			HardwareRO:   true,
			Rootdir:      GinkgoT().TempDir(),
			StateDir:     cnst.PersistentStateTarget,
			CustomMounts: map[string]string{"/dev/disk/by-label/COS_PERSISTENT": "/usr/local"},
		}
	}

	errorOf := func(g *herd.Graph, name string) error {
		for _, layer := range g.Analyze() {
			for _, op := range layer {
				if op.Name == name {
					return op.Error
				}
			}
		}
		return nil
	}

	It("makes the boot failure visible when the lower layer is not mounted", func() {
		// An ordinary op's error only skips its dependents: Run() returns nil,
		// immucore exits 0 and the node boots with none of its persistent state
		// and nothing on the console. The op is registered as herd.FatalOp so
		// that Run() returns the error and root.go paints the failure summary.
		previous := lowerIsMounted
		lowerIsMounted = func(string) bool { return false }
		DeferCleanup(func() { lowerIsMounted = previous })

		s := newState()
		g := herd.DAG(herd.EnableInit)
		Expect(g.Add(cnst.OpLoadConfig)).To(Succeed())
		Expect(g.Add(cnst.OpCustomMounts)).To(Succeed())
		Expect(g.Add(cnst.OpMountBaseOverlay)).To(Succeed())
		Expect(s.MountPersistentROOverlayDagStep(g)).To(Succeed())

		err := g.Run(context.Background())
		Expect(err).To(MatchError(ContainSubstring("is not mounted at")),
			"a fatal op's error must come back out of Run(), or nobody sees it")
		Expect(errorOf(g, cnst.OpPersistentROOverlay)).To(MatchError(ContainSubstring("is not mounted at")))
	})

	It("is skipped, and still fatal, when the persistent mount itself failed", func() {
		// herd skips an op whose strong dependency errored, marking it with a
		// "deps ... failed" error of its own. Being FatalOp, that surfaces from
		// Run() too, so a failed persistent mount is as visible as a failed
		// overlay. The lowerIsMounted guard is never consulted on this path.
		previous := lowerIsMounted
		lowerIsMounted = func(string) bool {
			Fail("the guard ran, but herd should have skipped the op before it")
			return false
		}
		DeferCleanup(func() { lowerIsMounted = previous })

		s := newState()
		g := herd.DAG(herd.EnableInit)
		Expect(g.Add(cnst.OpLoadConfig)).To(Succeed())
		Expect(g.Add(cnst.OpCustomMounts, herd.WithCallback(func(context.Context) error {
			return errors.New("mounting persistent: permission denied")
		}))).To(Succeed())
		Expect(g.Add(cnst.OpMountBaseOverlay)).To(Succeed())
		Expect(s.MountPersistentROOverlayDagStep(g)).To(Succeed())

		err := g.Run(context.Background())
		Expect(err).To(HaveOccurred())
		Expect(errorOf(g, cnst.OpPersistentROOverlay)).To(MatchError(ContainSubstring("deps")))
		Expect(errorOf(g, cnst.OpPersistentROOverlay)).To(MatchError(ContainSubstring(cnst.OpCustomMounts)))
	})
})

var _ = Describe("oemMountOptions", func() {
	It("mounts /oem read-write on writable media", func() {
		s := &State{}
		Expect(s.oemMountOptions("ext4")).To(Equal([]string{"rw", "suid", "dev", "exec", "async"}))
	})

	It("mounts /oem read-only, without journal replay, on write-protected media", func() {
		// No overlay for /oem on purpose: it holds authored configuration, and
		// a write that appeared to succeed and vanished on reboot is worse than
		// one that fails.
		s := &State{HardwareRO: true}
		Expect(s.oemMountOptions("ext4")).To(Equal([]string{"ro", "noload", "suid", "dev", "exec", "async"}))
		Expect(s.oemMountOptions("xfs")).To(Equal([]string{"ro", "norecovery", "suid", "dev", "exec", "async"}))
	})
})

var _ = Describe("RunKcryptUpgrade on write-protected media", func() {
	It("does not rewrite the LUKS headers", func() {
		called := false
		previous := upgradeKcryptPartitions
		upgradeKcryptPartitions = func() error {
			called = true
			return nil
		}
		DeferCleanup(func() { upgradeKcryptPartitions = previous })

		s := &State{HardwareRO: true}
		g := herd.DAG(herd.EnableInit)
		Expect(s.RunKcryptUpgrade(g)).To(Succeed())
		Expect(g.Run(context.Background())).To(Succeed())
		Expect(called).To(BeFalse(), "cryptsetup luksUUID --uuid writes the header, which the media refuses")
	})

	It("still runs on writable media", func() {
		called := false
		previous := upgradeKcryptPartitions
		upgradeKcryptPartitions = func() error {
			called = true
			return nil
		}
		DeferCleanup(func() { upgradeKcryptPartitions = previous })

		s := &State{}
		g := herd.DAG(herd.EnableInit)
		Expect(s.RunKcryptUpgrade(g)).To(Succeed())
		Expect(g.Run(context.Background())).To(Succeed())
		Expect(called).To(BeTrue())
	})
})
