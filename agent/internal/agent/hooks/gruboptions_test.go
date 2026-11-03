package hook_test

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	hook "github.com/kairos-io/kairos/v4/agent/internal/agent/hooks"
	"github.com/kairos-io/kairos/v4/agent/pkg/config"
	cnst "github.com/kairos-io/kairos/v4/agent/pkg/constants"
	fsutils "github.com/kairos-io/kairos/v4/agent/pkg/utils/fs"
	"github.com/kairos-io/kairos/v4/sdk/collector"
	ghwMock "github.com/kairos-io/kairos/v4/sdk/ghw/mocks"
	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
	sdkFs "github.com/kairos-io/kairos/v4/sdk/types/fs"
	install "github.com/kairos-io/kairos/v4/sdk/types/install"
	sdkLogger "github.com/kairos-io/kairos/v4/sdk/types/logger"
	sdkPartitions "github.com/kairos-io/kairos/v4/sdk/types/partitions"
	sdkSpec "github.com/kairos-io/kairos/v4/sdk/types/spec"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/twpayne/go-vfs/v5"
	"github.com/twpayne/go-vfs/v5/vfst"
	"k8s.io/mount-utils"
)

var _ = Describe("SelinuxGrubOpts", func() {
	DescribeTable(
		"builds the dedicated grubenv var pair",
		func(selinux install.SelinuxOptions, expected map[string]string) {
			Expect(hook.SelinuxGrubOpts(selinux)).To(Equal(expected))
		},
		Entry("field off (zero value) writes nothing",
			install.SelinuxOptions{},
			nil),
		Entry("enabled with no mode defaults to permissive",
			install.SelinuxOptions{Enabled: true},
			map[string]string{
				"selinux_enabled": "true",
				"selinux_mode":    "permissive",
			}),
		Entry("enabled with explicit permissive mode",
			install.SelinuxOptions{
				Enabled: true,
				Mode:    "permissive",
			},
			map[string]string{
				"selinux_enabled": "true",
				"selinux_mode":    "permissive",
			}),
		Entry("enabled with enforcing mode",
			install.SelinuxOptions{
				Enabled: true,
				Mode:    "enforcing",
			},
			map[string]string{
				"selinux_enabled": "true",
				"selinux_mode":    "enforcing",
			}),
	)
})

// roStateFS models what the initramfs leaves behind on a booted node:
// COS_STATE's superblock is mounted read-only, so every write under
// /run/cos/state fails with EROFS until something remounts it read-write.
// Mounting the device a second time does not help, it shares the superblock.
type roStateFS struct {
	sdkFs.KairosFS
	readOnly bool
}

func (f *roStateFS) WriteFile(name string, data []byte, perm os.FileMode) error {
	if f.readOnly && strings.HasPrefix(name, cnst.StateDir) {
		return &os.PathError{Op: "open", Path: name, Err: syscall.EROFS}
	}
	return f.KairosFS.WriteFile(name, data, perm)
}

// remountMounter flips that flag the way `mount -o remount,rw` flips the
// superblock, and records every mount call so the test can assert the
// read-only flag is put back.
type remountMounter struct {
	mount.Interface
	fs  *roStateFS
	log []string
}

func (m *remountMounter) Mount(source, target, fstype string, options []string) error {
	m.log = append(m.log, fmt.Sprintf("%s %s [%s]", source, target, strings.Join(options, ",")))
	if target == cnst.StateDir {
		for _, o := range options {
			switch o {
			case "rw":
				m.fs.readOnly = false
			case "ro":
				m.fs.readOnly = true
			}
		}
	}
	return m.Interface.Mount(source, target, fstype, options)
}

// failingMounter refuses one kind of mount call on the STATE mountpoint and
// passes the rest through, so a test can pick which step of the STATE write
// breaks: the plain mount, or the remount that makes the filesystem writable.
type failingMounter struct {
	mount.Interface
	fs     *roStateFS
	failOn string
}

