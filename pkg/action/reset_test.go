/*
   Copyright © 2022 SUSE LLC

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package action_test

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/kairos-io/kairos-agent/v2/pkg/action"
	agentConfig "github.com/kairos-io/kairos-agent/v2/pkg/config"
	"github.com/kairos-io/kairos-agent/v2/pkg/constants"
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/implementations/spec"
	fsutils "github.com/kairos-io/kairos-agent/v2/pkg/utils/fs"
	v1mock "github.com/kairos-io/kairos-agent/v2/tests/mocks"
	ghwMock "github.com/kairos-io/kairos-sdk/ghw/mocks"
	sdkConfig "github.com/kairos-io/kairos-sdk/types/config"
	sdkImages "github.com/kairos-io/kairos-sdk/types/images"
	sdkLogger "github.com/kairos-io/kairos-sdk/types/logger"
	sdkPartitions "github.com/kairos-io/kairos-sdk/types/partitions"

	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/twpayne/go-vfs/v5"
	"github.com/twpayne/go-vfs/v5/vfst"
)

var _ = Describe("Reset action tests", func() {
	var config *sdkConfig.Config
	var runner *v1mock.FakeRunner
	var fs vfs.FS
	var logger sdkLogger.KairosLogger
	var mounter *v1mock.ErrorMounter
	var syscall *v1mock.FakeSyscall
	var client *v1mock.FakeHTTPClient
	var cloudInit *v1mock.FakeCloudInitRunner
	var cleanup func()
	var memLog *bytes.Buffer
	var ghwTest ghwMock.GhwMock
	var extractor *v1mock.FakeImageExtractor

	BeforeEach(func() {
		runner = v1mock.NewFakeRunner()
		syscall = &v1mock.FakeSyscall{}
		mounter = v1mock.NewErrorMounter()
		client = &v1mock.FakeHTTPClient{}
		memLog = &bytes.Buffer{}
		logger = sdkLogger.NewBufferLogger(memLog)
		extractor = v1mock.NewFakeImageExtractor(logger)
		var err error
		fs, cleanup, err = vfst.NewTestFS(map[string]interface{}{
			"/dev/loop-control": "",
			"/dev/loop0":        "",
		})
		Expect(err).Should(BeNil())

		cloudInit = &v1mock.FakeCloudInitRunner{}
		config = agentConfig.NewConfig(
			agentConfig.WithFs(fs),
			agentConfig.WithRunner(runner),
			agentConfig.WithLogger(logger),
			agentConfig.WithMounter(mounter),
			agentConfig.WithSyscall(syscall),
			agentConfig.WithClient(client),
			agentConfig.WithCloudInitRunner(cloudInit),
			agentConfig.WithImageExtractor(extractor),
		)
	})

	AfterEach(func() { cleanup() })

	Describe("Reset Action", Label("reset"), func() {
		var spec *v1.ResetSpec
		var reset *action.ResetAction
		var cmdFail, bootedFrom string
		var err error
		BeforeEach(func() {
			Expect(err).ShouldNot(HaveOccurred())
			cmdFail = ""
			recoveryImg := filepath.Join(constants.RunningStateDir, "cOS", constants.RecoveryImgFile)
			err = fsutils.MkdirAll(fs, filepath.Dir(recoveryImg), constants.DirPerm)
			Expect(err).To(BeNil())
			_, err = fs.Create(recoveryImg)
			Expect(err).To(BeNil())

			mainDisk := sdkPartitions.Disk{
				Name: "device",
				Partitions: []*sdkPartitions.Partition{
					{
						Name:            "device1",
						FilesystemLabel: "COS_GRUB",
						FS:              "ext4",
					},
					{
						Name:            "device2",
						FilesystemLabel: "COS_OEM",
						FS:              "ext4",
						MountPoint:      "/oem",
					},
					{
						Name:            "device3",
						FilesystemLabel: "COS_RECOVERY",
						FS:              "ext4",
						MountPoint:      "/run/initramfs/cos-state",
					},
					{
						Name:            "device4",
						FilesystemLabel: "COS_STATE",
						FS:              "ext4",
					},
					{
						Name:            "device5",
						FilesystemLabel: "COS_PERSISTENT",
						FS:              "ext4",
					},
				},
			}
			ghwTest = ghwMock.GhwMock{}
			ghwTest.AddDisk(mainDisk)
			ghwTest.CreateDevices()

			Expect(fsutils.MkdirAll(fs, constants.EfiDevice, constants.DirPerm)).ToNot(HaveOccurred())

			bootedFrom = constants.SystemLabel
			runner.SideEffect = func(cmd string, args ...string) ([]byte, error) {
				if cmd == cmdFail {
					return []byte{}, errors.New("Command failed")
				}
				switch cmd {
				case "cat":
					return []byte(bootedFrom), nil
				default:
					return []byte{}, nil
				}
			}

			spec, err = agentConfig.NewResetSpec(config)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(spec.Active.Source.IsEmpty()).To(BeFalse())

			spec.Active.Size = 16

			grubCfg := filepath.Join(spec.Active.MountPoint, spec.GrubConf)
			err = fsutils.MkdirAll(fs, filepath.Dir(grubCfg), constants.DirPerm)
			Expect(err).To(BeNil())
			_, err = fs.Create(grubCfg)
			Expect(err).To(BeNil())

			// create the fake grub dir with modules, it needs an arch in the path
			Expect(fsutils.MkdirAll(fs, filepath.Join(spec.Active.MountPoint, constants.Archx86), constants.DirPerm)).ToNot(HaveOccurred())
			for _, mod := range constants.GetGrubModules() {
				_, err = fs.Create(filepath.Join(spec.Active.MountPoint, constants.Archx86, mod))
				Expect(err).ToNot(HaveOccurred())
			}

			// create fake shim
			Expect(fsutils.MkdirAll(fs, filepath.Join(spec.Active.MountPoint, "/usr/share/efi/x86_64/"), constants.DirPerm)).ToNot(HaveOccurred())
			_, err = fs.Create(filepath.Join(spec.Active.MountPoint, "/usr/share/efi/x86_64/", "shim.efi"))
			Expect(err).ToNot(HaveOccurred())

			// create fake grub
			Expect(fsutils.MkdirAll(fs, filepath.Join(spec.Active.MountPoint, "/usr/share/efi/x86_64/"), constants.DirPerm)).ToNot(HaveOccurred())
			_, err = fs.Create(filepath.Join(spec.Active.MountPoint, "/usr/share/efi/x86_64/", "grub.efi"))
			Expect(err).ToNot(HaveOccurred())

			reset = action.NewResetAction(config, spec)
		})

		AfterEach(func() {
			ghwTest.Clean()
		})
		Describe("With EFI", func() {
			It("Successfully resets on non-squashfs recovery", func() {
				Expect(reset.Run()).To(BeNil())
			})
			It("Successfully resets on non-squashfs recovery including persistent data", func() {
				spec.FormatPersistent = true
				spec.FormatOEM = true
				Expect(reset.Run()).To(BeNil())
			})
			It("Successfully resets from a squashfs recovery image", Label("channel"), func() {
				err := fsutils.MkdirAll(config.Fs, constants.IsoBaseTree, constants.DirPerm)
				Expect(err).ShouldNot(HaveOccurred())
				spec.Active.Source = sdkImages.NewDirSrc(constants.IsoBaseTree)
				Expect(reset.Run()).To(BeNil())
			})
			It("Successfully resets despite having errors on hooks", func() {
				cloudInit.Error = true
				Expect(reset.Run()).To(BeNil())
			})
			It("Successfully resets from a docker image", Label("docker"), func() {
				spec.Active.Source = sdkImages.NewDockerSrc("my/image:latest")
				Expect(reset.Run()).To(BeNil())

			})
			It("Fails formatting state partition", func() {
				cmdFail = "mkfs.ext4"
				Expect(reset.Run()).NotTo(BeNil())
				Expect(runner.IncludesCmds([][]string{{"mkfs.ext4"}}))
			})
			It("Fails setting the active label on non-squashfs recovery", func() {
				cmdFail = "tune2fs"
				Expect(reset.Run()).NotTo(BeNil())
			})
			It("Fails setting the passive label on squashfs recovery", func() {
				cmdFail = "tune2fs"
				Expect(reset.Run()).NotTo(BeNil())
				Expect(runner.IncludesCmds([][]string{{"tune2fs"}}))
			})
			It("Fails mounting partitions", func() {
				mounter.ErrorOnMount = true
				Expect(reset.Run()).NotTo(BeNil())
			})
			It("Fails unmounting partitions", func() {
				mounter.ErrorOnUnmount = true
				Expect(reset.Run()).NotTo(BeNil())
			})
			It("Fails unpacking docker image ", func() {
				spec.Active.Source = sdkImages.NewDockerSrc("my/image:latest")
				extractor.SideEffect = func(imageRef, destination, platformRef string) error {
					return fmt.Errorf("error")
				}
				Expect(reset.Run()).NotTo(BeNil())
			})
			It("Fails if some hook fails and strict is set", func() {
				config.Strict = true
				cloudInit.Error = true
				Expect(reset.Run()).NotTo(BeNil())
			})
			It("Fails formatting the persistent partition", func() {
				spec.FormatPersistent = true
				cmdFail = "mkfs.ext4"
				Expect(reset.Run()).NotTo(BeNil())
				Expect(runner.IncludesCmds([][]string{{"mkfs.ext4"}}))
			})
			It("Fails unmounting the persistent partition", func() {
				spec.FormatPersistent = true
				spec.Partitions.Persistent.MountPoint = "/usr/local"
				Expect(mounter.Mount("/dev/device5", "/usr/local", "auto", []string{"rw"})).To(Succeed())
				mounter.ErrorOnUnmount = true
				Expect(reset.Run()).NotTo(BeNil())
			})
			It("Fails formatting the OEM partition", func() {
				spec.FormatOEM = true
				spec.FormatPersistent = false
				cmdFail = "mkfs.ext4"
				Expect(reset.Run()).NotTo(BeNil())
				Expect(runner.IncludesCmds([][]string{{"mkfs.ext4"}}))
			})
			It("Fails unmounting the OEM partition", func() {
				spec.FormatOEM = true
				Expect(mounter.Mount("/dev/device2", "/oem", "auto", []string{"rw"})).To(Succeed())
				mounter.ErrorOnUnmount = true
				Expect(reset.Run()).NotTo(BeNil())
			})
			It("Fails mounting back the OEM partition", func() {
				spec.FormatOEM = true
				mounter.ErrorOnMount = true
				Expect(reset.Run()).NotTo(BeNil())
			})
			It("Fails installing grub", func() {
				err := config.Fs.Remove(filepath.Join(spec.Active.MountPoint, "/usr/share/efi/x86_64/", "shim.efi"))
				Expect(err).ToNot(HaveOccurred())
				Expect(reset.Run()).NotTo(BeNil())
			})
			It("Successfully resets with mounted persistent and OEM partitions", func() {
				// Do not format persistent so it stays mounted during the reset
				spec.FormatPersistent = false
				spec.Partitions.Persistent.MountPoint = "/usr/local"
				Expect(mounter.Mount("/dev/device5", "/usr/local", "auto", []string{"rw"})).To(Succeed())
				Expect(mounter.Mount("/dev/device2", "/oem", "auto", []string{"rw"})).To(Succeed())
				Expect(reset.Run()).To(BeNil())
			})
			It("Fails on the after-reset-chroot hook when strict", func() {
				// Stages read the cmdline, so it needs to exist for the other stages to pass
				_ = fsutils.MkdirAll(fs, "/proc", constants.DirPerm)
				Expect(fs.WriteFile("/proc/cmdline", []byte("foo"), constants.FilePerm)).ToNot(HaveOccurred())
				config.Strict = true
				config.CloudInitRunner = &stageFailCloudInitRunner{failStage: constants.AfterResetChrootHook}
				Expect(reset.Run()).NotTo(BeNil())
			})
			It("Fails on the after-reset hook when strict", func() {
				// Stages read the cmdline, so it needs to exist for the other stages to pass
				_ = fsutils.MkdirAll(fs, "/proc", constants.DirPerm)
				Expect(fs.WriteFile("/proc/cmdline", []byte("foo"), constants.FilePerm)).ToNot(HaveOccurred())
				config.Strict = true
				config.CloudInitRunner = &stageFailCloudInitRunner{failStage: constants.AfterResetHook}
				Expect(reset.Run()).NotTo(BeNil())
			})
			It("Fails to set the default grub entry", func() {
				// Make the grub env file a directory so writing the default entry fails
				Expect(fsutils.MkdirAll(fs, filepath.Join(spec.Partitions.State.MountPoint, constants.GrubOEMEnv), constants.DirPerm)).ToNot(HaveOccurred())
				Expect(reset.Run()).NotTo(BeNil())
			})
		})
	})
})
