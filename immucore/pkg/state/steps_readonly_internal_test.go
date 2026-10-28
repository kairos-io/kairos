package state

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/deniswernert/go-fstab"
	cnst "github.com/kairos-io/kairos/v4/immucore/internal/constants"
	internalUtils "github.com/kairos-io/kairos/v4/immucore/internal/utils"
	"github.com/kairos-io/kairos/v4/immucore/pkg/op"
	"github.com/kairos-io/kairos/v4/sdk/blockdev"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spectrocloud-labs/herd"
)

var _ = Describe("isPersistentVolume", func() {
	// The predicate keys off the mountpoint because that is the only half of a
	// VOLUMES entry that survives every way of declaring the partition. A device
	// string match misses a volume declared by UUID and applies the layout to
	// only half the mounts.

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
		// A substring match on the device path would remap an unrelated volume
		// whose name happens to contain the word.
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
	// Where the persistent volume lands and with which options, for every way
	// of declaring it. The answer does not depend on the media: on a
	// write-protected disk the snapshot step has already swapped the device
	// for its copy-on-write view, and that is mounted like any other disk.
	root := "/sysroot"

	DescribeTable("the persistent volume is mounted read-write in place",
		func(device string) {
			for _, ro := range []bool{true, false} {
				s := &State{WriteProtected: ro, Rootdir: root, StateDir: cnst.PersistentStateTarget}
				target, options := s.customMountPlan(device, "/usr/local")
				Expect(target).To(Equal(filepath.Join(root, "usr/local")))
				Expect(options("ext4")).To(Equal([]string{"rw"}))
			}
		},
		Entry("declared by label", "/dev/disk/by-label/COS_PERSISTENT"),
		Entry("declared by UUID", "/dev/disk/by-uuid/0a1b2c3d-4e5f-6071-8293-a4b5c6d7e8f9"),
		Entry("declared by device path", "/dev/sda5"),
		Entry("already swapped for the snapshot", "/dev/mapper/"+cnst.PersistentSnapshotName),
	)

	It("mounts any other volume read-only in place, whatever the media", func() {
		for _, ro := range []bool{true, false} {
			s := &State{WriteProtected: ro, Rootdir: root, StateDir: cnst.PersistentStateTarget}
			target, options := s.customMountPlan("/dev/sdb1", "/data")
			Expect(target).To(Equal(filepath.Join(root, "data")))
			Expect(options("ext4")).To(Equal([]string{"ro"}))
		}
	})

	It("keeps a label-declared persistent volume at an unusual mountpoint read-write", func() {
		s := &State{WriteProtected: false, Rootdir: root, StateDir: cnst.PersistentStateTarget}
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
		s := &State{WriteProtected: true, RootMountMode: "ro"}
		Expect(s.readOnlyOrMode("ext4")).To(Equal([]string{"ro", "noload"}))
		Expect(s.readOnlyOrMode("xfs")).To(Equal([]string{"ro", "norecovery"}))
	})
})