func (m *failingMounter) Mount(source, target, fstype string, options []string) error {
	if target == cnst.StateDir && strings.Join(options, ",") == m.failOn {
		return fmt.Errorf("mount %s %s: fake failure", source, target)
	}
	if target == cnst.StateDir {
		for _, o := range options {
			switch o {
			case "rw":
				m.fs.readOnly = false
			case "ro":
				m.fs.readOnly = true
			}
		}
	}
	return m.Interface.Mount(source, target, fstype, options)
}

// recordingHook stands in for the hooks that follow GrubFirstBootOptions in
// the first boot chain, FirstBootStage above all: if it did not run, neither
// did the firstboot sentinel write nor the bootstrap event behind it.
type recordingHook struct {
	ran bool
}

func (h *recordingHook) Run(_ sdkConfig.Config, _ sdkSpec.Spec) error {
	h.ran = true
	return nil
}

var _ = Describe("GrubFirstBootOptions", func() {
	var cfg *sdkConfig.Config
	var fs vfs.FS
	var stateFS *roStateFS
	var mounter *remountMounter
	var memLog *bytes.Buffer
	var ghwTest ghwMock.GhwMock
	var cleanup func()
	var err error

	// runWithParts presents ghw with a single disk holding the given
	// partitions and runs the given hooks as a chain, the way the first boot
	// block runs them.
	runWithParts := func(parts []*sdkPartitions.Partition, hooks ...hook.Interface) error {
		ghwTest = ghwMock.GhwMock{}
		ghwTest.AddDisk(sdkPartitions.Disk{Name: "vda", Partitions: parts})
		ghwTest.CreateDevices()
		DeferCleanup(ghwTest.Clean)
		return hook.Run(*cfg, nil, hooks...)
	}

	// runWithOEM presents ghw with a single disk holding COS_STATE and the
	// given COS_OEM partition, and runs the hook. A nil partition is a node
	// where the OEM label is nowhere to be found.
	runWithOEM := func(oem *sdkPartitions.Partition) error {
		parts := []*sdkPartitions.Partition{
			{Name: "vda1", Path: "/dev/vda1", FilesystemLabel: cnst.StateLabel, FS: "ext4"},
		}
		if oem != nil {
			parts = append(parts, oem)
		}
		return runWithParts(parts, hook.GrubFirstBootOptions{})
	}

	grubenv := func(dir string) (string, error) {
		content, readErr := fs.ReadFile(filepath.Join(dir, cnst.GrubEnv))
		return string(content), readErr
	}

	BeforeEach(func() {
		memLog = &bytes.Buffer{}
		logger := sdkLogger.NewBufferLogger(memLog)
		logger.SetLevel("debug")
		fs, cleanup, err = vfst.NewTestFS(map[string]interface{}{
			cnst.StateDir: &vfst.Dir{Perm: 0755},
			cnst.OEMPath:  &vfst.Dir{Perm: 0755},
		})
		Expect(err).ShouldNot(HaveOccurred())
		stateFS = &roStateFS{KairosFS: fs, readOnly: true}
		mounter = &remountMounter{Interface: mount.NewFakeMounter(nil), fs: stateFS}
		cfg = config.NewConfig(
			config.WithFs(stateFS),
			config.WithLogger(logger),
			config.WithMounter(mounter),
		)
		cfg.Collector = collector.Config{}
		cfg.GrubOptions = map[string]string{"default_menu_entry": "My Kairos"}
	})

	AfterEach(func() {
		cleanup()
	})

	It("writes to the STATE grubenv when COS_OEM is a LUKS container", func() {
		// GRUB searches the filesystems it can read for /grubenv, and an
		// encrypted COS_OEM is not one of them, so the only grubenv that can
		// ever be loaded on this node is the STATE one. STATE arrives
		// read-only from the initramfs, so getting there means remounting it.
		Expect(runWithOEM(&sdkPartitions.Partition{
			Name:            "vda2",
			Path:            "/dev/vda2",
			PartitionLabel:  "oem",
			FilesystemLabel: cnst.OEMLabel,
			FS:              "crypto_LUKS",
		})).Should(Succeed())

		Expect(grubenv(cnst.StateDir)).To(ContainSubstring("default_menu_entry=My Kairos"),
			"the options must land in the grubenv GRUB can read, log: %s", memLog.String())
		_, readErr := grubenv(cnst.OEMPath)
		Expect(readErr).To(HaveOccurred(),
			"nothing may be written to the encrypted OEM, where it would be lost")
	})

	It("leaves the STATE partition read-only again", func() {
		// The remount changes the superblock, so it also changes the
		// initramfs mount of the same device at /run/initramfs/cos-state.
		// Leaving STATE writable after the hook is not this hook's business.
		Expect(runWithOEM(&sdkPartitions.Partition{
			Name:            "vda2",
			Path:            "/dev/vda2",
			FilesystemLabel: cnst.OEMLabel,
			FS:              "crypto_LUKS",
		})).Should(Succeed())

		Expect(stateFS.readOnly).To(BeTrue(), "mount calls: %v", mounter.log)
		Expect(mounter.log).To(ContainElement("/dev/vda1 " + cnst.StateDir + " [remount,rw]"))
		Expect(mounter.log).To(ContainElement("/dev/vda1 " + cnst.StateDir + " [remount,ro]"))
	})

	It("writes to the OEM grubenv when COS_OEM is plaintext", func() {
		Expect(runWithOEM(&sdkPartitions.Partition{
			Name:            "vda2",
			Path:            "/dev/vda2",
			PartitionLabel:  "oem",
			FilesystemLabel: cnst.OEMLabel,
			FS:              "ext4",
		})).Should(Succeed())

		Expect(grubenv(cnst.OEMPath)).To(ContainSubstring("default_menu_entry=My Kairos"),
			"a plaintext OEM keeps the single runtime-modifiable grubenv, log: %s", memLog.String())
		_, readErr := grubenv(cnst.StateDir)
		Expect(readErr).To(HaveOccurred(),
			"writing both would leave GRUB to pick one by search order")
	})

	It("says so when COS_OEM cannot be classified, and moves nothing", func() {
		// No COS_OEM on any disk. Moving the file to STATE on a guess is
		// worse than the behaviour this hook has always had, but the log has
		// to name the risk, because on an encrypted node the options are
		// lost. Nothing reaches OEM either: that write resolves the same
		// label, so it reports the partition is missing instead of putting
		// the grubenv on the ephemeral root (kairos-io/kairos#5168).
		Expect(runWithOEM(nil)).Should(Succeed())

		Expect(memLog.String()).To(ContainSubstring("Could not determine whether COS_OEM is encrypted"))
		_, readErr := grubenv(cnst.StateDir)
		Expect(readErr).To(HaveOccurred(), "the file must not move to STATE on a guess")
		_, readErr = grubenv(cnst.OEMPath)
		Expect(readErr).To(HaveOccurred(), "nothing may be written when there is no OEM partition")
	})

	It("does nothing when no grub options are set", func() {
		cfg.GrubOptions = nil
		Expect(runWithOEM(&sdkPartitions.Partition{
			Name:            "vda2",
			Path:            "/dev/vda2",
			FilesystemLabel: cnst.OEMLabel,
			FS:              "crypto_LUKS",
		})).Should(Succeed())

		_, readErr := grubenv(cnst.OEMPath)
		Expect(readErr).To(HaveOccurred())
		_, readErr = grubenv(cnst.StateDir)
		Expect(readErr).To(HaveOccurred())
	})

	Context("when the STATE write fails", func() {
		// This hook runs inside the sentinel-gated first boot block, so an
		// error returned from here aborts the chain before the firstboot
		// sentinel is written and before the bootstrap event that starts the
		// provider. The node would then repeat the whole block on every
		// restart of the agent unit, and never come up. Every one of these
		// failures has to end in a log line and a nil.
		encryptedOEM := []*sdkPartitions.Partition{
			{Name: "vda1", Path: "/dev/vda1", FilesystemLabel: cnst.StateLabel, FS: "ext4"},
			{Name: "vda2", Path: "/dev/vda2", FilesystemLabel: cnst.OEMLabel, FS: "crypto_LUKS"},
		}
		var next *recordingHook

		BeforeEach(func() {
			next = &recordingHook{}
		})

		It("carries on when the STATE partition cannot be mounted", func() {
			cfg.Mounter = &failingMounter{Interface: mount.NewFakeMounter(nil), fs: stateFS}

			Expect(runWithParts(encryptedOEM, hook.GrubFirstBootOptions{}, next)).To(Succeed())

			Expect(next.ran).To(BeTrue(), "the rest of the first boot chain must still run, log: %s", memLog.String())
			Expect(memLog.String()).To(ContainSubstring("Failed to mount the STATE partition"))
			Expect(memLog.String()).To(ContainSubstring("continuing so the firstboot sentinel still gets written"))
			_, readErr := grubenv(cnst.StateDir)
			Expect(readErr).To(HaveOccurred())
		})

		It("carries on when the write fails because STATE stayed read-only", func() {
			// The remount is the only thing standing between the initramfs
			// read-only mount and the write, so when it fails the write
			// fails with EROFS.
			cfg.Mounter = &failingMounter{Interface: mount.NewFakeMounter(nil), fs: stateFS, failOn: "remount,rw"}

			Expect(runWithParts(encryptedOEM, hook.GrubFirstBootOptions{}, next)).To(Succeed())

			Expect(next.ran).To(BeTrue(), "the rest of the first boot chain must still run, log: %s", memLog.String())
			Expect(memLog.String()).To(ContainSubstring("Could not remount the STATE partition read-write"))
			Expect(memLog.String()).To(ContainSubstring("Failed to set grub options in STATE"))
			Expect(memLog.String()).To(ContainSubstring("continuing so the firstboot sentinel still gets written"))
		})

		It("carries on when the STATE partition is nowhere to be found", func() {
			Expect(runWithParts([]*sdkPartitions.Partition{
				{Name: "vda2", Path: "/dev/vda2", FilesystemLabel: cnst.OEMLabel, FS: "crypto_LUKS"},
			}, hook.GrubFirstBootOptions{}, next)).To(Succeed())

			Expect(next.ran).To(BeTrue(), "the rest of the first boot chain must still run, log: %s", memLog.String())
			Expect(memLog.String()).To(ContainSubstring("Failed to find the STATE partition"))
		})

		It("still writes when the remount fails on a filesystem that is writable anyway", func() {
			// The remount is best effort: a filesystem that is already
			// writable must not lose its grub options because mount refused
			// a flag it did not need.
			stateFS.readOnly = false
			cfg.Mounter = &failingMounter{Interface: mount.NewFakeMounter(nil), fs: stateFS, failOn: "remount,rw"}

			Expect(runWithParts(encryptedOEM, hook.GrubFirstBootOptions{}, next)).To(Succeed())

			Expect(next.ran).To(BeTrue())
			Expect(grubenv(cnst.StateDir)).To(ContainSubstring("default_menu_entry=My Kairos"),
				"log: %s", memLog.String())
			Expect(memLog.String()).To(ContainSubstring("Could not remount the STATE partition read-write"))
		})
	})
})

