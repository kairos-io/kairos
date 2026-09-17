/*
Copyright © 2026 SUSE LLC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package action_test

import (
	"bytes"
	"path/filepath"

	"github.com/kairos-io/kairos/v4/agent/pkg/action"
	agentConfig "github.com/kairos-io/kairos/v4/agent/pkg/config"
	"github.com/kairos-io/kairos/v4/agent/pkg/constants"
	v1 "github.com/kairos-io/kairos/v4/agent/pkg/implementations/spec"
	fsutils "github.com/kairos-io/kairos/v4/agent/pkg/utils/fs"
	v1mock "github.com/kairos-io/kairos/v4/agent/tests/mocks"
	ghwMock "github.com/kairos-io/kairos/v4/sdk/ghw/mocks"
	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
	sdkImages "github.com/kairos-io/kairos/v4/sdk/types/images"
	sdkLogger "github.com/kairos-io/kairos/v4/sdk/types/logger"
	sdkPartitions "github.com/kairos-io/kairos/v4/sdk/types/partitions"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/twpayne/go-vfs/v5"
	"github.com/twpayne/go-vfs/v5/vfst"
)

var _ = Describe("FinalizeContext", func() {
	It("round-trips through JSON on disk", func() {
		fs, cleanup, err := vfst.NewTestFS(nil)
		Expect(err).NotTo(HaveOccurred())
		defer cleanup()

		src := action.FinalizeContext{
			ImgMountPoint:        "/",
			TransitionImgFile:    "/host/run/initramfs/cos-state/cOS/transition.img",
			TransitionImgFS:      "ext4",
			RecoveryUpgrade:      false,
			GrubDefEntry:         "cos",
			ExtraDirsRootfs:      []string{"/var/lib/kubelet"},
			StateMountPoint:      "/host/run/initramfs/cos-state",
			StateFSLabel:         "COS_STATE",
			ActiveImgFile:        "/host/run/initramfs/cos-state/cOS/active.img",
			OEMMountPoint:        "/host/oem",
			PersistentMountPoint: "/host/usr/local",
			EFIPartition: &action.SerializedPartition{
				Path:            "/dev/sda1",
				MountPoint:      "/host/boot/efi",
				FS:              "vfat",
				FilesystemLabel: "COS_GRUB",
			},
			Arch: "amd64",
		}

		path := "/ctx.json"
		Expect(action.WriteFinalizeContext(fs, path, src)).To(Succeed())

		got, err := action.ReadFinalizeContext(fs, path)
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal(src))
	})

	It("rewrites host-side paths under the handoff prefix", func() {
		spec := &v1.UpgradeSpec{
			Entry:        "",
			GrubDefEntry: "cos",
			Partitions: sdkPartitions.ElementalPartitions{
				State:      &sdkPartitions.Partition{MountPoint: "/run/initramfs/cos-state", FilesystemLabel: "COS_STATE"},
				OEM:        &sdkPartitions.Partition{MountPoint: "/oem"},
				Persistent: &sdkPartitions.Partition{MountPoint: "/usr/local"},
				EFI:        &sdkPartitions.Partition{MountPoint: "/boot/efi", FS: "vfat"},
			},
		}
		upgradeImg := &sdkImages.Image{
			MountPoint: "/tmp/upgrade-mnt",
			File:       "/run/initramfs/cos-state/cOS/transition.img",
			FS:         "ext4",
		}

		ctx := action.NewFinalizeContextForHandoff(spec, upgradeImg, constants.HandoffHostPrefix)

		Expect(ctx.ImgMountPoint).To(Equal("/"))
		Expect(ctx.TransitionImgFile).To(Equal("/host/run/initramfs/cos-state/cOS/transition.img"))
		Expect(ctx.StateMountPoint).To(Equal("/host/run/initramfs/cos-state"))
		Expect(ctx.ActiveImgFile).To(Equal("/host/run/initramfs/cos-state/cOS/active.img"))
		Expect(ctx.OEMMountPoint).To(Equal("/host/oem"))
		Expect(ctx.PersistentMountPoint).To(Equal("/host/usr/local"))
		Expect(ctx.EFIPartition).NotTo(BeNil())
		Expect(ctx.EFIPartition.MountPoint).To(Equal("/host/boot/efi"))
	})

	It("omits empty optional fields in the marshaled JSON", func() {
		fs, cleanup, err := vfst.NewTestFS(nil)
		Expect(err).NotTo(HaveOccurred())
		defer cleanup()

		src := action.FinalizeContext{
			ImgMountPoint:     "/",
			TransitionImgFile: "/x",
			TransitionImgFS:   "ext4",
			StateMountPoint:   "/host/state",
			RecoveryUpgrade:   true,
		}
		path := "/ctx.json"
		Expect(action.WriteFinalizeContext(fs, path, src)).To(Succeed())

		raw, err := fs.ReadFile(path)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(raw)).NotTo(ContainSubstring("grubDefEntry"))
		Expect(string(raw)).NotTo(ContainSubstring("efiPartition"))
	})
})

var _ = Describe("Upgrade finalize handoff", Label("upgrade", "handoff"), func() {
	var (
		config    *sdkConfig.Config
		runner    *v1mock.FakeRunner
		fs        vfs.FS
		mounter   *v1mock.ErrorMounter
		cleanup   func()
		ghwTest   ghwMock.GhwMock
		extractor *v1mock.FakeImageExtractor
		spec      *v1.UpgradeSpec
		upgrade   *action.UpgradeAction
	)

	const (
		activeImg  = "/run/initramfs/cos-state/cOS/active.img"
		passiveImg = "/run/initramfs/cos-state/cOS/passive.img"
	)

	BeforeEach(func() {
		runner = v1mock.NewFakeRunner()
		mounter = v1mock.NewErrorMounter()
		var err error
		fs, cleanup, err = vfst.NewTestFS(map[string]interface{}{
			"/dev/loop-control": "",
			"/dev/loop0":        "",
		})
		Expect(err).ShouldNot(HaveOccurred())

		memLog := &bytes.Buffer{}
		logger := sdkLogger.NewBufferLogger(memLog)
		logger.SetLevel("debug")
		extractor = v1mock.NewFakeImageExtractor(logger)
		config = agentConfig.NewConfig(
			agentConfig.WithFs(fs),
			agentConfig.WithRunner(runner),
			agentConfig.WithLogger(logger),
			agentConfig.WithMounter(mounter),
			agentConfig.WithSyscall(&v1mock.FakeSyscall{}),
			agentConfig.WithClient(&v1mock.FakeHTTPClient{}),
			agentConfig.WithCloudInitRunner(&v1mock.FakeCloudInitRunner{}),
			agentConfig.WithImageExtractor(extractor),
			agentConfig.WithPlatform("linux/amd64"),
		)

		fsutils.MkdirAll(fs, "/run/initramfs/cos-state/cOS", constants.DirPerm)
		fsutils.MkdirAll(fs, "/run/initramfs/live/cOS", constants.DirPerm)

		ghwTest = ghwMock.GhwMock{}
		ghwTest.AddDisk(sdkPartitions.Disk{
			Name: "device",
			Partitions: []*sdkPartitions.Partition{
				{Name: "device1", FilesystemLabel: "COS_GRUB", FS: "ext4"},
				{Name: "device2", FilesystemLabel: "COS_STATE", FS: "ext4", MountPoint: "/run/initramfs/cos-state"},
				{Name: "loop0", FilesystemLabel: "COS_ACTIVE", FS: "ext4"},
				{Name: "device5", FilesystemLabel: "COS_RECOVERY", FS: "ext4", MountPoint: "/run/initramfs/live"},
				{Name: "device6", FilesystemLabel: "COS_OEM", FS: "ext4"},
			},
		})
		ghwTest.CreateDevices()

		spec, err = agentConfig.NewUpgradeSpec(config)
		Expect(err).ShouldNot(HaveOccurred())
		spec.Active.Size = 10
		spec.Passive.Size = 10
		spec.Recovery.Size = 10
		spec.Active.Source = sdkImages.NewDockerSrc("alpine")

		Expect(fsutils.MkdirAll(fs, filepath.Join(spec.Active.MountPoint, "etc"), constants.DirPerm)).To(Succeed())
		Expect(fsutils.MkdirAll(fs, "/proc", constants.DirPerm)).To(Succeed())
		Expect(fs.WriteFile("/proc/cmdline", []byte(constants.ActiveLabel), constants.FilePerm)).To(Succeed())
		Expect(fs.WriteFile(filepath.Join(spec.Active.MountPoint, "etc", "kairos-release"), []byte("GRUB_ENTRY_NAME=TESTOS"), constants.FilePerm)).To(Succeed())
		Expect(fs.WriteFile(activeImg, []byte("active"), constants.FilePerm)).To(Succeed())
		Expect(fs.WriteFile(passiveImg, []byte("passive"), constants.FilePerm)).To(Succeed())
		mounter.Mount("device2", constants.RunningStateDir, "auto", []string{"ro"})

		runner.SideEffect = func(command string, args ...string) ([]byte, error) {
			if command == "mv" && len(args) == 3 && args[0] == "-f" && args[1] == activeImg && args[2] == passiveImg {
				source, _ := fs.ReadFile(activeImg)
				_ = fs.WriteFile(passiveImg, source, constants.FilePerm)
				_ = fs.RemoveAll(activeImg)
			}
			if command == "mv" && len(args) == 3 && args[0] == "-f" && args[1] == spec.Active.File && args[2] == activeImg {
				source, _ := fs.ReadFile(spec.Active.File)
				_ = fs.WriteFile(activeImg, source, constants.FilePerm)
				_ = fs.RemoveAll(spec.Active.File)
			}
			return []byte{}, nil
		}

		// Extractor writes the target rootfs when the image is deployed;
		// stage the capability marker and the target binary inside it so
		// canHandoffToTarget picks the handoff path.
		extractor.SideEffect = func(imageRef, destination, platformRef string) error {
			for _, dir := range []string{"usr/bin", "etc/kairos/capabilities"} {
				if err := fsutils.MkdirAll(fs, filepath.Join(destination, dir), constants.DirPerm); err != nil {
					return err
				}
			}
			if err := fs.WriteFile(filepath.Join(destination, "usr/bin/kairos-agent"), []byte("fake-agent"), 0o755); err != nil {
				return err
			}
			if err := fs.WriteFile(filepath.Join(destination, "etc/kairos/capabilities/upgrade-finalize"), []byte{}, 0o644); err != nil {
				return err
			}
			return nil
		}
	})

	AfterEach(func() {
		ghwTest.Clean()
		cleanup()
	})

	It("execs the target's kairos-agent upgrade-finalize when the capability marker is present", func() {
		upgrade = action.NewUpgradeAction(config, spec)
		Expect(upgrade.Run()).ToNot(HaveOccurred())

		// Diagnostic: verify the marker file was actually written by the
		// extractor at the location canHandoffToTarget looks at, so a
		// failure here is not a silent MountPoint mismatch.
		mnt := spec.Active.MountPoint
		markerPath := filepath.Join(mnt, constants.UpgradeFinalizeCapabilityMarker)
		_, statErr := fs.Stat(markerPath)
		Expect(statErr).NotTo(HaveOccurred(), "expected marker at %s", markerPath)

		Expect(runner.IncludesCmds([][]string{
			{constants.TargetKairosAgentPath, "upgrade-finalize", "--context-file", constants.UpgradeFinalizeContextPath},
		})).To(Succeed(), "expected the target agent to be invoked with upgrade-finalize")
	})

	It("falls back to inline finalize when the capability marker is absent", func() {
		// Rewire the extractor to install a target binary without the
		// capability marker; the host must then run the finalize inline.
		extractor.SideEffect = func(imageRef, destination, platformRef string) error {
			if err := fsutils.MkdirAll(fs, filepath.Join(destination, "usr/bin"), constants.DirPerm); err != nil {
				return err
			}
			return fs.WriteFile(filepath.Join(destination, "usr/bin/kairos-agent"), []byte("fake-agent"), 0o755)
		}

		upgrade = action.NewUpgradeAction(config, spec)
		Expect(upgrade.Run()).ToNot(HaveOccurred())

		Expect(runner.IncludesCmds([][]string{
			{constants.TargetKairosAgentPath},
		})).ToNot(Succeed(), "did not expect the target agent to be invoked in fallback mode")
	})
})
