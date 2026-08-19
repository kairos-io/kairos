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

package config_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kairos-io/kairos-agent/v2/pkg/config"
	"github.com/kairos-io/kairos-agent/v2/pkg/constants"
	"github.com/kairos-io/kairos-agent/v2/pkg/implementations/imageextractor"
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/implementations/spec"
	fsutils "github.com/kairos-io/kairos-agent/v2/pkg/utils/fs"
	v1mock "github.com/kairos-io/kairos-agent/v2/tests/mocks"
	"github.com/kairos-io/kairos-sdk/collector"
	sdkConstants "github.com/kairos-io/kairos-sdk/constants"
	ghwMock "github.com/kairos-io/kairos-sdk/ghw/mocks"
	sdkBundles "github.com/kairos-io/kairos-sdk/types/bundles"
	sdkConfig "github.com/kairos-io/kairos-sdk/types/config"
	sdkImages "github.com/kairos-io/kairos-sdk/types/images"
	sdkInstall "github.com/kairos-io/kairos-sdk/types/install"
	sdkLogger "github.com/kairos-io/kairos-sdk/types/logger"
	sdkPartitions "github.com/kairos-io/kairos-sdk/types/partitions"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/rs/zerolog"
	"github.com/sanity-io/litter"
	"github.com/twpayne/go-vfs/v5/vfst"
	"k8s.io/mount-utils"
)