// failingMountMounter refuses the mount, so the grubenv write would land on
// whatever /oem is when the partition is not there.
type failingMountMounter struct {
	mount.Interface
}

func (failingMountMounter) Mount(_, _, _ string, _ []string) error {
	return errors.New("no such device")
}

var _ = Describe("GrubFirstBootOptions mounts", func() {
	var cfg *sdkConfig.Config
	var fs vfs.FS
	var mounter *mount.FakeMounter
	var memLog *bytes.Buffer
	var cleanup func()
	var err error

	// withOEM presents ghw with a single disk holding a plaintext COS_OEM, the
	// partition the hook has to resolve when it mounts OEM itself.
	withOEM := func() {
		ghwTest := ghwMock.GhwMock{}
		ghwTest.AddDisk(sdkPartitions.Disk{Name: "vda", Partitions: []*sdkPartitions.Partition{
			{Name: "vda1", Path: "/dev/vda1", FilesystemLabel: cnst.StateLabel, FS: "ext4"},
			{Name: "vda2", Path: "/dev/vda2", FilesystemLabel: cnst.OEMLabel, FS: "ext4"},
		}})
		ghwTest.CreateDevices()
		DeferCleanup(ghwTest.Clean)
	}

	grubenv := func() (string, error) {
		content, readErr := fs.ReadFile(filepath.Join(cnst.OEMPath, cnst.GrubEnv))
		return string(content), readErr
	}

	// mountPaths is what `findmnt` would report after the hook has run.
	mountPaths := func() []string {
		points, listErr := mounter.List()
		Expect(listErr).ShouldNot(HaveOccurred())
		paths := []string{}
		for _, p := range points {
			paths = append(paths, p.Path)
		}
		return paths
	}

	BeforeEach(func() {
		memLog = &bytes.Buffer{}
		logger := sdkLogger.NewBufferLogger(memLog)
		logger.SetLevel("debug")
		fs, cleanup, err = vfst.NewTestFS(map[string]interface{}{
			cnst.OEMPath: &vfst.Dir{Perm: 0755},
		})
		Expect(err).ShouldNot(HaveOccurred())
		mounter = mount.NewFakeMounter(nil)
		cfg = config.NewConfig(
			config.WithFs(fs),
			config.WithLogger(logger),
			config.WithMounter(mounter),
		)
		cfg.Collector = collector.Config{}
		cfg.GrubOptions = map[string]string{"default_menu_entry": "My Kairos"}
	})

	AfterEach(func() {
		cleanup()
	})

	It("keeps the OEM mount immucore made for the rest of the boot", func() {
		// On the first boot of an installed node /oem is immucore's mount,
		// not this hook's. The first-boot cloud-config stage runs after this
		// hook and the config.Scan that `agent run` does runs after that, and
		// both read the user's cloud-config from there.
		// The partition is on the disk too, so the only thing standing between
		// the hook and a mount of its own is the question it asks about /oem.
		withOEM()
		mounter.MountPoints = []mount.MountPoint{{Device: "/dev/vda2", Path: cnst.OEMPath, Type: "ext4"}}

		Expect(hook.GrubFirstBootOptions{}.Run(*cfg, nil)).Should(Succeed())

		Expect(grubenv()).To(ContainSubstring("default_menu_entry=My Kairos"),
			"the options still have to be written, log: %s", memLog.String())
		Expect(mountPaths()).To(ContainElement(cnst.OEMPath),
			"the hook must leave the mount table as it found it, actions: %v", mounter.GetLog())
		// The old code reached for machine.Umount and machine.Mount, which
		// shell out and never reach the fake mounter, so an empty action log
		// on its own proves nothing. The log line and the mount table do.
		Expect(memLog.String()).To(ContainSubstring("OEM is already mounted"),
			"the hook has to recognise the mount instead of taking it over")
		for _, action := range mounter.GetLog() {
			Expect(action.Target).ToNot(Equal(cnst.OEMPath),
				"a mount this hook does not own is not its to move, actions: %v", mounter.GetLog())
		}
	})

	It("mounts OEM itself when nothing has, and unmounts it again", func() {
		// At install time the hook does own the mount: the live medium does
		// not mount OEM, so there is nothing to give back.
		withOEM()

		Expect(hook.GrubFirstBootOptions{}.Run(*cfg, nil)).Should(Succeed())

		Expect(grubenv()).To(ContainSubstring("default_menu_entry=My Kairos"),
			"log: %s", memLog.String())
		Expect(mounter.GetLog()).To(ContainElement(mount.FakeAction{
			Action: mount.FakeActionMount, Target: cnst.OEMPath, Source: "/dev/vda2", FSType: "auto",
		}))
		Expect(mountPaths()).ToNot(ContainElement(cnst.OEMPath),
			"a mount this hook made is its own to clean up, actions: %v", mounter.GetLog())
	})

	It("writes nothing when OEM cannot be mounted, and lets the boot carry on", func() {
		// Without the mount the write would go to the ephemeral root, where
		// GRUB never looks, and the old code reported success for it.
		// Failing is not the answer either: a non-zero kairos-agent start
		// ends agent.Run before the firstboot sentinel and the bootstrap
		// event, and systemd restarts the unit every few seconds.
		withOEM()
		cfg.Mounter = failingMountMounter{Interface: mounter}

		Expect(hook.GrubFirstBootOptions{}.Run(*cfg, nil)).Should(Succeed())

		_, readErr := grubenv()
		Expect(readErr).To(HaveOccurred(),
			"nothing may be written when the partition is not there")
		Expect(memLog.String()).To(ContainSubstring("leaving the grub options unwritten"))
	})

	It("writes nothing and carries on when the node has no COS_OEM at all", func() {
		// The agent service runs on live and netboot nodes too, where /oem
		// is the datasource userdata and COS_OEM was never created, so the
		// label does not resolve to anything.
		ghwTest := ghwMock.GhwMock{}
		ghwTest.AddDisk(sdkPartitions.Disk{Name: "vda", Partitions: []*sdkPartitions.Partition{
			{Name: "vda1", Path: "/dev/vda1", FilesystemLabel: cnst.StateLabel, FS: "ext4"},
		}})
		ghwTest.CreateDevices()
		DeferCleanup(ghwTest.Clean)

		Expect(hook.GrubFirstBootOptions{}.Run(*cfg, nil)).Should(Succeed())

		_, readErr := grubenv()
		Expect(readErr).To(HaveOccurred(),
			"nothing may be written when there is no OEM partition")
		Expect(memLog.String()).To(ContainSubstring("leaving the grub options unwritten"))
	})

	It("still fails the install when OEM cannot be mounted", func() {
		// The install path keeps the error. It has just created the
		// partition, so not reaching it is a broken install rather than a
		// node that never had one.
		withOEM()
		cfg.Mounter = failingMountMounter{Interface: mounter}
		cfg.Install.GrubOptions = map[string]string{"default_menu_entry": "My Kairos"}

		Expect(hook.GrubPostInstallOptions{}.Run(*cfg, nil)).ShouldNot(Succeed())

		_, readErr := grubenv()
		Expect(readErr).To(HaveOccurred(),
			"nothing may be written when the partition is not there")
	})
})