var _ = Describe("MountPersistentSnapshotDagStep", func() {
	// The step is driven through its seams: there is no tmpfs to mount, no
	// block device to snapshot and no loop device on the test host.
	type calls struct {
		cowSpec    string
		sparsePath string
		sparseSize uint64
		loopPath   string
		name       string
		origin     string
		cow        string
	}
	const tmpfsBytes = uint64(1 << 30)

	var got calls
	install := func(snapshotErr error) {
		got = calls{}
		previous := []any{mountCowStore, cowTmpfsSize, createSparse, attachLoop, createSnapshot, snapshotStatus}
		mountCowStore = func(spec string) (op.MountOperation, error) {
			got.cowSpec = spec
			return op.MountOperation{FstabEntry: fstab.Mount{Spec: "tmpfs", File: cnst.PersistentCowDir, VfsType: "tmpfs"}}, nil
		}
		cowTmpfsSize = func(string) (uint64, error) { return tmpfsBytes, nil }
		createSparse = func(path string, size uint64) error {
			got.sparsePath, got.sparseSize = path, size
			return nil
		}
		attachLoop = func(path string) (string, error) {
			got.loopPath = path
			return "/dev/loop7", nil
		}
		createSnapshot = func(name, origin, cow string) error {
			got.name, got.origin, got.cow = name, origin, cow
			return snapshotErr
		}
		snapshotStatus = func(string) (blockdev.SnapshotStatus, error) {
			return blockdev.SnapshotStatus{UsedSectors: 16, TotalSectors: 2048, State: "active"}, nil
		}
		DeferCleanup(func() {
			mountCowStore = previous[0].(func(string) (op.MountOperation, error))
			cowTmpfsSize = previous[1].(func(string) (uint64, error))
			createSparse = previous[2].(func(string, uint64) error)
			attachLoop = previous[3].(func(string) (string, error))
			createSnapshot = previous[4].(func(string, string, string) error)
			snapshotStatus = previous[5].(func(string) (blockdev.SnapshotStatus, error))
		})
	}

	newState := func() *State {
		return &State{
			WriteProtected: true,
			Rootdir:        GinkgoT().TempDir(),
			StateDir:       cnst.PersistentStateTarget,
			CowBase:        "tmpfs:10%",
			// A plain device path, so the step has no label to resolve
			// through udev.
			CustomMounts: map[string]string{"/dev/sda5": "/usr/local"},
		}
	}

	run := func(s *State) error {
		g := herd.DAG(herd.EnableInit)
		Expect(g.Add(cnst.OpLoadConfig)).To(Succeed())
		Expect(s.MountPersistentSnapshotDagStep(g)).To(Succeed())
		return g.Run(context.Background())
	}

	It("puts the snapshot in the persistent volume's place", func() {
		install(nil)
		s := newState()
		Expect(run(s)).To(Succeed())

		Expect(got.cowSpec).To(Equal("tmpfs:10%"))
		Expect(got.sparsePath).To(Equal(cnst.PersistentCowFile))
		Expect(got.sparseSize).To(Equal(internalUtils.CowStoreSize(tmpfsBytes)))
		Expect(got.loopPath).To(Equal(cnst.PersistentCowFile))
		Expect(got.name).To(Equal(cnst.PersistentSnapshotName))
		Expect(got.origin).To(Equal("/dev/sda5"))
		Expect(got.cow).To(Equal("/dev/loop7"))

		// The custom mount step now sees the snapshot under the same
		// mountpoint, and nothing else.
		Expect(s.CustomMounts).To(Equal(map[string]string{
			"/dev/mapper/" + cnst.PersistentSnapshotName: "/usr/local",
		}))
		// The store's tmpfs reaches the fstab, so it survives switch_root.
		Expect(s.fstabs).To(HaveLen(1))
		Expect(s.fstabs[0].File).To(Equal(cnst.PersistentCowDir))
	})

	It("does nothing when no persistent volume is configured", func() {
		install(nil)
		s := newState()
		s.CustomMounts = map[string]string{"/dev/sdb1": "/data"}
		Expect(run(s)).To(Succeed())
		Expect(got).To(Equal(calls{}))
		Expect(s.CustomMounts).To(Equal(map[string]string{"/dev/sdb1": "/data"}))
	})

	It("makes a failure visible", func() {
		// An ordinary op's error only skips its dependents: Run() returns nil,
		// immucore exits 0 and the node boots with none of its persistent state
		// and nothing on the console. The op is registered as herd.FatalOp so
		// that Run() returns the error and root.go paints the failure summary.
		install(errors.New("dmsetup create kairos-persistent: device-mapper: reload ioctl failed"))
		s := newState()
		err := run(s)
		Expect(err).To(MatchError(ContainSubstring("dmsetup create")),
			"a fatal op's error must come back out of Run(), or nobody sees it")
		// And the volume is left as it was, so the failure is not compounded
		// by a mount of a mapper that does not exist.
		Expect(s.CustomMounts).To(HaveKey("/dev/sda5"))
	})
})

var _ = Describe("WriteProtectedSnapshotDeps", func() {
	It("adds nothing on a writable install", func() {
		s := &State{}
		Expect(s.WriteProtectedSnapshotDeps()).To(BeEmpty())
	})

	It("makes the custom mounts wait for the snapshot on write-protected media", func() {
		s := &State{WriteProtected: true}
		g := herd.DAG(herd.EnableInit)
		Expect(g.Add(cnst.OpPersistentSnapshot)).To(Succeed())
		Expect(g.Add(cnst.OpCustomMounts, s.WriteProtectedSnapshotDeps()...)).To(Succeed())

		layerOf := func(name string) int {
			for i, layer := range g.Analyze() {
				for _, op := range layer {
					if op.Name == name {
						return i
					}
				}
			}
			return -1
		}
		Expect(layerOf(cnst.OpPersistentSnapshot)).To(BeNumerically("<", layerOf(cnst.OpCustomMounts)))
	})
})

var _ = Describe("oemMountOptions", func() {
	It("mounts /oem read-write on writable media", func() {
		s := &State{}
		Expect(s.oemMountOptions("ext4")).To(Equal([]string{"rw", "suid", "dev", "exec", "async"}))
	})

	It("mounts /oem read-only, without journal replay, on write-protected media", func() {
		// /oem gets no overlay: it holds authored configuration, and a write
		// that appeared to succeed and vanished on reboot is worse than one
		// that fails.
		s := &State{WriteProtected: true}
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

		s := &State{WriteProtected: true}
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
