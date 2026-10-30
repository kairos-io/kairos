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
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kairos-io/kairos/v4/agent/pkg/action"
	agentConfig "github.com/kairos-io/kairos/v4/agent/pkg/config"
	"github.com/kairos-io/kairos/v4/agent/pkg/constants"
	v1 "github.com/kairos-io/kairos/v4/agent/pkg/implementations/spec"
	"github.com/kairos-io/kairos/v4/agent/pkg/utils"
	fsutils "github.com/kairos-io/kairos/v4/agent/pkg/utils/fs"
	v1mock "github.com/kairos-io/kairos/v4/agent/tests/mocks"
	"github.com/kairos-io/kairos/v4/sdk/diffupgrade"
	ghwMock "github.com/kairos-io/kairos/v4/sdk/ghw/mocks"
	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
	sdkImages "github.com/kairos-io/kairos/v4/sdk/types/images"
	sdkLogger "github.com/kairos-io/kairos/v4/sdk/types/logger"
	sdkPartitions "github.com/kairos-io/kairos/v4/sdk/types/partitions"
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
					[]byte("GRUB_ENTRY_NAME=TESTOS\nKAIROS_INIT_VERSION=\"v4.3.0\"\n"),
					constants.FilePerm,
				)
				Expect(err).ShouldNot(HaveOccurred())
				// KAIROS_INIT_VERSION on the running system's kairos-release
				// so the downgrade gate in UpgradeAction.runFinalizeStep can
				// compare "target >= current". Same version as target = pass.
				Expect(fsutils.MkdirAll(fs, "/etc", constants.DirPerm)).To(Succeed())
				Expect(fs.WriteFile(constants.KairosReleaseFile, []byte(`KAIROS_INIT_VERSION="v4.3.0"`+"\n"), constants.FilePerm)).To(Succeed())

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
			It("Successfully upgrades a machine with no recovery partition", Label("docker"), func() {
				// A system upgrade never writes to the recovery partition, so
				// a machine that has none has to upgrade like any other. Only
				// a --recovery upgrade needs it, and Sanitize rejects that
				// case separately.
				spec.Partitions.Recovery = nil
				spec.Active.Source = sdkImages.NewDockerSrc("alpine")
				upgrade = action.NewUpgradeAction(config, spec)
				Expect(upgrade.Run()).ToNot(HaveOccurred())

				info, err := fs.Stat(activeImg)
				Expect(err).ToNot(HaveOccurred())
				Expect(info.Size()).To(BeNumerically("==", int64(spec.Active.Size*1024*1024)))

				f, _ := fs.ReadFile(passiveImg)
				Expect(f).To(ContainSubstring("active"))
			})
			It("Successfully upgrades from docker image", Label("docker"), func() {
				spec.Active.Source = sdkImages.NewDockerSrc("alpine")
				upgrade = action.NewUpgradeAction(config, spec)
				err := upgrade.Run()
				Expect(err).ToNot(HaveOccurred())

				Expect(memLog).To(ContainSubstring("Handing off upgrade finalize"), memLog.String())

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
				Expect(memLog).To(ContainSubstring("Handing off upgrade finalize"))

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

				Expect(memLog).To(ContainSubstring("Handing off upgrade finalize"))

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

				Expect(memLog).To(ContainSubstring("Handing off upgrade finalize"))

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
			Context("with a diff upgrade", Label("diff"), func() {
				const fullRef = "quay.io/kairos/test:v2"
				const diffRef = fullRef + diffupgrade.TagSuffix
				const base = "sha256:release-1"
				const target = "sha256:release-2"

				// writeDiff unpacks a diff for the given base into dest, the
				// way the extractor would.
				writeDiff := func(dest, forBase string) error {
					release := "GRUB_ENTRY_NAME=TESTOS\nKAIROS_VERSION=v2\nKAIROS_INIT_VERSION=\"v4.3.0\"\n"
					targetSum := sha256.Sum256([]byte(release))
					manifest := fmt.Sprintf(`{"version":1,"base":%q,"target":%q,"target-release":"sha256:%x","deleted":["usr/lib/old"]}`, forBase, target, targetSum)
					for path, content := range map[string]string{
						"usr/bin/new":            "new",
						"etc/kairos-release":     release,
						diffupgrade.ManifestPath: manifest,
					} {
						full := filepath.Join(dest, path)
						if err := fsutils.MkdirAll(fs, filepath.Dir(full), constants.DirPerm); err != nil {
							return err
						}
						if err := fs.WriteFile(full, []byte(content), constants.FilePerm); err != nil {
							return err
						}
					}
					return nil
				}

				BeforeEach(func() {
					spec.Active.Source = sdkImages.NewDockerSrc(fullRef)
					spec.Diff = true

					Expect(fsutils.MkdirAll(fs, filepath.Join(spec.Active.MountPoint, "usr/lib"), constants.DirPerm)).To(Succeed())
					Expect(fs.WriteFile(filepath.Join(spec.Active.MountPoint, "usr/lib/old"), []byte("old"), constants.FilePerm)).To(Succeed())
					// What the install or upgrade that wrote active.img
					// recorded for it.
					Expect(diffupgrade.RecordID(fs, activeImg, base)).To(Succeed())

					// The transition image starts as a copy of active.img.
					previous := runner.SideEffect
					runner.SideEffect = func(command string, args ...string) ([]byte, error) {
						if command == "cp" {
							data, err := fs.ReadFile(args[len(args)-2])
							if err != nil {
								return nil, err
							}
							return nil, fs.WriteFile(args[len(args)-1], data, constants.FilePerm)
						}
						return previous(command, args...)
					}
				})

				It("builds the transition image from the diff instead of the full image", func() {
					extractor.SideEffect = func(imageRef, dest, _ string) error {
						return writeDiff(dest, base)
					}
					upgrade = action.NewUpgradeAction(config, spec)
					Expect(upgrade.Run()).To(Succeed())

					Expect(extractor.WasCalledWithImageRef(diffRef)).To(BeTrue())
					Expect(extractor.WasCalledWithImageRef(fullRef)).To(BeFalse(), "the full image must not be pulled")
					Expect(runner.IncludesCmds([][]string{
						{"cp", "--sparse=always", activeImg, spec.Active.File},
						{"tune2fs", "-L", spec.Active.Label, spec.Active.File},
						{"tune2fs", "-U", "random", spec.Active.File},
					})).To(Succeed())

					root := spec.Active.MountPoint
					Expect(fs.ReadFile(filepath.Join(root, "usr/bin/new"))).To(Equal([]byte("new")))
					_, err := fs.Stat(filepath.Join(root, "usr/lib/old"))
					Expect(err).To(MatchError(os.ErrNotExist), "deleted paths are removed")
					_, err = fs.Stat(filepath.Join(root, diffupgrade.ManifestDir))
					Expect(err).To(MatchError(os.ErrNotExist), "the manifest does not stay on the system")

					// Same swap as a full upgrade: the new image is active,
					// the old one is passive.
					Expect(fs.ReadFile(activeImg)).To(Equal([]byte("active")))
					Expect(fs.ReadFile(passiveImg)).To(Equal([]byte("active")))
					_, err = fs.Stat(spec.Active.File)
					Expect(err).To(HaveOccurred(), "the transition image is gone")

					// The IDs follow the images through the swap.
					Expect(diffupgrade.RecordedID(fs, activeImg)).To(Equal(target))
					Expect(diffupgrade.RecordedID(fs, passiveImg)).To(Equal(base))
				})

				It("copies the base in full with a cp that has no --sparse, and checks the copy when tune2fs asks", func() {
					previous := runner.SideEffect
					uuidTries := 0
					runner.SideEffect = func(command string, args ...string) ([]byte, error) {
						if command == "cp" && args[0] == "--sparse=always" {
							return []byte("cp: unrecognized option: sparse=always"), fmt.Errorf("exit status 1")
						}
						if command == "tune2fs" && args[0] == "-U" {
							uuidTries++
							if uuidTries == 1 {
								return []byte("Please run e2fsck -f on the filesystem."), fmt.Errorf("exit status 1")
							}
						}
						return previous(command, args...)
					}
					extractor.SideEffect = func(imageRef, dest, _ string) error {
						return writeDiff(dest, base)
					}
					upgrade = action.NewUpgradeAction(config, spec)
					Expect(upgrade.Run()).To(Succeed())
					Expect(runner.IncludesCmds([][]string{
						{"cp", activeImg, spec.Active.File},
						{"e2fsck", "-fy", spec.Active.File},
					})).To(Succeed())
					Expect(uuidTries).To(Equal(2))
					Expect(memLog.String()).ToNot(ContainSubstring("keeps the filesystem UUID"))
					Expect(diffupgrade.RecordedID(fs, activeImg)).To(Equal(target))
				})

				It("falls back without pulling the diff when no ID was recorded for the base", func() {
					Expect(diffupgrade.ForgetID(fs, activeImg)).To(Succeed())
					upgrade = action.NewUpgradeAction(config, spec)
					Expect(upgrade.Run()).To(Succeed())
					Expect(memLog.String()).To(ContainSubstring(diffupgrade.ErrNoRecordedID.Error()))
					Expect(extractor.ClientCalls).To(HaveLen(1))
					Expect(extractor.ClientCalls[0].ImageRef).To(Equal(fullRef))
				})

				It("does not trust an ID recorded before the base image was rewritten", func() {
					// What an older agent resetting the node would leave.
					Expect(fs.WriteFile(activeImg, []byte("rewritten by something else"), constants.FilePerm)).To(Succeed())
					upgrade = action.NewUpgradeAction(config, spec)
					Expect(upgrade.Run()).To(Succeed())
					Expect(memLog.String()).To(ContainSubstring("rewritten after its ID was recorded"))
					Expect(extractor.WasCalledWithImageRef(diffRef)).To(BeFalse())
				})

				It("falls back to the full image when the diff is for another release", func() {
					extractor.SideEffect = func(imageRef, dest, _ string) error {
						if imageRef == diffRef {
							return writeDiff(dest, "sha256:another-release")
						}
						return nil
					}
					upgrade = action.NewUpgradeAction(config, spec)
					Expect(upgrade.Run()).To(Succeed())

					Expect(memLog.String()).To(ContainSubstring("falling back to the full image"))
					Expect(memLog.String()).To(ContainSubstring(diffupgrade.ErrBaseMismatch.Error()))
					Expect(extractor.ClientCalls).To(HaveLen(2))
					Expect(extractor.ClientCalls[0].ImageRef).To(Equal(diffRef))
					Expect(extractor.ClientCalls[1].ImageRef).To(Equal(fullRef))

					info, err := fs.Stat(activeImg)
					Expect(err).ToNot(HaveOccurred())
					Expect(info.Size()).To(BeNumerically("==", int64(spec.Active.Size*1024*1024)), "active.img is the full image")
				})

				It("falls back to the full image when the diff cannot be pulled", func() {
					extractor.SideEffect = func(imageRef, _, _ string) error {
						if imageRef == diffRef {
							return fmt.Errorf("MANIFEST_UNKNOWN")
						}
						return nil
					}
					upgrade = action.NewUpgradeAction(config, spec)
					Expect(upgrade.Run()).To(Succeed())
					Expect(memLog.String()).To(ContainSubstring("MANIFEST_UNKNOWN"))
					Expect(extractor.WasCalledWithImageRef(fullRef)).To(BeTrue())
				})

				It("falls back to the full image when the source is pinned by digest", func() {
					spec.Active.Source = sdkImages.NewDockerSrc("quay.io/kairos/test@sha256:" + strings.Repeat("0", 64))
					upgrade = action.NewUpgradeAction(config, spec)
					Expect(upgrade.Run()).To(Succeed())
					Expect(memLog.String()).To(ContainSubstring("pinned by digest"))
					Expect(extractor.ClientCalls).To(HaveLen(1))
				})

				It("pulls the entry of the diff index whose base is this release", func() {
					const ownDiff = "quay.io/kairos/test@sha256:0000000000000000000000000000000000000000000000000000000000000001"
					extractor.ResolveAnnotatedFunc = func(ref string, match func(map[string]string) bool) (string, error) {
						Expect(ref).To(Equal(diffRef))
						// An index with diffs from two releases.
						if match(map[string]string{diffupgrade.BaseLabel: "sha256:release-0"}) {
							return "quay.io/kairos/test@sha256:wrong", nil
						}
						if match(map[string]string{diffupgrade.BaseLabel: base}) {
							return ownDiff, nil
						}
						return "", fmt.Errorf("no match")
					}
					extractor.SideEffect = func(imageRef, dest, _ string) error {
						return writeDiff(dest, base)
					}
					upgrade = action.NewUpgradeAction(config, spec)
					Expect(upgrade.Run()).To(Succeed())
					Expect(extractor.ClientCalls).To(HaveLen(1))
					Expect(extractor.ClientCalls[0].ImageRef).To(Equal(ownDiff))
					Expect(diffupgrade.RecordedID(fs, activeImg)).To(Equal(target))
				})

				It("falls back when neither the index nor the diff's own tag has a diff for this release", func() {
					ownTag, err := diffupgrade.BaseRefFor(diffRef, base)
					Expect(err).ToNot(HaveOccurred())
					extractor.ResolveAnnotatedFunc = func(ref string, _ func(map[string]string) bool) (string, error) {
						return "", fmt.Errorf("%s: no manifest in the image index matches", ref)
					}
					extractor.SideEffect = func(imageRef, _, _ string) error {
						if imageRef == ownTag {
							return fmt.Errorf("MANIFEST_UNKNOWN")
						}
						return nil
					}
					upgrade = action.NewUpgradeAction(config, spec)
					Expect(upgrade.Run()).To(Succeed())
					Expect(memLog.String()).To(ContainSubstring("falling back to the full image"))
					Expect(extractor.ClientCalls).To(HaveLen(2))
					Expect(extractor.ClientCalls[0].ImageRef).To(Equal(ownTag))
					Expect(extractor.ClientCalls[1].ImageRef).To(Equal(fullRef))
				})

				It("finds a diff a concurrent publish dropped from the index at its own tag", func() {
					ownTag, err := diffupgrade.BaseRefFor(diffRef, base)
					Expect(err).ToNot(HaveOccurred())
					Expect(ownTag).To(Equal(diffRef + "-release-1"))
					extractor.ResolveAnnotatedFunc = func(ref string, _ func(map[string]string) bool) (string, error) {
						return "", fmt.Errorf("%s: no manifest in the image index matches", ref)
					}
					extractor.SideEffect = func(imageRef, dest, _ string) error {
						return writeDiff(dest, base)
					}
					upgrade = action.NewUpgradeAction(config, spec)
					Expect(upgrade.Run()).To(Succeed())
					Expect(extractor.ClientCalls).To(HaveLen(1))
					Expect(extractor.ClientCalls[0].ImageRef).To(Equal(ownTag))
					Expect(diffupgrade.RecordedID(fs, activeImg)).To(Equal(target))
				})

				It("records the rootfs ID of a full upgrade and moves the old one to passive", func() {
					spec.Diff = false
					upgrade = action.NewUpgradeAction(config, spec)
					Expect(upgrade.Run()).To(Succeed())

					root, err := fs.RawPath(spec.Active.MountPoint)
					Expect(err).ToNot(HaveOccurred())
					expected, err := diffupgrade.RootfsID(root)
					Expect(err).ToNot(HaveOccurred())
					Expect(diffupgrade.RecordedID(fs, activeImg)).To(Equal(expected))
					Expect(diffupgrade.RecordedID(fs, passiveImg)).To(Equal(base))
				})

				It("uses diff-source when it is set", func() {
					spec.Diff = false
					spec.DiffSource = "oci:example.org/custom/diff:latest"
					extractor.SideEffect = func(imageRef, dest, _ string) error {
						return writeDiff(dest, base)
					}
					upgrade = action.NewUpgradeAction(config, spec)
					Expect(upgrade.Run()).To(Succeed())
					Expect(extractor.ClientCalls).To(HaveLen(1))
					Expect(extractor.ClientCalls[0].ImageRef).To(Equal("example.org/custom/diff:latest"))
				})
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
					[]byte("GRUB_ENTRY_NAME=TESTOS\nKAIROS_INIT_VERSION=\"v4.3.0\"\n"),
					constants.FilePerm,
				)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(fsutils.MkdirAll(fs, "/etc", constants.DirPerm)).To(Succeed())
				Expect(fs.WriteFile(constants.KairosReleaseFile, []byte(`KAIROS_INIT_VERSION="v4.3.0"`+"\n"), constants.FilePerm)).To(Succeed())

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

				Expect(memLog).To(ContainSubstring("Handing off upgrade finalize"))

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

					// KAIROS_INIT_VERSION setup for the downgrade gate. The
					// squashfs recovery tree is unpacked into a temp dir, so
					// the target side of the compare is written by the
					// extractor and rsync fakes below.
					Expect(fsutils.MkdirAll(fs, "/etc", constants.DirPerm)).To(Succeed())
					Expect(fs.WriteFile(constants.KairosReleaseFile, []byte(`KAIROS_INIT_VERSION="v4.3.0"`+"\n"), constants.FilePerm)).To(Succeed())
					extractor.SideEffect = func(_, destination, _ string) error {
						if err := fsutils.MkdirAll(fs, filepath.Join(destination, "etc"), constants.DirPerm); err != nil {
							return err
						}
						return fs.WriteFile(filepath.Join(destination, constants.KairosReleaseFile), []byte(`KAIROS_INIT_VERSION="v4.3.0"`+"\n"), constants.FilePerm)
					}

					err = fsutils.MkdirAll(config.Fs, "/proc", constants.DirPerm)
					Expect(err).ShouldNot(HaveOccurred())

					// Write proc/cmdline so we can detect what we booted from
					err = fs.WriteFile("/proc/cmdline", []byte(constants.RecoveryLabel), constants.FilePerm)
					Expect(err).ShouldNot(HaveOccurred())

					runner.SideEffect = func(command string, args ...string) ([]byte, error) {
						if command == "cat" && args[0] == "/proc/cmdline" {
							return []byte(constants.RecoveryLabel), nil
						}
						if command == constants.Rsync {
							// rsync gets real paths, the target is the last arg
							target := args[len(args)-1]
							_ = os.MkdirAll(filepath.Join(target, "etc"), constants.DirPerm)
							_ = os.WriteFile(filepath.Join(target, constants.KairosReleaseFile), []byte(`KAIROS_INIT_VERSION="v4.3.0"`+"\n"), constants.FilePerm)
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
				It("runs finalize on the unpacked recovery tree before building the squashfs", Label("docker", "bug-5619"), func() {
					spec.ExtraDirsRootfs = []string{"/var/lib/kubelet"}
					spec.Recovery.Source = sdkImages.NewDockerSrc("alpine")

					var tree string
					baseExtract := extractor.SideEffect
					extractor.SideEffect = func(imageRef, destination, platformRef string) error {
						tree = destination
						if err := fs.WriteFile(filepath.Join(destination, "file.file"), []byte("something"), constants.FilePerm); err != nil {
							return err
						}
						return baseExtract(imageRef, destination, platformRef)
					}

					finalized := false
					squashed := false
					baseEffect := runner.SideEffect
					runner.SideEffect = func(command string, args ...string) ([]byte, error) {
						if command == constants.TargetKairosAgentPath && len(args) > 0 && args[0] == "upgrade-finalize" {
							Expect(tree).ToNot(BeEmpty())
							Expect(syscall.WasChrootCalledWith(tree)).To(BeTrue())
							Expect(fsutils.Exists(fs, constants.UpgradeFinalizeContextPath)).To(BeFalse())
							Expect(fsutils.Exists(fs, filepath.Join(tree, "file.file"))).To(BeTrue())

							ctx, err := action.ReadFinalizeContext(fs, filepath.Join(tree, constants.UpgradeFinalizeContextPath))
							Expect(err).ToNot(HaveOccurred())
							Expect(ctx.ImgMountPoint).To(Equal("/"))
							Expect(ctx.RecoveryUpgrade).To(BeTrue())
							Expect(ctx.ExtraDirsRootfs).To(Equal([]string{"/var/lib/kubelet"}))

							// The fake syscall does not chroot, so resolve the
							// target's "/" to the tree it was handed.
							ctx.ImgMountPoint = tree
							Expect(action.RunFinalize(config, ctx)).To(Succeed())
							finalized = true
						}
						if command == "mksquashfs" {
							Expect(finalized).To(BeTrue())
							Expect(args[0]).To(Equal(tree))
							Expect(fsutils.Exists(fs, filepath.Join(tree, "var", "lib", "kubelet"))).To(BeTrue())
							squashed = true
						}
						return baseEffect(command, args...)
					}

					upgrade = action.NewUpgradeAction(config, spec)
					Expect(upgrade.Run()).To(Succeed(), memLog.String())

					Expect(finalized).To(BeTrue())
					Expect(squashed).To(BeTrue())
					Expect(fsutils.Exists(fs, tree)).To(BeFalse())
					Expect(fsutils.Exists(fs, "/var/lib/kubelet")).To(BeFalse())
					Expect(fsutils.Exists(fs, constants.UpgradeFinalizeContextPath)).To(BeFalse())
					Expect(fsutils.Exists(fs, spec.Recovery.File)).To(BeFalse())
					Expect(fsutils.Exists(fs, recoveryImgSquash)).To(BeTrue())
				})
				It("drops the unpacked recovery tree without building the squashfs when finalize fails", Label("docker", "bug-5619"), func() {
					Expect(fs.WriteFile(constants.KairosReleaseFile, []byte(`KAIROS_INIT_VERSION="v9.0.0"`+"\n"), constants.FilePerm)).To(Succeed())
					spec.Recovery.Source = sdkImages.NewDockerSrc("alpine")

					var tree string
					baseExtract := extractor.SideEffect
					extractor.SideEffect = func(imageRef, destination, platformRef string) error {
						tree = destination
						return baseExtract(imageRef, destination, platformRef)
					}

					upgrade = action.NewUpgradeAction(config, spec)
					err := upgrade.Run()
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring("refusing to upgrade"))

					Expect(tree).ToNot(BeEmpty())
					for _, cmd := range runner.Cmds() {
						Expect(cmd[0]).ToNot(Equal("mksquashfs"))
					}
					Expect(fsutils.Exists(fs, tree)).To(BeFalse())
					Expect(fsutils.Exists(fs, spec.Recovery.File)).To(BeFalse())
					f, err := fs.ReadFile(recoveryImgSquash)
					Expect(err).ToNot(HaveOccurred())
					Expect(string(f)).To(Equal("recovery"))
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

					// KAIROS_INIT_VERSION setup for the downgrade gate.
					Expect(fsutils.MkdirAll(fs, "/etc", constants.DirPerm)).To(Succeed())
					Expect(fs.WriteFile(constants.KairosReleaseFile, []byte(`KAIROS_INIT_VERSION="v4.3.0"`+"\n"), constants.FilePerm)).To(Succeed())
					if mp := spec.Recovery.MountPoint; mp != "" {
						Expect(fsutils.MkdirAll(fs, filepath.Join(mp, "etc"), constants.DirPerm)).To(Succeed())
						Expect(fs.WriteFile(filepath.Join(mp, "etc", "kairos-release"), []byte(`KAIROS_INIT_VERSION="v4.3.0"`+"\n"), constants.FilePerm)).To(Succeed())
					}

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
