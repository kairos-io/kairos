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
	"fmt"
	"os"
	"path/filepath"

	"github.com/kairos-io/kairos-agent/v2/pkg/action"
	agentConfig "github.com/kairos-io/kairos-agent/v2/pkg/config"
	"github.com/kairos-io/kairos-agent/v2/pkg/constants"
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/implementations/spec"
	"github.com/kairos-io/kairos-agent/v2/pkg/utils"
	fsutils "github.com/kairos-io/kairos-agent/v2/pkg/utils/fs"
	v1mock "github.com/kairos-io/kairos-agent/v2/tests/mocks"
	ghwMock "github.com/kairos-io/kairos-sdk/ghw/mocks"
	sdkConfig "github.com/kairos-io/kairos-sdk/types/config"
	sdkImages "github.com/kairos-io/kairos-sdk/types/images"
	sdkLogger "github.com/kairos-io/kairos-sdk/types/logger"
	sdkPartitions "github.com/kairos-io/kairos-sdk/types/partitions"
	"github.com/mudler/yip/pkg/schema"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/twpayne/go-vfs/v5"
	"github.com/twpayne/go-vfs/v5/vfst"
)

// stageFailCloudInitRunner is a CloudInitRunner that only fails for a given stage,
// so hook error paths beyond the first hook can be exercised.
type stageFailCloudInitRunner struct {
	failStage string
}

func (c *stageFailCloudInitRunner) Run(stage string, args ...string) error {
	if stage == c.failStage {
		return fmt.Errorf("failure on stage %s", stage)
	}
	return nil
}

func (c *stageFailCloudInitRunner) SetModifier(_ schema.Modifier) {}

func (c *stageFailCloudInitRunner) Analyze(_ string, _ ...string) {}

// failingMountSyscall fails all syscall mount calls, used to trigger cleanup errors
type failingMountSyscall struct {
	*v1mock.FakeSyscall
}

func (f *failingMountSyscall) Mount(_ string, _ string, _ string, _ uintptr, _ string) error {
	return fmt.Errorf("syscall mount failure")
}

