package hook_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	hook "github.com/kairos-io/kairos/v4/agent/internal/agent/hooks"
	"github.com/kairos-io/kairos/v4/agent/pkg/config"
	cnst "github.com/kairos-io/kairos/v4/agent/pkg/constants"
	"github.com/kairos-io/kairos/v4/sdk/collector"
	ghwMock "github.com/kairos-io/kairos/v4/sdk/ghw/mocks"
	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
	sdkFs "github.com/kairos-io/kairos/v4/sdk/types/fs"
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

var _ = Describe("GrubFirstBootOptions", func() {
	var cfg *sdkConfig.Config
	var fs vfs.FS
	var stateFS *roStateFS
	var mounter *remountMounter
	var memLog *bytes.Buffer
	var ghwTest ghwMock.GhwMock
	var cleanup func()
	var err error

	// runWithOEM presents ghw with a single disk holding the given COS_OEM
	// partition and runs the hook. A nil partition is a node where the label
	// is nowhere to be found.
	runWithOEM := func(oem *sdkPartitions.Partition) error {
		parts := []*sdkPartitions.Partition{
			{Name: "vda1", Path: "/dev/vda1", FilesystemLabel: cnst.StateLabel, FS: "ext4"},
		}
		if oem != nil {
			parts = append(parts, oem)
		}
		ghwTest = ghwMock.GhwMock{}
		ghwTest.AddDisk(sdkPartitions.Disk{Name: "vda", Partitions: parts})
		ghwTest.CreateDevices()
		DeferCleanup(ghwTest.Clean)
		return hook.GrubFirstBootOptions{}.Run(*cfg, nil)
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

	It("keeps writing to OEM and says so when COS_OEM cannot be classified", func() {
		// No COS_OEM on any disk. Moving the file on a guess is worse than
		// the behaviour this hook has always had, but the log has to name
		// the risk, because on an encrypted node the options are lost.
		Expect(runWithOEM(nil)).Should(Succeed())

		Expect(grubenv(cnst.OEMPath)).To(ContainSubstring("default_menu_entry=My Kairos"))
		Expect(memLog.String()).To(ContainSubstring("Could not determine whether COS_OEM is encrypted"))
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
})