var _ = Describe("GrubFirstBootOptions", func() {
	var cfg *sdkConfig.Config
	var fs vfs.FS
	var cleanup func()
	var memLog *bytes.Buffer
	var grubenv string

	// newConfig builds a first-boot config whose /proc/cmdline holds cmdline,
	// with a top-level grub_options key set and /oem already there, so the
	// hook's only remaining decision is whether to write the grubenv.
	newConfig := func(cmdline string) {
		var err error
		fs, cleanup, err = vfst.NewTestFS(map[string]interface{}{"/proc/cmdline": cmdline})
		Expect(err).ShouldNot(HaveOccurred())
		Expect(fsutils.MkdirAll(fs, cnst.OEMPath, os.ModeDir|os.ModePerm)).To(Succeed())

		memLog = &bytes.Buffer{}
		log := sdkLogger.NewBufferLogger(memLog)
		log.SetLevel("debug")
		// This hook resolves and mounts COS_OEM itself when nothing has
		// mounted it, so a grub boot needs the partition on a disk and a
		// mounter that does not touch the host.
		ghwTest := ghwMock.GhwMock{}
		ghwTest.AddDisk(sdkPartitions.Disk{Name: "vda", Partitions: []*sdkPartitions.Partition{
			{Name: "vda1", Path: "/dev/vda1", FilesystemLabel: cnst.StateLabel, FS: "ext4"},
			{Name: "vda2", Path: "/dev/vda2", FilesystemLabel: cnst.OEMLabel, FS: "ext4"},
		}})
		ghwTest.CreateDevices()
		DeferCleanup(ghwTest.Clean)

		cfg = config.NewConfig(config.WithFs(fs), config.WithLogger(log), config.WithMounter(mount.NewFakeMounter(nil)))
		cfg.Collector = collector.Config{}
		cfg.GrubOptions = map[string]string{"extra_cmdline": "console=ttyS0"}
	}

	BeforeEach(func() {
		grubenv = filepath.Join(cnst.OEMPath, cnst.GrubEnv)
	})

	AfterEach(func() {
		if cleanup != nil {
			cleanup()
		}
	})

	// The bug: GrubPostInstallOptions returns early under UKI but this hook
	// did not, so a UKI node with a top-level grub_options key wrote a
	// grubenv that nothing on a UKI boot reads, and logged success for it.
	It("writes no grubenv on a UKI boot, and says why", func() {
		newConfig("rd.immucore.uki root=LABEL=COS_STATE")

		Expect(hook.GrubFirstBootOptions{}.Run(*cfg, nil)).To(Succeed())

		_, err := fs.Stat(grubenv)
		Expect(os.IsNotExist(err)).To(BeTrue(), "a UKI boot must not get a grubenv written under /oem")
		Expect(memLog.String()).To(ContainSubstring("Skipping GrubFirstBootOptions hook in uki mode"))
		Expect(memLog.String()).ToNot(ContainSubstring("Successfully set grub options in OEM"))
	})

	It("still writes the grubenv on a grub boot", func() {
		newConfig("root=LABEL=COS_STATE")

		Expect(hook.GrubFirstBootOptions{}.Run(*cfg, nil)).To(Succeed())

		content, err := fs.ReadFile(grubenv)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(string(content)).To(ContainSubstring("extra_cmdline=console=ttyS0"))
		Expect(memLog.String()).To(ContainSubstring("Successfully set grub options in OEM"))
	})

	// Without a grub_options key there is nothing to apply either way, so the
	// UKI warning must not fire and nag a user who set nothing.
	It("stays quiet when no grub options are set", func() {
		newConfig("rd.immucore.uki root=LABEL=COS_STATE")
		cfg.GrubOptions = nil

		Expect(hook.GrubFirstBootOptions{}.Run(*cfg, nil)).To(Succeed())

		Expect(memLog.String()).ToNot(ContainSubstring("uki mode"))
	})
})

