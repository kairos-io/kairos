package hook_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"

	hook "github.com/kairos-io/kairos/v4/agent/internal/agent/hooks"
	"github.com/kairos-io/kairos/v4/agent/pkg/config"
	cnst "github.com/kairos-io/kairos/v4/agent/pkg/constants"
	fsutils "github.com/kairos-io/kairos/v4/agent/pkg/utils/fs"
	"github.com/kairos-io/kairos/v4/sdk/collector"
	ghwMock "github.com/kairos-io/kairos/v4/sdk/ghw/mocks"
	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
	install "github.com/kairos-io/kairos/v4/sdk/types/install"
	sdkLogger "github.com/kairos-io/kairos/v4/sdk/types/logger"
	sdkPartitions "github.com/kairos-io/kairos/v4/sdk/types/partitions"

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