var _ = Describe("Types", Label("types", "config"), func() {
	Describe("Config", func() {
		var err error
		var cleanup func()
		var fs *vfst.TestFS
		var mounter *v1mock.ErrorMounter
		var runner *v1mock.FakeRunner
		var client *v1mock.FakeHTTPClient
		var sysc *v1mock.FakeSyscall
		var logger sdkLogger.KairosLogger
		var ci *v1mock.FakeCloudInitRunner
		var c *sdkConfig.Config
		var memLog bytes.Buffer

		BeforeEach(func() {
			memLog = bytes.Buffer{}
			logger = sdkLogger.NewBufferLogger(&memLog)
			logger.SetLevel("debug")

			fs, cleanup, err = vfst.NewTestFS(nil)
			Expect(err).ToNot(HaveOccurred())
			mounter = v1mock.NewErrorMounter()
			runner = v1mock.NewFakeRunner()
			client = &v1mock.FakeHTTPClient{}
			sysc = &v1mock.FakeSyscall{}
			ci = &v1mock.FakeCloudInitRunner{}
			c = config.NewConfig(
				config.WithFs(fs),
				config.WithMounter(mounter),
				config.WithRunner(runner),
				config.WithSyscall(sysc),
				config.WithLogger(logger),
				config.WithCloudInitRunner(ci),
				config.WithClient(client),
				config.WithPlatform("linux/arm64"),
			)
			c.Install = &sdkInstall.Install{}
			c.Bundles = sdkBundles.Bundles{}
			c.Collector = collector.Config{}
		})
		AfterEach(func() {
			cleanup()
		})
		Describe("ConfigOptions", func() {
			It("Sets the proper interfaces in the config struct", func() {
				Expect(c.Fs).To(Equal(fs))
				Expect(c.Mounter).To(Equal(mounter))
				Expect(c.Runner).To(Equal(runner))
				Expect(c.Syscall).To(Equal(sysc))
				Expect(c.Logger).To(Equal(logger))
				Expect(c.CloudInitRunner).To(Equal(ci))
				Expect(c.Client).To(Equal(client))
				Expect(c.Platform.OS).To(Equal("linux"))
				Expect(c.Platform.Arch).To(Equal("arm64"))
				Expect(c.Platform.GolangArch).To(Equal("arm64"))
			})
			It("Sets the runner if we dont pass one", func() {
				fs, cleanup, err := vfst.NewTestFS(nil)
				defer cleanup()
				Expect(err).ToNot(HaveOccurred())
				c := config.NewConfig(
					config.WithFs(fs),
					config.WithMounter(mounter),
				)
				Expect(c.Fs).To(Equal(fs))
				Expect(c.Mounter).To(Equal(mounter))
				Expect(c.Runner).NotTo(BeNil())
			})
			It("defaults to sane platform if the platform is broken", func() {
				c = config.NewConfig(
					config.WithFs(fs),
					config.WithMounter(mounter),
					config.WithRunner(runner),
					config.WithSyscall(sysc),
					config.WithLogger(logger),
					config.WithCloudInitRunner(ci),
					config.WithClient(client),
					config.WithPlatform("wwwwwww"),
				)
				Expect(c.Platform.OS).To(Equal("linux"))
				Expect(c.Platform.Arch).To(Equal("x86_64"))
				Expect(c.Platform.GolangArch).To(Equal("amd64"))
			})
			It("accepts riscv64 platforms", func() {
				c = config.NewConfig(
					config.WithFs(fs),
					config.WithMounter(mounter),
					config.WithRunner(runner),
					config.WithSyscall(sysc),
					config.WithLogger(logger),
					config.WithCloudInitRunner(ci),
					config.WithClient(client),
					config.WithPlatform("linux/riscv64"),
				)
				Expect(c.Platform.OS).To(Equal("linux"))
				Expect(c.Platform.Arch).To(Equal("riscv64"))
				Expect(c.Platform.GolangArch).To(Equal("riscv64"))
			})
		})
		Describe("ConfigOptions no mounter specified", Label("mount", "mounter"), func() {
			It("should use the default mounter", Label("systemctl"), func() {
				runner := v1mock.NewFakeRunner()
				sysc := &v1mock.FakeSyscall{}
				logger := sdkLogger.NewNullLogger()
				c := config.NewConfig(
					config.WithRunner(runner),
					config.WithSyscall(sysc),
					config.WithLogger(logger),
				)
				Expect(c.Mounter).To(Equal(mount.New(constants.MountBinary)))
			})
		})
		Describe("Config", func() {
			cfg := config.NewConfig(config.WithMounter(mounter))
			Expect(cfg.Mounter).To(Equal(mounter))
			Expect(cfg.Runner).NotTo(BeNil())
		})
		Describe("InstallSpec", func() {
			It("sets installation defaults from install efi media with recovery", Label("install", "efi"), func() {
				// Set EFI firmware detection
				err = fsutils.MkdirAll(fs, filepath.Dir(constants.EfiDevice), constants.DirPerm)
				Expect(err).ShouldNot(HaveOccurred())
				_, err = fs.Create(constants.EfiDevice)
				Expect(err).ShouldNot(HaveOccurred())

				setupIsoBaseTreeDetection(fs)

				// Set recovery image detection detection
				recoveryImgFile := filepath.Join(constants.LiveDir, constants.RecoverySquashFile)
				err = fsutils.MkdirAll(fs, filepath.Dir(recoveryImgFile), constants.DirPerm)
				Expect(err).ShouldNot(HaveOccurred())
				_, err = fs.Create(recoveryImgFile)
				Expect(err).ShouldNot(HaveOccurred())

				spec, err := config.NewInstallSpec(c)
				Expect(err).ToNot(HaveOccurred())
				Expect(spec.Firmware).To(Equal(sdkConstants.EFI))
				Expect(spec.Active.Source.Value()).To(Equal(constants.IsoBaseTree))
				Expect(spec.Recovery.Source.Value()).To(Equal(recoveryImgFile))
				Expect(spec.PartTable).To(Equal(sdkConstants.GPT))

				// No firmware partitions added yet
				Expect(spec.Partitions.EFI).To(BeNil())

				// Adding firmware partitions
				err = spec.Partitions.SetFirmwarePartitions(spec.Firmware, spec.PartTable)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(spec.Partitions.EFI).NotTo(BeNil())
			})
			It("sets installation defaults from install bios media without recovery", Label("install", "bios"), func() {
				setupIsoBaseTreeDetection(fs)

				spec, err := config.NewInstallSpec(c)
				Expect(err).ToNot(HaveOccurred())
				Expect(spec.Firmware).To(Equal(sdkConstants.BIOS))
				Expect(spec.Active.Source.Value()).To(Equal(constants.IsoBaseTree))
				Expect(spec.Recovery.Source.Value()).To(Equal(spec.Active.File))
				Expect(spec.PartTable).To(Equal(sdkConstants.GPT))

				// No firmware partitions added yet
				Expect(spec.Partitions.BIOS).To(BeNil())

				// Adding firmware partitions
				err = spec.Partitions.SetFirmwarePartitions(spec.Firmware, spec.PartTable)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(spec.Partitions.BIOS).NotTo(BeNil())
			})
			It("fails if not in installation media or without source", Label("install"), func() {
				// Should fail if not on installation media and no source specified
				spec, err := config.NewInstallSpec(c)
				Expect(err).ToNot(HaveOccurred())
				Expect(spec.Sanitize()).To(HaveOccurred())

			})
			It("sets installation defaults without being on installation media but with source", Label("install"), func() {
				c.Install.Source = "oci:test:latest"
				spec, err := config.NewInstallSpec(c)
				Expect(err).ToNot(HaveOccurred())
				Expect(spec.Firmware).To(Equal(sdkConstants.BIOS))
				Expect(spec.Active.Source.IsEmpty()).To(BeFalse())
				Expect(spec.Recovery.Source.Value()).To(Equal(spec.Active.File))
				Expect(spec.PartTable).To(Equal(sdkConstants.GPT))
				Expect(spec.Sanitize()).ToNot(HaveOccurred())
			})
			It("sets installation defaults without being on installation media and no source, fails sanitize", Label("install"), func() {
				spec, err := config.NewInstallSpec(c)
				Expect(err).ToNot(HaveOccurred())
				Expect(spec.Firmware).To(Equal(sdkConstants.BIOS))
				Expect(spec.Active.Source.IsEmpty()).To(BeTrue())
				Expect(spec.Recovery.Source.Value()).To(Equal(spec.Active.File))
				Expect(spec.PartTable).To(Equal(sdkConstants.GPT))
				Expect(spec.Sanitize()).To(HaveOccurred())
			})
			It("copies reboot flag from config to spec", Label("install", "reboot"), func() {
				setupIsoBaseTreeDetection(fs)

				// Set reboot flag in config
				c.Install.Reboot = true
				c.Install.Poweroff = false

				spec, err := config.NewInstallSpec(c)
				Expect(err).ToNot(HaveOccurred())
				Expect(spec.Reboot).To(BeTrue())
				Expect(spec.PowerOff).To(BeFalse())
				Expect(spec.ShouldReboot()).To(BeTrue())
				Expect(spec.ShouldShutdown()).To(BeFalse())
			})
			It("copies poweroff flag from config to spec", Label("install", "poweroff"), func() {
				setupIsoBaseTreeDetection(fs)

				// Set poweroff flag in config
				c.Install.Reboot = false
				c.Install.Poweroff = true

				spec, err := config.NewInstallSpec(c)
				Expect(err).ToNot(HaveOccurred())
				Expect(spec.Reboot).To(BeFalse())
				Expect(spec.PowerOff).To(BeTrue())
				Expect(spec.ShouldReboot()).To(BeFalse())
				Expect(spec.ShouldShutdown()).To(BeTrue())
			})
			It("copies both reboot and poweroff flags from config to spec", Label("install", "reboot", "poweroff"), func() {
				setupIsoBaseTreeDetection(fs)

				// Set both flags in config
				c.Install.Reboot = true
				c.Install.Poweroff = true

				spec, err := config.NewInstallSpec(c)
				Expect(err).ToNot(HaveOccurred())
				Expect(spec.Reboot).To(BeTrue())
				Expect(spec.PowerOff).To(BeTrue())
				Expect(spec.ShouldReboot()).To(BeTrue())
				Expect(spec.ShouldShutdown()).To(BeTrue())
			})
			It("defaults reboot and poweroff flags to false when not set", Label("install", "reboot", "poweroff"), func() {
				setupIsoBaseTreeDetection(fs)

				// Ensure flags are false in config
				c.Install.Reboot = false
				c.Install.Poweroff = false

				spec, err := config.NewInstallSpec(c)
				Expect(err).ToNot(HaveOccurred())
				Expect(spec.Reboot).To(BeFalse())
				Expect(spec.PowerOff).To(BeFalse())
				Expect(spec.ShouldReboot()).To(BeFalse())
				Expect(spec.ShouldShutdown()).To(BeFalse())
			})
			It("keeps the secure extractor by default", Label("install", "allow-insecure-registries"), func() {
				setupIsoBaseTreeDetection(fs)

				spec, err := config.NewInstallSpec(c)
				Expect(err).ToNot(HaveOccurred())
				Expect(spec.AllowInsecureRegistries).To(BeFalse())
				Expect(c.ImageExtractor).To(Equal(imageextractor.OCIImageExtractor{Insecure: false}))
			})
			It("enables the insecure extractor when install.allow-insecure-registries is set", Label("install", "allow-insecure-registries"), func() {
				setupIsoBaseTreeDetection(fs)

				cfg, err := config.ScanNoLogs(collector.Readers(strings.NewReader("#cloud-config\ninstall:\n  allow-insecure-registries: true\n")))
				Expect(err).ToNot(HaveOccurred())
				c.Collector = cfg.Collector

				spec, err := config.NewInstallSpec(c)
				Expect(err).ToNot(HaveOccurred())
				Expect(spec.AllowInsecureRegistries).To(BeTrue())
				Expect(c.ImageExtractor).To(Equal(imageextractor.OCIImageExtractor{Insecure: true}))
			})
		})
		Describe("ResetSpec", Label("reset"), func() {
			Describe("Successful executions", func() {
				var ghwTest ghwMock.GhwMock
				BeforeEach(func() {
					mainDisk := sdkPartitions.Disk{
						Name: "device",
						Partitions: []*sdkPartitions.Partition{
							{
								Name:            "device1",
								FilesystemLabel: constants.EfiLabel,
								FS:              "vfat",
							},
							{
								Name:            "device2",
								FilesystemLabel: constants.OEMLabel,
								FS:              "ext4",
							},
							{
								Name:            "device3",
								FilesystemLabel: constants.RecoveryLabel,
								FS:              "ext4",
							},
							{
								Name:            "device4",
								FilesystemLabel: constants.StateLabel,
								FS:              "ext4",
							},
							{
								Name:            "device5",
								FilesystemLabel: constants.PersistentLabel,
								FS:              "ext4",
							},
						},
					}
					ghwTest = ghwMock.GhwMock{}
					ghwTest.AddDisk(mainDisk)
					ghwTest.CreateDevices()

					runner.SideEffect = func(cmd string, args ...string) ([]byte, error) {
						switch cmd {
						case "cat":
							return []byte(constants.SystemLabel), nil
						default:
							return []byte{}, nil
						}
					}
				})
				AfterEach(func() {
					ghwTest.Clean()
				})
				It("sets reset defaults on efi from squashed recovery", func() {
					// Set EFI firmware detection
					err = fsutils.MkdirAll(fs, filepath.Dir(constants.EfiDevice), constants.DirPerm)
					Expect(err).ShouldNot(HaveOccurred())
					_, err = fs.Create(constants.EfiDevice)
					Expect(err).ShouldNot(HaveOccurred())

					setupIsoBaseTreeDetection(fs)

					spec, err := config.NewResetSpec(c)
					Expect(err).ShouldNot(HaveOccurred())
					Expect(spec.Active.Source.Value()).To(Equal(constants.IsoBaseTree))
					Expect(spec.Partitions.EFI.MountPoint).To(Equal(constants.EfiDir))
				})
				It("sets reset defaults on bios from non-squashed recovery", func() {
					// Set non-squashfs recovery image detection
					recoveryImg := filepath.Join(constants.RunningStateDir, "cOS", constants.RecoveryImgFile)
					err = fsutils.MkdirAll(fs, filepath.Dir(recoveryImg), constants.DirPerm)
					Expect(err).ShouldNot(HaveOccurred())
					_, err = fs.Create(recoveryImg)
					Expect(err).ShouldNot(HaveOccurred())

					spec, err := config.NewResetSpec(c)
					Expect(err).ShouldNot(HaveOccurred())
					Expect(spec.Active.Source.Value()).To(Equal(recoveryImg))
				})
				It("sets reset defaults on bios from unknown recovery", func() {
					spec, err := config.NewResetSpec(c)
					Expect(err).ShouldNot(HaveOccurred())
					Expect(spec.Active.Source.IsEmpty()).To(BeTrue())
				})
			})
			Describe("Failures", func() {
				var bootedFrom string
				var ghwTest ghwMock.GhwMock
				BeforeEach(func() {
					bootedFrom = ""
					runner.SideEffect = func(cmd string, args ...string) ([]byte, error) {
						switch cmd {
						case "cat":
							return []byte(bootedFrom), nil
						default:
							return []byte{}, nil
						}
					}

					// Set an empty disk for tests, otherwise reads the hosts hardware
					mainDisk := sdkPartitions.Disk{
						Name: "device",
						Partitions: []*sdkPartitions.Partition{
							{
								Name:            "device4",
								FilesystemLabel: constants.StateLabel,
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
				It("fails to set defaults if not booted from recovery", func() {
					_, err := config.NewResetSpec(c)
					Expect(err).Should(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring("reset can only be called from the recovery system"))
				})
				It("fails to set defaults if no recovery partition detected", func() {
					bootedFrom = constants.SystemLabel
					_, err := config.NewResetSpec(c)
					Expect(err).Should(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring("recovery partition not found"))
				})
				It("fails to set defaults if no state partition detected", func() {
					mainDisk := sdkPartitions.Disk{
						Name:       "device",
						Partitions: []*sdkPartitions.Partition{},
					}
					ghwTest = ghwMock.GhwMock{}
					ghwTest.AddDisk(mainDisk)
					ghwTest.CreateDevices()
					defer ghwTest.Clean()

					bootedFrom = constants.SystemLabel
					_, err := config.NewResetSpec(c)
					Expect(err).Should(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring("state partition not found"))
				})
				It("fails to set defaults if no efi partition on efi firmware", func() {
					// Set EFI firmware detection
					err = fsutils.MkdirAll(fs, filepath.Dir(constants.EfiDevice), constants.DirPerm)
					Expect(err).ShouldNot(HaveOccurred())
					_, err = fs.Create(constants.EfiDevice)
					Expect(err).ShouldNot(HaveOccurred())

					bootedFrom = constants.SystemLabel
					_, err := config.NewResetSpec(c)
					Expect(err).Should(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring("EFI partition not found"))
				})
			})
		})
		Describe("UpgradeSpec", Label("upgrade"), func() {
			Describe("Successful executions", func() {
				var ghwTest ghwMock.GhwMock
				BeforeEach(func() {
					mainDisk := sdkPartitions.Disk{
						Name: "device",
						Partitions: []*sdkPartitions.Partition{
							{
								Name:            "device1",
								FilesystemLabel: constants.EfiLabel,
								FS:              "vfat",
							},
							{
								Name:            "device2",
								FilesystemLabel: constants.OEMLabel,
								FS:              "ext4",
							},
							{
								Name:            "device3",
								FilesystemLabel: constants.RecoveryLabel,
								FS:              "ext4",
								MountPoint:      constants.LiveDir,
							},
							{
								Name:            "device4",
								FilesystemLabel: constants.StateLabel,
								FS:              "ext4",
							},
							{
								Name:            "device5",
								FilesystemLabel: constants.PersistentLabel,
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
				It("sets upgrade defaults for active upgrade", func() {
					spec, err := config.NewUpgradeSpec(c)
					Expect(err).ShouldNot(HaveOccurred())
					Expect(spec.Active.Source.IsEmpty()).To(BeTrue())
				})
				It("sets upgrade defaults for non-squashed recovery upgrade", func() {
					spec, err := config.NewUpgradeSpec(c)
					Expect(err).ShouldNot(HaveOccurred())
					Expect(spec.Recovery.Source.IsEmpty()).To(BeTrue())
					Expect(spec.Recovery.FS).To(Equal(constants.LinuxImgFs))
				})
				It("sets upgrade defaults for squashed recovery upgrade", func() {
					//Set squashed recovery detection
					mounter.Mount("device3", constants.LiveDir, "auto", []string{})
					img := filepath.Join(constants.LiveDir, "cOS", constants.RecoverySquashFile)
					err = fsutils.MkdirAll(fs, filepath.Dir(img), constants.DirPerm)
					Expect(err).ShouldNot(HaveOccurred())
					_, err = fs.Create(img)
					Expect(err).ShouldNot(HaveOccurred())

					spec, err := config.NewUpgradeSpec(c)
					Expect(err).ShouldNot(HaveOccurred())
					Expect(spec.Recovery.Source.IsEmpty()).To(BeTrue())
					Expect(spec.Recovery.FS).To(Equal(constants.SquashFs))
				})

				It("sets image size to default value if not set", func() {
					spec, err := config.NewUpgradeSpec(c)
					Expect(err).ShouldNot(HaveOccurred())
					Expect(spec.Active.Size).To(Equal(sdkConstants.ImgSize))
				})

				It("sets image size to provided value if set in the config and image is smaller", func() {
					cfg, err := config.ScanNoLogs(collector.Readers(strings.NewReader("#cloud-config\nupgrade:\n  system:\n    size: 666\n")))
					// Set manually the config collector in the cfg file before unmarshalling the spec
					c.Collector = cfg.Collector
					spec, err := config.NewUpgradeSpec(c)
					Expect(err).ShouldNot(HaveOccurred())
					Expect(spec.Active.Size).To(Equal(uint(666)))
				})
				It("sets image size to default value if not set in the config and image is smaller", func() {
					cfg, err := config.ScanNoLogs(collector.Readers(strings.NewReader("#cloud-config\nupgrade:\n  system:\n    source: dir:/\n")))
					// Set manually the config collector in the cfg file before unmarshalling the spec
					c.Collector = cfg.Collector
					spec, err := config.NewUpgradeSpec(c)
					Expect(err).ShouldNot(HaveOccurred())
					Expect(spec.Active.Size).To(Equal(sdkConstants.ImgSize))
				})

				It("sets image size to the source if default is smaller", func() {
					cfg, err := config.ScanNoLogs(collector.Readers(strings.NewReader("#cloud-config\nupgrade:\n  system:\n    source: file:/tmp/waka\n")))
					// Set manually the config collector in the cfg file before unmarshalling the spec
					c.Collector = cfg.Collector
					Expect(c.Fs.Mkdir("/tmp", 0777)).ShouldNot(HaveOccurred())
					Expect(c.Fs.WriteFile("/tmp/waka", []byte("waka"), 0777)).ShouldNot(HaveOccurred())
					Expect(c.Fs.Truncate("/tmp/waka", 5120*1024*1024)).ShouldNot(HaveOccurred())
					spec, err := config.NewUpgradeSpec(c)
					Expect(err).ShouldNot(HaveOccurred())
					f, err := c.Fs.Stat("/tmp/waka")
					Expect(err).ShouldNot(HaveOccurred())
					// Make the same calculation as the code (uses binary megabytes /1024/1024)
					Expect(spec.Active.Size).To(Equal(uint(f.Size()/1024/1024) + 100))
				})

				It("parses deprecated 'uri' field into Source for backwards compatibility", func() {
					// Create a test file using the virtual file system
					Expect(c.Fs.Mkdir("/tmp", 0777)).ShouldNot(HaveOccurred())
					Expect(c.Fs.WriteFile("/tmp/testfile", []byte("test"), 0777)).ShouldNot(HaveOccurred())
					Expect(c.Fs.Truncate("/tmp/testfile", 1024*1024)).ShouldNot(HaveOccurred())

					cfg, err := config.ScanNoLogs(collector.Readers(strings.NewReader(`#cloud-config
upgrade:
  system:
    uri: file:/tmp/testfile
`)))
					Expect(err).ToNot(HaveOccurred())
					// Set manually the config collector in the cfg file before unmarshalling the spec
					c.Collector = cfg.Collector
					spec, err := config.NewUpgradeSpec(c)
					Expect(err).ToNot(HaveOccurred())
					Expect(spec.Active.Source).ToNot(BeNil())
					Expect(spec.Active.Source.Value()).To(Equal("/tmp/testfile"))
				})

				It("keeps the secure extractor by default", func() {
					spec, err := config.NewUpgradeSpec(c)
					Expect(err).ShouldNot(HaveOccurred())
					Expect(spec.AllowInsecureRegistries).To(BeFalse())
					Expect(c.ImageExtractor).To(Equal(imageextractor.OCIImageExtractor{Insecure: false}))
				})

				It("enables the insecure extractor when upgrade.allow-insecure-registries is set", func() {
					cfg, err := config.ScanNoLogs(collector.Readers(strings.NewReader("#cloud-config\nupgrade:\n  allow-insecure-registries: true\n")))
					Expect(err).ToNot(HaveOccurred())
					c.Collector = cfg.Collector
					spec, err := config.NewUpgradeSpec(c)
					Expect(err).ShouldNot(HaveOccurred())
					Expect(spec.AllowInsecureRegistries).To(BeTrue())
					Expect(c.ImageExtractor).To(Equal(imageextractor.OCIImageExtractor{Insecure: true}))
				})

				It("fails fast for uki upgrade when the container image cannot be resolved", func() {
					fake := &v1mock.FakeImageExtractor{
						Logger: logger,
						SizeSideEffect: func(imageRef, platformRef string) (int64, error) {
							return 0, fmt.Errorf("MANIFEST_UNKNOWN")
						},
					}
					ukiCfg := config.NewConfig(
						config.WithFs(fs),
						config.WithMounter(mounter),
						config.WithRunner(runner),
						config.WithLogger(logger),
						config.WithImageExtractor(fake),
						config.WithPlatform("linux/amd64"),
					)
					cc, err := config.ScanNoLogs(collector.Readers(strings.NewReader("#cloud-config\nupgrade:\n  system:\n    source: oci:missing/image:tag\n")))
					Expect(err).ToNot(HaveOccurred())
					ukiCfg.Collector = cc.Collector

					_, err = config.NewUkiUpgradeSpec(ukiCfg)
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring("could not resolve image"))
				})

			})
		})
		Describe("Config from cloudconfig", Label("cloud-config"), func() {
			var bootedFrom string
			var dir string
			var ghwTest ghwMock.GhwMock

			BeforeEach(func() {
				bootedFrom = ""
				runner.SideEffect = func(cmd string, args ...string) ([]byte, error) {
					switch cmd {
					case "cat":
						return []byte(bootedFrom), nil
					default:
						return []byte{}, nil
					}
				}

				dir, err = os.MkdirTemp("", "test-config")
				Expect(err).ToNot(HaveOccurred())
				ccdata := []byte(`#cloud-config
strict: true
install:
  device: /some/device
  skip_copy_kcrypt_plugin: true
  grub-entry-name: "MyCustomOS"
  system:
    size: 666
reset:
  reset-persistent: true
  reset-oem: true
  passive:
    label: MY_LABEL
upgrade:
  recovery: true
  system:
    source: oci:busybox
  recovery-system:
    source: oci:busybox
cloud-init-paths:
- /what
`)
				err = os.WriteFile(filepath.Join(dir, "cc.yaml"), ccdata, os.ModePerm)
				Expect(err).ToNot(HaveOccurred())

				mainDisk := sdkPartitions.Disk{
					Name: "device",
					Partitions: []*sdkPartitions.Partition{
						{
							Name:            "device1",
							FilesystemLabel: constants.EfiLabel,
							FS:              "vfat",
						},
						{
							Name:            "device2",
							FilesystemLabel: constants.OEMLabel,
							FS:              "ext4",
						},
						{
							Name:            "device3",
							FilesystemLabel: constants.RecoveryLabel,
							FS:              "ext4",
						},
						{
							Name:            "device4",
							FilesystemLabel: constants.StateLabel,
							FS:              "ext4",
						},
						{
							Name:            "device5",
							FilesystemLabel: constants.PersistentLabel,
							FS:              "ext4",
						},
					},
				}
				ghwTest = ghwMock.GhwMock{}
				ghwTest.AddDisk(mainDisk)
				ghwTest.CreateDevices()

				fs, cleanup, err = vfst.NewTestFS(nil)
				setupIsoBaseTreeDetection(fs)
			})

			AfterEach(func() {
				os.RemoveAll(dir)
				ghwTest.Clean()
			})
			It("Reads properly the cloud config for install", func() {
				cfg, err := config.ScanNoLogs(collector.Directories([]string{dir}...),
					collector.NoLogs,
				)
				cfg.Fs = fs
				cfg.Logger = logger

				Expect(err).ToNot(HaveOccurred())
				// Once we got the cfg override the fs to our test fs
				cfg.Runner = runner
				cfg.Fs = fs
				cfg.Mounter = mounter
				cfg.CloudInitRunner = ci
				installSpec, err := config.ReadInstallSpecFromConfig(cfg)
				Expect(err).ToNot(HaveOccurred())
				Expect(cfg.Strict).To(BeTrue())
				Expect(cfg.Install.SkipEncryptCopyPlugins).To(BeTrue())
				Expect(cfg.Install.Device).To(Equal("/some/device"))
				Expect(installSpec.Target).To(Equal("/some/device"))
				Expect(installSpec.GrubDefEntry).To(Equal("MyCustomOS"))
				Expect(installSpec.Active.Size).To(Equal(uint(666)))
				Expect(cfg.CloudInitPaths).To(ContainElement("/what"))

			})
			It("Skips device-mapper devices when auto-detecting the largest install device", func() {
				// A multipath map (dm-0) over a single-path disk shows up in
				// /sys/block alongside the disk itself and must never be picked
				// as install target: the agent deactivates dm devices via
				// blkdeactivate right before partitioning.
				ghwTest.Clean()
				ghwTest = ghwMock.GhwMock{}
				ghwTest.AddDisk(sdkPartitions.Disk{Name: "sda", SizeBytes: 100})
				ghwTest.AddDisk(sdkPartitions.Disk{
					Name:      "dm-0",
					SizeBytes: 200,
					UUID:      "mpath-3600508b1001c7e72",
				})
				ghwTest.CreateDevices()

				autoDir, err := os.MkdirTemp("", "kairos-auto-device-test-*")
				Expect(err).ToNot(HaveOccurred())
				defer os.RemoveAll(autoDir)
				ccdata := []byte("#cloud-config\ninstall:\n  auto: true\n")
				Expect(os.WriteFile(filepath.Join(autoDir, "cc.yaml"), ccdata, os.ModePerm)).To(Succeed())

				cfg, err := config.ScanNoLogs(collector.Directories([]string{autoDir}...), collector.NoLogs)
				Expect(err).ToNot(HaveOccurred())
				cfg.Runner = runner
				cfg.Fs = fs
				cfg.Mounter = mounter
				cfg.CloudInitRunner = ci
				cfg.Logger = logger
				installSpec, err := config.ReadInstallSpecFromConfig(cfg)
				Expect(err).ToNot(HaveOccurred())
				Expect(installSpec.Target).To(Equal("/dev/sda"))
			})
			It("Resolves install.device via script:// and uses its stdout as the target", func() {
				script, err := os.CreateTemp("", "pick-disk-*.sh")
				Expect(err).ToNot(HaveOccurred())
				defer os.Remove(script.Name())
				_, err = script.WriteString("#!/bin/sh\necho /some/device\n")
				Expect(err).ToNot(HaveOccurred())
				Expect(script.Close()).To(Succeed())
				Expect(os.Chmod(script.Name(), 0755)).To(Succeed())

				scriptDir, err := os.MkdirTemp("", "kairos-script-test-*")
				Expect(err).ToNot(HaveOccurred())
				defer os.RemoveAll(scriptDir)
				ccdata := fmt.Sprintf("#cloud-config\ninstall:\n  device: \"script://%s\"\n", script.Name())
				Expect(os.WriteFile(filepath.Join(scriptDir, "cc.yaml"), []byte(ccdata), os.ModePerm)).To(Succeed())

				cfg, err := config.ScanNoLogs(collector.Directories([]string{scriptDir}...), collector.NoLogs)
				Expect(err).ToNot(HaveOccurred())
				cfg.Runner = runner
				cfg.Fs = fs
				cfg.Mounter = mounter
				cfg.CloudInitRunner = ci
				cfg.Logger = logger
				installSpec, err := config.ReadInstallSpecFromConfig(cfg)
				Expect(err).ToNot(HaveOccurred())
				Expect(installSpec.Target).To(Equal("/some/device"))
			})
			It("Returns an error when the script:// script fails for install.device", func() {
				script, err := os.CreateTemp("", "bad-disk-*.sh")
				Expect(err).ToNot(HaveOccurred())
				defer os.Remove(script.Name())
				_, err = script.WriteString("#!/bin/sh\necho 'no disk available' >&2\nexit 1\n")
				Expect(err).ToNot(HaveOccurred())
				Expect(script.Close()).To(Succeed())
				Expect(os.Chmod(script.Name(), 0755)).To(Succeed())

				scriptDir, err := os.MkdirTemp("", "kairos-script-test-*")
				Expect(err).ToNot(HaveOccurred())
				defer os.RemoveAll(scriptDir)
				ccdata := fmt.Sprintf("#cloud-config\ninstall:\n  device: \"script://%s\"\n", script.Name())
				Expect(os.WriteFile(filepath.Join(scriptDir, "cc.yaml"), []byte(ccdata), os.ModePerm)).To(Succeed())

				cfg, err := config.ScanNoLogs(collector.Directories([]string{scriptDir}...), collector.NoLogs)
				Expect(err).ToNot(HaveOccurred())
				cfg.Runner = runner
				cfg.Fs = fs
				cfg.Mounter = mounter
				cfg.CloudInitRunner = ci
				cfg.Logger = logger
				_, err = config.ReadInstallSpecFromConfig(cfg)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("no disk available"))
			})
			It("Reads properly the cloud config for reset", func() {
				bootedFrom = constants.SystemLabel
				cfg, err := config.ScanNoLogs(collector.Directories([]string{dir}...), collector.NoLogs)
				Expect(err).ToNot(HaveOccurred())
				// Override the config with our test params
				cfg.Runner = runner
				cfg.Fs = fs
				cfg.Mounter = mounter
				cfg.CloudInitRunner = ci
				cfg.Logger = logger
				spec, err := config.ReadSpecFromCloudConfig(cfg, "reset")
				Expect(err).ToNot(HaveOccurred())
				resetSpec := spec.(*v1.ResetSpec)
				Expect(resetSpec.FormatPersistent).To(BeTrue())
				Expect(resetSpec.FormatOEM).To(BeTrue())
				Expect(resetSpec.Passive.Label).To(Equal("MY_LABEL"))
			})
			It("Reads properly the cloud config for upgrade", func() {
				cfg, err := config.ScanNoLogs(collector.Directories([]string{dir}...), collector.NoLogs)
				Expect(err).ToNot(HaveOccurred())
				// Override the config with our test params
				cfg.Runner = runner
				cfg.Fs = fs
				cfg.Mounter = mounter
				cfg.CloudInitRunner = ci
				cfg.Logger = logger
				spec, err := config.ReadSpecFromCloudConfig(cfg, "upgrade")
				Expect(err).ToNot(HaveOccurred())
				upgradeSpec := spec.(*v1.UpgradeSpec)
				Expect(upgradeSpec.RecoveryUpgrade()).To(BeTrue())
			})
			It("Fails when a wrong action is read", func() {
				cfg, err := config.ScanNoLogs(collector.Directories([]string{dir}...), collector.NoLogs)
				cfg.Logger = logger
				Expect(err).ToNot(HaveOccurred())
				_, err = config.ReadSpecFromCloudConfig(cfg, "nope")
				Expect(err).To(HaveOccurred())
			})
			It("Sets debug level if its on the cloud-config", func() {
				ccdata := []byte(`#cloud-config
debug: true
`)
				err = os.WriteFile(filepath.Join(dir, "cc.yaml"), ccdata, os.ModePerm)
				Expect(err).ToNot(HaveOccurred())
				cfg, err := config.Scan(collector.Directories([]string{dir}...))
				Expect(err).ToNot(HaveOccurred())
				fmt.Println(litter.Sdump(cfg))
				Expect(cfg.Logger.GetLevel()).To(Equal(zerolog.DebugLevel))

			})
		})
		Describe("TestBootedFrom", Label("BootedFrom"), func() {
			It("returns true if we are booting from label FAKELABEL", func() {
				runner.ReturnValue = []byte("")
				Expect(config.BootedFrom(runner, "FAKELABEL")).To(BeFalse())
			})
			It("returns false if we are not booting from label FAKELABEL", func() {
				runner.ReturnValue = []byte("FAKELABEL")
				Expect(config.BootedFrom(runner, "FAKELABEL")).To(BeTrue())
			})
		})
	})
})

func createFileOfSizeInMB(filename string, sizeInMB int) error {
	// Calculate the number of bytes needed to reach the desired size in megabytes
	fileSizeInBytes := int64(sizeInMB) * 1024 * 1024

	// Create the file
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	// Seek to the desired file size
	_, err = file.Seek(fileSizeInBytes-1, 0)
	if err != nil {
		return err
	}

	// Write a single byte to "expand" the file to the desired size
	_, err = file.Write([]byte{0})
	if err != nil {
		return err
	}

	return nil
}

func setupIsoBaseTreeDetection(fs *vfst.TestFS) {
	err := fsutils.MkdirAll(fs, filepath.Dir(constants.IsoBaseTree), constants.DirPerm)
	Expect(err).ShouldNot(HaveOccurred())
	_, err = fs.Create(constants.IsoBaseTree)
	Expect(err).ShouldNot(HaveOccurred())
}

var _ = Describe("GetSourceSize", Label("GetSourceSize"), func() {
	var tempDir string
	var tempFilePath string
	var err error
	var logger sdkLogger.KairosLogger
	var conf *sdkConfig.Config
	var imageSource *sdkImages.ImageSource
	var memLog bytes.Buffer

	BeforeEach(func() {
		tempDir, err = os.MkdirTemp("/tmp", "kairos-test")
		Expect(err).To(BeNil())

		//logger = sdkTypes.NewNullLogger()
		memLog = bytes.Buffer{}
		logger = sdkLogger.NewBufferLogger(&memLog)
		logger.SetLevel("debug")
		conf = config.NewConfig(
			config.WithLogger(logger),
		)

		tempFilePath = filepath.Join(tempDir, "200MB.txt")
		err := createFileOfSizeInMB(tempFilePath, 200)
		Expect(err).To(BeNil())

		imageSource = sdkImages.NewDirSrc(tempDir)
	})

	AfterEach(func() {
		defer os.RemoveAll(tempDir)
	})

	It("doesn't count symlinks more than once", func() {
		sizeBefore, err := config.GetSourceSize(conf, imageSource)
		Expect(err).To(BeNil())
		Expect(sizeBefore).ToNot(BeZero())

		err = os.Symlink(tempFilePath, filepath.Join(tempDir, "200MB-symlink.txt"))
		Expect(err).To(BeNil())

		sizeAfter, err := config.GetSourceSize(conf, imageSource)
		Expect(err).ToNot(HaveOccurred())
		Expect(sizeAfter).To(Equal(sizeBefore))
	})
	It("Skips the kubernetes host dir when calculating the sizes if set", func() {
		sizeBefore, err := config.GetSourceSize(conf, imageSource)
		Expect(err).To(BeNil())
		Expect(sizeBefore).ToNot(BeZero())

		Expect(os.Mkdir(filepath.Join(tempDir, "host"), os.ModePerm)).ToNot(HaveOccurred())
		Expect(os.Mkdir(filepath.Join(tempDir, "host", "one"), os.ModePerm)).ToNot(HaveOccurred())
		Expect(os.Mkdir(filepath.Join(tempDir, "host", "two"), os.ModePerm)).ToNot(HaveOccurred())
		Expect(createFileOfSizeInMB(filepath.Join(tempDir, "host", "what.txt"), 200)).ToNot(HaveOccurred())
		// Set env var like the suc upgrade and k8s does to trigger the skip
		Expect(os.Setenv("KUBERNETES_SERVICE_HOST", "10.0.0.1")).ToNot(HaveOccurred())
		Expect(os.Setenv("HOST_DIR", filepath.Join(tempDir, "host"))).ToNot(HaveOccurred())

		sizeAfter, err := config.GetSourceSize(conf, imageSource)
		Expect(err).ToNot(HaveOccurred())
		Expect(sizeAfter).To(Equal(sizeBefore))
		// We log that we are skipping the host dir
		Expect(memLog.String()).To(ContainSubstring("Skipping dir as it is a host directory"))
		Expect(memLog.String()).To(ContainSubstring(filepath.Join(tempDir, "host")))
		// We also log the dirs we are skipping inside the host dir, we expect those to NOT shown up as we skipped the full dir
		Expect(memLog.String()).ToNot(ContainSubstring(filepath.Join(tempDir, "host", "one")))
		Expect(memLog.String()).ToNot(ContainSubstring(filepath.Join(tempDir, "host", "two")))
	})
	It("Counts the kubernetes host dir when calculating the sizes if not set", func() {
		sizeBefore, err := config.GetSourceSize(conf, imageSource)
		Expect(err).To(BeNil())
		Expect(sizeBefore).ToNot(BeZero())

		Expect(os.Mkdir(filepath.Join(tempDir, "host"), os.ModePerm)).ToNot(HaveOccurred())
		Expect(createFileOfSizeInMB(filepath.Join(tempDir, "host", "what.txt"), 200)).ToNot(HaveOccurred())

		sizeAfter, err := config.GetSourceSize(conf, imageSource)
		Expect(err).ToNot(HaveOccurred())
		Expect(sizeAfter).ToNot(Equal(sizeBefore))
		Expect(sizeAfter).ToNot(BeZero())
		// Size is 2 files of 200 + 100MB on top, normalized from bytes to MB
		// So take those 200MB, converts to bytes by multiplying them (400*1024*1024), then back to MB by dividing
		// what we get (/1024/1024) then we finish by adding and extra 100MB on top, like the GetSourceSize does internally
		Expect(sizeAfter).To(Equal(int64(400 + 100)))
	})
	It("Does not skip the dirs if outside of kubernetes", func() {
		sizeBefore, err := config.GetSourceSize(conf, imageSource)
		Expect(err).To(BeNil())
		Expect(sizeBefore).ToNot(BeZero())

		// Not inside kubernetes so it should count this dir
		Expect(os.Mkdir(filepath.Join(tempDir, "run"), os.ModePerm)).ToNot(HaveOccurred())
		Expect(createFileOfSizeInMB(filepath.Join(tempDir, "run", "what.txt"), 200)).ToNot(HaveOccurred())

		sizeAfter, err := config.GetSourceSize(conf, imageSource)
		Expect(err).ToNot(HaveOccurred())
		Expect(sizeAfter).ToNot(Equal(sizeBefore))
		Expect(sizeAfter).ToNot(BeZero())
		Expect(sizeAfter).To(Equal(int64(400 + 100)))
	})

	It("calculates size for ocifile sources with 2x multiplier", func() {
		// Create a test OCI tar file
		ociTarFile := filepath.Join(tempDir, "test-image.tar")
		err := createFileOfSizeInMB(ociTarFile, 100) // 100MB tar file
		Expect(err).To(BeNil())

		// Create ocifile source
		ociSource := sdkImages.NewOCIFileSrc(ociTarFile)

		// Calculate size
		size, err := config.GetSourceSize(conf, ociSource)
		Expect(err).To(BeNil())
		Expect(size).ToNot(BeZero())

		// Expected: (100MB * 2.0 multiplier) + 100MB normalization = 300MB
		expectedSize := int64((100 * 2.0) + 100)
		Expect(size).To(Equal(expectedSize))
	})

	It("handles missing ocifile gracefully", func() {
		// Create ocifile source pointing to non-existent file
		ociSource := sdkImages.NewOCIFileSrc("/non/existent/file.tar")

		// Should return error
		size, err := config.GetSourceSize(conf, ociSource)
		Expect(err).ToNot(BeNil())
		Expect(size).To(Equal(int64(0)))
	})
})
