package hook_test

import (
	"bytes"
	"errors"
	"path/filepath"

	hook "github.com/kairos-io/kairos/v4/agent/internal/agent/hooks"
	"github.com/kairos-io/kairos/v4/agent/pkg/config"
	cnst "github.com/kairos-io/kairos/v4/agent/pkg/constants"
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
		for _, action := range mounter.GetLog() {
			Expect(action.Action).ToNot(Equal(mount.FakeActionUnmount),
				"nothing this hook did not mount may be unmounted, actions: %v", mounter.GetLog())
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

	It("fails instead of writing the options outside the OEM partition", func() {
		// Without the mount the write goes to the ephemeral root, where GRUB
		// never looks, and the old code reported success for it.
		withOEM()
		cfg.Mounter = failingMountMounter{Interface: mounter}

		Expect(hook.GrubFirstBootOptions{}.Run(*cfg, nil)).ShouldNot(Succeed())

		_, readErr := grubenv()
		Expect(readErr).To(HaveOccurred(),
			"nothing may be written when the partition is not there")
	})
})