var _ = Describe("GrubPostInstallOptions", func() {
	// Its sibling reads the same cmdline through the same seam, so the two
	// hooks cannot drift apart again on what counts as a UKI boot.
	It("skips a UKI boot read through the config filesystem", func() {
		fs, cleanup, err := vfst.NewTestFS(map[string]interface{}{"/proc/cmdline": "rd.immucore.uki"})
		Expect(err).ShouldNot(HaveOccurred())
		defer cleanup()
		Expect(fsutils.MkdirAll(fs, cnst.OEMPath, os.ModeDir|os.ModePerm)).To(Succeed())

		memLog := &bytes.Buffer{}
		log := sdkLogger.NewBufferLogger(memLog)
		log.SetLevel("debug")
		cfg := config.NewConfig(config.WithFs(fs), config.WithLogger(log))
		cfg.Collector = collector.Config{}
		cfg.Install.GrubOptions = map[string]string{"extra_cmdline": "console=ttyS0"}

		Expect(hook.GrubPostInstallOptions{}.Run(*cfg, nil)).To(Succeed())

		_, err = fs.Stat(filepath.Join(cnst.OEMPath, cnst.GrubEnv))
		Expect(os.IsNotExist(err)).To(BeTrue())
		Expect(memLog.String()).To(ContainSubstring("Skipping GrubPostInstallOptions hook in uki mode"))
	})
})