var _ = Describe("Upgrade Actions test", func() {
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
	var dummySourceFile string

	BeforeEach(func() {
		runner = v1mock.NewFakeRunner()
		syscall = &v1mock.FakeSyscall{}
		mounter = v1mock.NewErrorMounter()
		client = &v1mock.FakeHTTPClient{}
		memLog = &bytes.Buffer{}
		logger = sdkLogger.NewBufferLogger(memLog)
		logger.SetLevel("debug")
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
			agentConfig.WithPlatform("linux/amd64"),
		)
	})

	AfterEach(func() {
		cleanup()
		os.RemoveAll(dummySourceFile)
	})

	Describe("Upgrade Action", Label("upgrade"), func() {
		var spec *v1.UpgradeSpec
		var upgrade *action.UpgradeAction
		var memLog *bytes.Buffer
		activeImg := fmt.Sprintf("%s/cOS/%s", constants.RunningStateDir, constants.ActiveImgFile)
		passiveImg := fmt.Sprintf("%s/cOS/%s", constants.RunningStateDir, constants.PassiveImgFile)
		recoveryImgSquash := fmt.Sprintf("%s/cOS/%s", constants.LiveDir, constants.RecoverySquashFile)
		recoveryImg := fmt.Sprintf("%s/cOS/%s", constants.LiveDir, constants.RecoveryImgFile)

		BeforeEach(func() {
			memLog = &bytes.Buffer{}
			logger = sdkLogger.NewBufferLogger(memLog)
			extractor = v1mock.NewFakeImageExtractor(logger)
			config.Logger = logger
			config.ImageExtractor = extractor
			logger.SetLevel("debug")

			// Create paths used by tests
			fsutils.MkdirAll(fs, fmt.Sprintf("%s/cOS", constants.RunningStateDir), constants.DirPerm)
			fsutils.MkdirAll(fs, fmt.Sprintf("%s/cOS", constants.LiveDir), constants.DirPerm)

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
						FilesystemLabel: "COS_STATE",
						FS:              "ext4",
						MountPoint:      constants.RunningStateDir,
					},
					{
						Name:            "loop0",
						FilesystemLabel: "COS_ACTIVE",
						FS:              "ext4",
					},
					{
						Name:            "device5",
						FilesystemLabel: "COS_RECOVERY",
						FS:              "ext4",
						MountPoint:      constants.LiveDir,
					},
					{
						Name:            "device6",
						FilesystemLabel: "COS_OEM",
						FS:              "ext4",
					},
				},
			}
			ghwTest = ghwMock.GhwMock{}
			ghwTest.AddDisk(mainDisk)
			ghwTest.CreateDevices()
		})
		AfterEach(func() {
			ghwTest.Clean()
		})
		Describe(fmt.Sprintf("Booting from %s", constants.ActiveLabel), Label("active_label"), func() {
			var err error
			BeforeEach(func() {
				spec, err = agentConfig.NewUpgradeSpec(config)
				Expect(err).ShouldNot(HaveOccurred())

				err = fsutils.MkdirAll(config.Fs, filepath.Join(spec.Active.MountPoint, "etc"), constants.DirPerm)
				Expect(err).ShouldNot(HaveOccurred())

				err = fsutils.MkdirAll(config.Fs, "/proc", constants.DirPerm)
				Expect(err).ShouldNot(HaveOccurred())

				// Write proc/cmdline so we can detect what we booted from
				err = fs.WriteFile("/proc/cmdline", []byte(constants.ActiveLabel), constants.FilePerm)
				Expect(err).ShouldNot(HaveOccurred())

				err = fs.WriteFile(
					filepath.Join(spec.Active.MountPoint, "etc", "kairos-release"),
					[]byte("GRUB_ENTRY_NAME=TESTOS"),
					constants.FilePerm,
				)
				Expect(err).ShouldNot(HaveOccurred())

				spec.Active.Size = 10
				spec.Passive.Size = 10
				spec.Recovery.Size = 10

				runner.SideEffect = func(command string, args ...string) ([]byte, error) {
					if command == "mv" && args[0] == "-f" && args[1] == activeImg && args[2] == passiveImg {
						// we doing backup, do the "move"
						source, _ := fs.ReadFile(activeImg)
						_ = fs.WriteFile(passiveImg, source, constants.FilePerm)
						_ = fs.RemoveAll(activeImg)
					}
					if command == "mv" && args[0] == "-f" && args[1] == spec.Active.File && args[2] == activeImg {
						// we doing the image substitution, do the "move"
						source, _ := fs.ReadFile(spec.Active.File)
						_ = fs.WriteFile(activeImg, source, constants.FilePerm)
						_ = fs.RemoveAll(spec.Active.File)
					}
					return []byte{}, nil
				}
				config.Runner = runner
				// Create fake active/passive files
				_ = fs.WriteFile(activeImg, []byte("active"), constants.FilePerm)
				_ = fs.WriteFile(passiveImg, []byte("passive"), constants.FilePerm)
				// Mount state partition as it is expected to be mounted when booting from active
				mounter.Mount("device2", constants.RunningStateDir, "auto", []string{"ro"})
			})
			AfterEach(func() {
				_ = fs.RemoveAll(activeImg)
				_ = fs.RemoveAll(passiveImg)
				mounter.Unmount("device2")
			})
			It("Fails if some hook fails and strict is set", func() {
				runner.SideEffect = func(command string, args ...string) ([]byte, error) {
					return []byte{}, nil
				}
				config.Strict = true
				cloudInit.Error = true
				upgrade = action.NewUpgradeAction(config, spec)
				err := upgrade.Run()
				Expect(err).To(HaveOccurred())
				// Make sure is a cloud init error!
				Expect(err.Error()).To(ContainSubstring("cloud init"))
			})
			It("Successfully upgrades from docker image", Label("docker"), func() {
				spec.Active.Source = sdkImages.NewDockerSrc("alpine")
				upgrade = action.NewUpgradeAction(config, spec)
				err := upgrade.Run()
				Expect(err).ToNot(HaveOccurred())

				// Check that the rebrand worked with our kairos-release value
				Expect(memLog).To(ContainSubstring("Setting default grub entry to TESTOS"), memLog.String())

				// This should be the new image
				info, err := fs.Stat(activeImg)
				Expect(err).ToNot(HaveOccurred())
				// Image size should be the config.ImgSize as its truncated from the upgrade
				Expect(info.Size()).To(BeNumerically("==", int64(spec.Active.Size*1024*1024)))
				Expect(info.IsDir()).To(BeFalse())

				// Should have backed up active to passive
				info, err = fs.Stat(passiveImg)
				Expect(err).ToNot(HaveOccurred())
				// Should be a tiny image as it should only contain our text
				// As this was generated by us at the start test and moved by the upgrade from active.iomg
				Expect(info.Size()).To(BeNumerically(">", 0))
				Expect(info.Size()).To(BeNumerically("<", int64(spec.Active.Size*1024*1024)))
				f, _ := fs.ReadFile(passiveImg)
				// This should be a backup so it should read active
				Expect(f).To(ContainSubstring("active"))

				// Expect transition image to be gone
				_, err = fs.Stat(spec.Active.File)
				Expect(err).To(HaveOccurred())
			})
			It("Successfully reboots after upgrade from docker image", Label("docker"), func() {
				spec.Active.Source = sdkImages.NewDockerSrc("alpine")
				upgrade = action.NewUpgradeAction(config, spec)
				By("Upgrading")
				err := upgrade.Run()
				Expect(err).ToNot(HaveOccurred())
				By("Checking the log")
				// Check that the rebrand worked with our kairos-release value
				Expect(memLog).To(ContainSubstring("Setting default grub entry to TESTOS"))

				By("checking active image")
				// This should be the new image
				info, err := fs.Stat(activeImg)
				Expect(err).ToNot(HaveOccurred())
				// Image size should be the config.ImgSize as its truncated from the upgrade
				Expect(info.Size()).To(BeNumerically("==", int64(spec.Active.Size*1024*1024)))
				Expect(info.IsDir()).To(BeFalse())

				By("Checking passive image")
				// Should have backed up active to passive
				info, err = fs.Stat(passiveImg)
				Expect(err).ToNot(HaveOccurred())
				// Should be a tiny image as it should only contain our text
				// As this was generated by us at the start test and moved by the upgrade from active.iomg
				Expect(info.Size()).To(BeNumerically(">", 0))
				Expect(info.Size()).To(BeNumerically("<", int64(spec.Active.Size*1024*1024)))
				f, _ := fs.ReadFile(passiveImg)
				// This should be a backup so it should read active
				Expect(f).To(ContainSubstring("active"))
				By("checking transition image")
				// Expect transition image to be gone
				_, err = fs.Stat(spec.Active.File)
				Expect(err).To(HaveOccurred())
				By("checking it called reboot")
			})
			It("Successfully powers off after upgrade from docker image", Label("docker"), func() {
				spec.Active.Source = sdkImages.NewDockerSrc("alpine")
				upgrade = action.NewUpgradeAction(config, spec)
				err := upgrade.Run()
				Expect(err).ToNot(HaveOccurred())

				// Check that the rebrand worked with our kairos-release value
				Expect(memLog).To(ContainSubstring("Setting default grub entry to TESTOS"))

				// This should be the new image
				info, err := fs.Stat(activeImg)
				Expect(err).ToNot(HaveOccurred())
				// Image size should be the config.ImgSize as its truncated from the upgrade
				Expect(info.Size()).To(BeNumerically("==", int64(spec.Active.Size*1024*1024)))
				Expect(info.IsDir()).To(BeFalse())

				// Should have backed up active to passive
				info, err = fs.Stat(passiveImg)
				Expect(err).ToNot(HaveOccurred())
				// Should be a tiny image as it should only contain our text
				// As this was generated by us at the start test and moved by the upgrade from active.iomg
				Expect(info.Size()).To(BeNumerically(">", 0))
				Expect(info.Size()).To(BeNumerically("<", int64(spec.Active.Size*1024*1024)))
				f, _ := fs.ReadFile(passiveImg)
				// This should be a backup so it should read active
				Expect(f).To(ContainSubstring("active"))

				// Expect transition image to be gone
				_, err = fs.Stat(spec.Active.File)
				Expect(err).To(HaveOccurred())
			})
			It("Logs with the action helpers", func() {
				upgrade = action.NewUpgradeAction(config, spec)
				upgrade.Info("info %s", "message")
				upgrade.Debug("debug %s", "message")
				upgrade.Error("error %s", "message")
				Expect(memLog.String()).To(ContainSubstring("info message"))
				Expect(memLog.String()).To(ContainSubstring("debug message"))
				Expect(memLog.String()).To(ContainSubstring("error message"))
			})
			It("Fails if the state partition cannot be remounted RW", func() {
				// Also remove the cmdline so the boot detection fails and logs a warning
				Expect(fs.RemoveAll("/proc/cmdline")).ToNot(HaveOccurred())
				mounter.ErrorOnMount = true
				upgrade = action.NewUpgradeAction(config, spec)
				err := upgrade.Run()
				Expect(err).To(HaveOccurred())
				Expect(memLog.String()).To(ContainSubstring("error detecting boot"))
			})
			It("Fails if deploying the image fails", Label("docker"), func() {
				extractor.SideEffect = func(imageRef, destination, platformRef string) error {
					return fmt.Errorf("extraction error")
				}
				spec.Active.Source = sdkImages.NewDockerSrc("alpine")
				upgrade = action.NewUpgradeAction(config, spec)
				err := upgrade.Run()
				Expect(err).To(HaveOccurred())
				Expect(memLog.String()).To(ContainSubstring("Failed deploying image"))
			})
			It("Fails if labeling the passive image fails", Label("docker"), func() {
				runner.SideEffect = func(command string, args ...string) ([]byte, error) {
					if command == "tune2fs" {
						return []byte{}, fmt.Errorf("tune2fs failure")
					}
					return []byte{}, nil
				}
				spec.Active.Source = sdkImages.NewDockerSrc("alpine")
				upgrade = action.NewUpgradeAction(config, spec)
				err := upgrade.Run()
				Expect(err).To(HaveOccurred())
				Expect(memLog.String()).To(ContainSubstring("Error while labeling the passive image"))
			})
			It("Fails if the active image cannot be backed up", Label("docker"), func() {
				// Replace the active image with a directory so the backup rename fails
				Expect(fs.RemoveAll(activeImg)).ToNot(HaveOccurred())
				Expect(fsutils.MkdirAll(fs, filepath.Join(activeImg, "subdir"), constants.DirPerm)).ToNot(HaveOccurred())
				spec.Active.Source = sdkImages.NewDockerSrc("alpine")
				upgrade = action.NewUpgradeAction(config, spec)
				err := upgrade.Run()
				Expect(err).To(HaveOccurred())
				Expect(memLog.String()).To(ContainSubstring("Failed to move"))
			})
			It("Fails on the after-upgrade-chroot hook when strict", Label("docker"), func() {
				config.Strict = true
				config.CloudInitRunner = &stageFailCloudInitRunner{failStage: constants.AfterUpgradeChrootHook}
				spec.Active.Source = sdkImages.NewDockerSrc("alpine")
				upgrade = action.NewUpgradeAction(config, spec)
				err := upgrade.Run()
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring(constants.AfterUpgradeChrootHook))
				Expect(memLog.String()).To(ContainSubstring("Error running hook after-upgrade-chroot"))
			})
			It("Fails on the after-upgrade hook when strict", Label("docker"), func() {
				config.Strict = true
				config.CloudInitRunner = &stageFailCloudInitRunner{failStage: constants.AfterUpgradeHook}
				spec.Active.Source = sdkImages.NewDockerSrc("alpine")
				upgrade = action.NewUpgradeAction(config, spec)
				err := upgrade.Run()
				Expect(err).To(HaveOccurred())
				Expect(memLog.String()).To(ContainSubstring("Error running hook after-upgrade"))
			})
			It("Fails relabeling when umounting the chroot binds fails", Label("docker"), func() {
				mounter.ErrorOnUnmount = true
				spec.Active.Source = sdkImages.NewDockerSrc("alpine")
				upgrade = action.NewUpgradeAction(config, spec)
				err := upgrade.Run()
				Expect(err).To(HaveOccurred())
			})
			It("Warns but does not fail when the rebranding fails", Label("docker"), func() {
				// Make the grub env file a directory so writing the default entry fails
				Expect(fsutils.MkdirAll(fs, filepath.Join(constants.RunningStateDir, constants.GrubOEMEnv), constants.DirPerm)).ToNot(HaveOccurred())
				spec.Active.Source = sdkImages.NewDockerSrc("alpine")
				upgrade = action.NewUpgradeAction(config, spec)
				err := upgrade.Run()
				Expect(err).ToNot(HaveOccurred())
				Expect(memLog.String()).To(ContainSubstring("failure while rebranding GRUB default entry"))
			})
			It("Warns but does not fail when cleanup fails", Label("docker"), func() {
				config.Syscall = &failingMountSyscall{FakeSyscall: syscall}
				spec.Active.Source = sdkImages.NewDockerSrc("alpine")
				upgrade = action.NewUpgradeAction(config, spec)
				err := upgrade.Run()
				Expect(err).ToNot(HaveOccurred())
				Expect(memLog.String()).To(ContainSubstring("failure during cleanup"))
			})
			It("Successfully upgrades with persistent and OEM partitions", Label("docker"), func() {
				spec.Active.Source = sdkImages.NewDockerSrc("alpine")
				spec.Partitions.Persistent = &sdkPartitions.Partition{
					Name:            "device7",
					FilesystemLabel: constants.PersistentLabel,
					FS:              "ext4",
					MountPoint:      "/usr/local",
					Path:            "/dev/device7",
				}
				spec.Partitions.OEM = &sdkPartitions.Partition{
					Name:            "device6",
					FilesystemLabel: constants.OEMLabel,
					FS:              "ext4",
					MountPoint:      "/oem",
					Path:            "/dev/device6",
				}
				// OEM is already mounted, persistent will be mounted by the upgrade
				mounter.Mount("/dev/device6", "/oem", "auto", []string{"ro"})

				upgrade = action.NewUpgradeAction(config, spec)
				err := upgrade.Run()
				Expect(err).ToNot(HaveOccurred())

				// This should be the new image
				info, err := fs.Stat(activeImg)
				Expect(err).ToNot(HaveOccurred())
				Expect(info.Size()).To(BeNumerically("==", int64(spec.Active.Size*1024*1024)))
			})
			It("Successfully upgrades from directory", Label("directory"), func() {
				dirSrc, _ := fsutils.TempDir(fs, "", "elementalupgrade")
				// Create the dir on real os as rsync works on the real os
				defer fs.RemoveAll(dirSrc)
				spec.Active.Source = sdkImages.NewDirSrc(dirSrc)
				// create a random file on it
				err := fs.WriteFile(fmt.Sprintf("%s/file.file", dirSrc), []byte("something"), constants.FilePerm)
				Expect(err).ToNot(HaveOccurred())

				upgrade = action.NewUpgradeAction(config, spec)
				err = upgrade.Run()
				Expect(err).ToNot(HaveOccurred())

				// Check that the rebrand worked with our kairos-release value
				Expect(memLog).To(ContainSubstring("Setting default grub entry to TESTOS"))

				// Not much that we can create here as the dir copy was done on the real os, but we do the rest of the ops on a mem one
				// This should be the new image
				info, err := fs.Stat(activeImg)
				Expect(err).ToNot(HaveOccurred())
				// Image size should not be empty
				Expect(info.Size()).To(BeNumerically("==", int64(spec.Active.Size*1024*1024)))
				Expect(info.IsDir()).To(BeFalse())

				// Should have backed up active to passive
				info, err = fs.Stat(passiveImg)
				Expect(err).ToNot(HaveOccurred())
				// Should be a tiny image as it should only contain our text
				// As this was generated by us at the start test and moved by the upgrade from active.img
				Expect(info.Size()).To(BeNumerically(">", 0))
				Expect(info.Size()).To(BeNumerically("<", int64(spec.Active.Size*1024*1024)))
				f, _ := fs.ReadFile(passiveImg)
				// This should be a backup so it should read active
				Expect(f).To(ContainSubstring("active"))

				// Expect transition image to be gone
				_, err = fs.Stat(spec.Active.File)
				Expect(err).To(HaveOccurred())

			})
			It("Sets next_entry to cos after a system upgrade", Label("bootentry"), func() {
				Expect(fsutils.MkdirAll(fs, "/etc/cos", constants.DirPerm)).To(Succeed())
				Expect(fs.WriteFile("/etc/cos/grub.cfg", []byte("menuentry x --id cos {"), constants.FilePerm)).To(Succeed())
				Expect(fsutils.MkdirAll(fs, "/oem", constants.DirPerm)).To(Succeed())

				spec.Active.Source = sdkImages.NewDockerSrc("alpine")
				upgrade = action.NewUpgradeAction(config, spec)
				Expect(upgrade.Run()).To(Succeed())

				variables, err := utils.ReadPersistentVariables("/oem/grubenv", config)
				Expect(err).ToNot(HaveOccurred())
				Expect(variables["next_entry"]).To(Equal(constants.BootEntryActive))
			})
		})
		Describe(fmt.Sprintf("Booting from %s", constants.PassiveLabel), Label("passive_label"), func() {
			var err error
			BeforeEach(func() {
				spec, err = agentConfig.NewUpgradeSpec(config)
				Expect(err).ShouldNot(HaveOccurred())

				err = fsutils.MkdirAll(config.Fs, filepath.Join(spec.Active.MountPoint, "etc"), constants.DirPerm)
				Expect(err).ShouldNot(HaveOccurred())

				err = fs.WriteFile(
					filepath.Join(spec.Active.MountPoint, "etc", "kairos-release"),
					[]byte("GRUB_ENTRY_NAME=TESTOS"),
					constants.FilePerm,
				)
				Expect(err).ShouldNot(HaveOccurred())

				err = fsutils.MkdirAll(config.Fs, "/proc", constants.DirPerm)
				Expect(err).ShouldNot(HaveOccurred())

				// Write proc/cmdline so we can detect what we booted from
				err = fs.WriteFile("/proc/cmdline", []byte(constants.PassiveLabel), constants.FilePerm)
				Expect(err).ShouldNot(HaveOccurred())

				spec.Active.Size = 10
				spec.Passive.Size = 10
				spec.Recovery.Size = 10

				runner.SideEffect = func(command string, args ...string) ([]byte, error) {
					if command == "cat" && args[0] == "/proc/cmdline" {
						return []byte(constants.PassiveLabel), nil
					}
					if command == "mv" && args[0] == "-f" && args[1] == spec.Active.File && args[2] == activeImg {
						// we doing the image substitution, do the "move"
						source, _ := fs.ReadFile(spec.Active.File)
						_ = fs.WriteFile(activeImg, source, constants.FilePerm)
						_ = fs.RemoveAll(spec.Active.File)
					}
					if command == "mv" && args[0] == "-f" && args[1] == activeImg && args[2] == passiveImg {
						// If this command was called then its a complete failure as it tried to copy active into passive
						StopTrying("Passive was overwritten").Now()
					}
					return []byte{}, nil
				}
				config.Runner = runner
				// Create fake active/passive files
				_ = fs.WriteFile(activeImg, []byte("active"), constants.FilePerm)
				_ = fs.WriteFile(passiveImg, []byte("passive"), constants.FilePerm)
				// Mount state partition as it is expected to be mounted when booting from active
				mounter.Mount("device2", constants.RunningStateDir, "auto", []string{"ro"})
			})
			AfterEach(func() {
				_ = fs.RemoveAll(activeImg)
				_ = fs.RemoveAll(passiveImg)
			})
			It("does not backup active img to passive", Label("docker"), func() {
				spec.Active.Source = sdkImages.NewDockerSrc("alpine")
				upgrade = action.NewUpgradeAction(config, spec)
				err := upgrade.Run()
				Expect(err).ToNot(HaveOccurred())

				// Check that the rebrand worked with our kairos-release value
				Expect(memLog).To(ContainSubstring("Setting default grub entry to TESTOS"))

				// This should be the new image
				info, err := fs.Stat(activeImg)
				Expect(err).ToNot(HaveOccurred())
				// Image size should not be empty
				Expect(info.Size()).To(BeNumerically("==", int64(spec.Active.Size*1024*1024)))
				Expect(info.IsDir()).To(BeFalse())

				// Passive should have not been touched
				info, err = fs.Stat(passiveImg)
				Expect(err).ToNot(HaveOccurred())
				// Should be a tiny image as it should only contain our text
				// As this was generated by us at the start test and moved by the upgrade from active.iomg
				Expect(info.Size()).To(BeNumerically(">", 0))
				Expect(info.Size()).To(BeNumerically("<", int64(spec.Active.Size*1024*1024)))
				f, _ := fs.ReadFile(passiveImg)
				Expect(f).To(ContainSubstring("passive"))

				// Expect transition image to be gone
				_, err = fs.Stat(spec.Active.File)
				Expect(err).To(HaveOccurred())

			})
			It("Fails moving the transition image to active", Label("docker"), func() {
				// Replace the active image with a directory so the final rename fails
				Expect(fs.RemoveAll(activeImg)).ToNot(HaveOccurred())
				Expect(fsutils.MkdirAll(fs, filepath.Join(activeImg, "subdir"), constants.DirPerm)).ToNot(HaveOccurred())
				spec.Active.Source = sdkImages.NewDockerSrc("alpine")
				upgrade = action.NewUpgradeAction(config, spec)
				err := upgrade.Run()
				Expect(err).To(HaveOccurred())
				Expect(memLog.String()).To(ContainSubstring("Failed to move"))
			})
			It("Sets next_entry to cos when upgrading from passive so the new active is tried on reboot", Label("bootentry"), func() {
				Expect(fsutils.MkdirAll(fs, "/etc/cos", constants.DirPerm)).To(Succeed())
				Expect(fs.WriteFile("/etc/cos/grub.cfg", []byte("menuentry x --id cos {"), constants.FilePerm)).To(Succeed())
				Expect(fsutils.MkdirAll(fs, "/oem", constants.DirPerm)).To(Succeed())

				spec.Active.Source = sdkImages.NewDockerSrc("alpine")
				upgrade = action.NewUpgradeAction(config, spec)
				Expect(upgrade.Run()).To(Succeed())

				variables, err := utils.ReadPersistentVariables("/oem/grubenv", config)
				Expect(err).ToNot(HaveOccurred())
				Expect(variables["next_entry"]).To(Equal(constants.BootEntryActive))
			})
		})
		Describe(fmt.Sprintf("Booting from %s", constants.RecoveryLabel), Label("recovery_label"), func() {
			Describe("Using squashfs", Label("squashfs"), func() {
				var err error
				BeforeEach(func() {
					// Mount recovery partition as it is expected to be mounted when booting from recovery
					mounter.Mount("device5", constants.LiveDir, "auto", []string{"ro"})
					// Create recoveryImgSquash so ti identifies that we are using squash recovery
					err = fs.WriteFile(recoveryImgSquash, []byte("recovery"), constants.FilePerm)
					Expect(err).ShouldNot(HaveOccurred())

					spec, err = agentConfig.NewUpgradeSpec(config)
					Expect(err).ShouldNot(HaveOccurred())
					spec.Active.Size = 10
					spec.Passive.Size = 10
					spec.Recovery.Size = 10
					spec.Entry = constants.BootEntryRecovery

					err = fsutils.MkdirAll(config.Fs, "/proc", constants.DirPerm)
					Expect(err).ShouldNot(HaveOccurred())

					// Write proc/cmdline so we can detect what we booted from
					err = fs.WriteFile("/proc/cmdline", []byte(constants.RecoveryLabel), constants.FilePerm)
					Expect(err).ShouldNot(HaveOccurred())

					runner.SideEffect = func(command string, args ...string) ([]byte, error) {
						if command == "cat" && args[0] == "/proc/cmdline" {
							return []byte(constants.RecoveryLabel), nil
						}
						if command == "mksquashfs" && args[1] == spec.Recovery.File {
							// create the transition img for squash to fake it
							_, _ = fs.Create(spec.Recovery.File)
						}
						if command == "mv" && args[0] == "-f" && args[1] == spec.Recovery.File && args[2] == recoveryImgSquash {
							// fake "move"
							f, _ := fs.ReadFile(spec.Recovery.File)
							_ = fs.WriteFile(recoveryImgSquash, f, constants.FilePerm)
							_ = fs.RemoveAll(spec.Recovery.File)
						}
						return []byte{}, nil
					}
					config.Runner = runner
				})
				It("Successfully upgrades recovery from docker image", Label("docker"), func() {
					// This should be the old image
					info, err := fs.Stat(recoveryImgSquash)
					Expect(err).ToNot(HaveOccurred())
					// Image size should be empty
					Expect(info.Size()).To(BeNumerically(">", 0))
					Expect(info.IsDir()).To(BeFalse())
					f, _ := fs.ReadFile(recoveryImgSquash)
					Expect(f).To(ContainSubstring("recovery"))

					spec.Recovery.Source = sdkImages.NewDockerSrc("alpine")
					upgrade = action.NewUpgradeAction(config, spec)
					err = upgrade.Run()
					Expect(err).ToNot(HaveOccurred())

					// This should be the new image
					info, err = fs.Stat(recoveryImgSquash)
					Expect(err).ToNot(HaveOccurred())
					// Image size should be empty
					Expect(info.Size()).To(BeNumerically("==", 0))
					Expect(info.IsDir()).To(BeFalse())
					f, _ = fs.ReadFile(recoveryImgSquash)
					Expect(f).ToNot(ContainSubstring("recovery"))

					// Transition squash should not exist
					info, err = fs.Stat(spec.Recovery.File)
					Expect(err).To(HaveOccurred())

				})
				It("Successfully upgrades recovery from directory", Label("directory"), func() {
					srcDir, _ := fsutils.TempDir(fs, "", "elemental")
					// create a random file on it
					_ = fs.WriteFile(fmt.Sprintf("%s/file.file", srcDir), []byte("something"), constants.FilePerm)

					spec.Recovery.Source = sdkImages.NewDirSrc(srcDir)
					upgrade = action.NewUpgradeAction(config, spec)
					err := upgrade.Run()
					Expect(err).ToNot(HaveOccurred())

					// This should be the new image
					info, err := fs.Stat(recoveryImgSquash)
					Expect(err).ToNot(HaveOccurred())
					// Image size should be empty
					Expect(info.Size()).To(BeNumerically("==", 0))
					Expect(info.IsDir()).To(BeFalse())

					// Transition squash should not exist
					info, err = fs.Stat(spec.Recovery.File)
					Expect(err).To(HaveOccurred())

				})
			})
			Describe("Not using squashfs", Label("non-squashfs"), func() {
				var err error
				BeforeEach(func() {
					// Create recoveryImg so it identifies that we are using nonsquash recovery
					err = fs.WriteFile(recoveryImg, []byte("recovery"), constants.FilePerm)
					Expect(err).ShouldNot(HaveOccurred())

					spec, err = agentConfig.NewUpgradeSpec(config)
					Expect(err).ShouldNot(HaveOccurred())

					spec.Active.Size = 10
					spec.Passive.Size = 10
					spec.Recovery.Size = 10
					spec.Entry = constants.BootEntryRecovery

					err = fsutils.MkdirAll(config.Fs, "/proc", constants.DirPerm)
					Expect(err).ShouldNot(HaveOccurred())

					// Write proc/cmdline so we can detect what we booted from
					err = fs.WriteFile("/proc/cmdline", []byte(constants.RecoveryLabel), constants.FilePerm)
					Expect(err).ShouldNot(HaveOccurred())

					runner.SideEffect = func(command string, args ...string) ([]byte, error) {
						if command == "cat" && args[0] == "/proc/cmdline" {
							return []byte(constants.RecoveryLabel), nil
						}
						if command == "mv" && args[0] == "-f" && args[1] == spec.Recovery.File && args[2] == recoveryImg {
							// fake "move"
							f, _ := fs.ReadFile(spec.Recovery.File)
							_ = fs.WriteFile(recoveryImg, f, constants.FilePerm)
							_ = fs.RemoveAll(spec.Recovery.File)
						}
						return []byte{}, nil
					}
					config.Runner = runner
					_ = fs.WriteFile(recoveryImg, []byte("recovery"), constants.FilePerm)
					// Mount recovery partition as it is expected to be mounted when booting from recovery
					mounter.Mount("device5", constants.LiveDir, "auto", []string{"ro"})
				})
				It("Successfully upgrades recovery from docker image", Label("docker"), func() {
					// This should be the old image
					info, err := fs.Stat(recoveryImg)
					Expect(err).ToNot(HaveOccurred())
					// Image size should not be empty
					Expect(info.Size()).To(BeNumerically(">", 0))
					Expect(info.Size()).To(BeNumerically("<", int64(spec.Recovery.Size*1024*1024)))
					Expect(info.IsDir()).To(BeFalse())
					f, _ := fs.ReadFile(recoveryImg)
					Expect(f).To(ContainSubstring("recovery"))

					spec.Recovery.Source = sdkImages.NewDockerSrc("alpine")

					upgrade = action.NewUpgradeAction(config, spec)
					err = upgrade.Run()
					Expect(err).ToNot(HaveOccurred())

					// Should have created recovery image
					info, err = fs.Stat(recoveryImg)
					Expect(err).ToNot(HaveOccurred())
					// Image size should be default size
					Expect(info.Size()).To(BeNumerically("==", int64(spec.Recovery.Size*1024*1024)))

					// Expect the rest of the images to not be there
					for _, img := range []string{activeImg, passiveImg, recoveryImgSquash} {
						_, err := fs.Stat(img)
						Expect(err).To(HaveOccurred())
					}
				})
				It("Successfully upgrades recovery from directory", Label("directory"), func() {
					srcDir, _ := fsutils.TempDir(fs, "", "elemental")
					// create a random file on it
					_ = fs.WriteFile(fmt.Sprintf("%s/file.file", srcDir), []byte("something"), constants.FilePerm)

					spec.Recovery.Source = sdkImages.NewDirSrc(srcDir)

					upgrade = action.NewUpgradeAction(config, spec)
					err := upgrade.Run()
					Expect(err).ToNot(HaveOccurred())

					// This should be the new image
					info, err := fs.Stat(recoveryImg)
					Expect(err).ToNot(HaveOccurred())
					// Image size should be default size
					Expect(info.Size()).To(BeNumerically("==", int64(spec.Recovery.Size*1024*1024)))
					Expect(info.IsDir()).To(BeFalse())

					// Transition squash should not exist
					info, err = fs.Stat(spec.Recovery.File)
					Expect(err).To(HaveOccurred())
				})
				It("Does not touch next_entry when only recovery is upgraded", Label("bootentry"), func() {
					Expect(fsutils.MkdirAll(fs, "/etc/cos", constants.DirPerm)).To(Succeed())
					Expect(fs.WriteFile("/etc/cos/grub.cfg", []byte("menuentry x --id cos {"), constants.FilePerm)).To(Succeed())
					Expect(fsutils.MkdirAll(fs, "/oem", constants.DirPerm)).To(Succeed())

					spec.Recovery.Source = sdkImages.NewDockerSrc("alpine")
					upgrade = action.NewUpgradeAction(config, spec)
					Expect(upgrade.Run()).To(Succeed())

					_, err := fs.Stat("/oem/grubenv")
					Expect(err).To(HaveOccurred(), "recovery upgrade must not write grubenv")
				})
			})
		})
	})
})
