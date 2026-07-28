/*
Copyright © 2026 Kairos authors

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
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/implementations/spec"
	fsutils "github.com/kairos-io/kairos-agent/v2/pkg/utils/fs"
	v1mock "github.com/kairos-io/kairos-agent/v2/tests/mocks"
	"github.com/kairos-io/kairos-sdk/collector"
	sdkConstants "github.com/kairos-io/kairos-sdk/constants"
	ghwMock "github.com/kairos-io/kairos-sdk/ghw/mocks"
	sdkConfig "github.com/kairos-io/kairos-sdk/types/config"
	sdkImages "github.com/kairos-io/kairos-sdk/types/images"
	sdkInstall "github.com/kairos-io/kairos-sdk/types/install"
	sdkLogger "github.com/kairos-io/kairos-sdk/types/logger"
	sdkPartitions "github.com/kairos-io/kairos-sdk/types/partitions"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/rs/zerolog"
	"github.com/spf13/viper"
	"github.com/twpayne/go-vfs/v5/vfst"
)

// createDMDevice creates the sysfs/udev files inside the test FS that
// GetPartitionViaDM needs to detect a device-mapper backed partition.
// It writes the files under the currently set GHW_CHROOT prefix, since
// ghw.NewPaths gives precedence to that env var when building the paths.
func createDMDevice(fs *vfst.TestFS, dmName, devNumber, label string) {
	chroot := os.Getenv("GHW_CHROOT")
	Expect(chroot).ToNot(BeEmpty())
	dmDir := filepath.Join(chroot, "sys", "block", dmName)
	Expect(fsutils.MkdirAll(fs, filepath.Join(dmDir, "queue"), constants.DirPerm)).To(Succeed())
	Expect(fs.WriteFile(filepath.Join(dmDir, "dev"), []byte(devNumber+"\n"), constants.FilePerm)).To(Succeed())
	Expect(fs.WriteFile(filepath.Join(dmDir, "size"), []byte("8192\n"), constants.FilePerm)).To(Succeed())
	Expect(fs.WriteFile(filepath.Join(dmDir, "queue", "logical_block_size"), []byte("512\n"), constants.FilePerm)).To(Succeed())
	udevDir := filepath.Join(chroot, "run", "udev", "data")
	Expect(fsutils.MkdirAll(fs, udevDir, constants.DirPerm)).To(Succeed())
	udevData := fmt.Sprintf("E:ID_FS_LABEL=%s\nE:ID_FS_TYPE=ext4\nE:DM_NAME=%s\n", label, dmName)
	Expect(fs.WriteFile(filepath.Join(udevDir, "b"+devNumber), []byte(udevData), constants.FilePerm)).To(Succeed())
}

var _ = Describe("Config helpers", Label("config", "helpers"), func() {
	Describe("AddHeader", func() {
		It("prepends the header to the data", func() {
			Expect(config.AddHeader("#cloud-config", "foo: bar")).To(Equal("#cloud-config\nfoo: bar"))
		})
	})
	Describe("Stage", func() {
		It("returns the proper string representation", func() {
			Expect(config.NetworkStage.String()).To(Equal("network"))
			Expect(config.InitramfsStage.String()).To(Equal("initramfs"))
		})
	})
	Describe("MergeYAML", func() {
		It("merges different objects into a single yaml", func() {
			out, err := config.MergeYAML(
				map[string]interface{}{"foo": "bar"},
				map[string]interface{}{"baz": "qux"},
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(out)).To(ContainSubstring("foo: bar"))
			Expect(string(out)).To(ContainSubstring("baz: qux"))
		})
		It("last object wins on common keys", func() {
			out, err := config.MergeYAML(
				map[string]interface{}{"foo": "bar"},
				map[string]interface{}{"foo": "win"},
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(out)).To(ContainSubstring("foo: win"))
		})
		It("fails to merge non-map objects", func() {
			_, err := config.MergeYAML("just-a-plain-string")
			Expect(err).To(HaveOccurred())
		})
	})
	Describe("FilterKeys", func() {
		It("filters the config keys", func() {
			out, err := config.FilterKeys([]byte("install:\n  device: /dev/sda\n"))
			Expect(err).ToNot(HaveOccurred())
			Expect(string(out)).To(ContainSubstring("device: /dev/sda"))
		})
		It("fails on invalid yaml", func() {
			_, err := config.FilterKeys([]byte("install: [unclosed"))
			Expect(err).To(HaveOccurred())
		})
	})
	Describe("CheckConfigForExtraPartitions", func() {
		It("passes when no extra partitions are defined", func() {
			c := &sdkConfig.Config{Install: &sdkInstall.Install{}}
			Expect(config.CheckConfigForExtraPartitions(c)).To(Succeed())
		})
		It("passes when extra partitions have names", func() {
			c := &sdkConfig.Config{Install: &sdkInstall.Install{
				ExtraPartitions: sdkPartitions.PartitionList{
					{Name: "myPartition", Size: 100},
				},
			}}
			Expect(config.CheckConfigForExtraPartitions(c)).To(Succeed())
		})
		It("fails when extra partitions have no name", func() {
			c := &sdkConfig.Config{Install: &sdkInstall.Install{
				ExtraPartitions: sdkPartitions.PartitionList{
					{Size: 100},
				},
			}}
			err := config.CheckConfigForExtraPartitions(c)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("without a name"))
		})
	})
	Describe("CheckConfigForUsers extra paths", func() {
		var fs *vfst.TestFS
		var cleanup func()
		BeforeEach(func() {
			var err error
			fs, cleanup, err = vfst.NewTestFS(nil)
			Expect(err).ToNot(HaveOccurred())
		})
		AfterEach(func() {
			cleanup()
		})
		It("skips the check if the nousers sentinel is present", func() {
			Expect(fsutils.MkdirAll(fs, "/etc/kairos", constants.DirPerm)).To(Succeed())
			Expect(fs.WriteFile("/etc/kairos/.nousers", []byte{}, constants.FilePerm)).To(Succeed())
			c := config.NewConfig(config.WithFs(fs))
			Expect(config.CheckConfigForUsers(c)).To(Succeed())
		})
		It("skips the check if nousers is set in the install config", func() {
			c := config.NewConfig(config.WithFs(fs))
			c.Install.NoUsers = true
			Expect(config.CheckConfigForUsers(c)).To(Succeed())
		})
		It("fails if the config cannot be loaded as yip stages", func() {
			c := config.NewConfig(config.WithFs(fs))
			c.Collector = collector.Config{Values: collector.ConfigValues{
				"stages": "not-a-map",
			}}
			Expect(config.CheckConfigForUsers(c)).ToNot(Succeed())
		})
	})
	Describe("NewConfig", func() {
		It("sets debug level when viper debug is set", func() {
			viper.Set("debug", true)
			defer viper.Set("debug", false)
			c := config.NewConfig()
			Expect(c).ToNot(BeNil())
			Expect(c.Logger.GetLevel()).To(Equal(zerolog.DebugLevel))
		})
		It("sets the image extractor", func() {
			extractor := v1mock.NewFakeImageExtractor(sdkLogger.NewNullLogger())
			c := config.NewConfig(config.WithImageExtractor(extractor))
			Expect(c).ToNot(BeNil())
			Expect(c.ImageExtractor).To(Equal(extractor))
		})
		It("keeps the compression options if compression is enabled", func() {
			c := config.NewConfig(func(c *sdkConfig.Config) {
				c.SquashFsNoCompression = false
			})
			Expect(c).ToNot(BeNil())
			Expect(c.SquashFsCompressionConfig).To(Equal(constants.GetDefaultSquashfsCompressionOptions()))
		})
		It("derives the platform from the arch if the platform is missing", func() {
			c := config.NewConfig(func(c *sdkConfig.Config) {
				c.Platform = nil
				c.Arch = "arm64"
			})
			Expect(c).ToNot(BeNil())
			Expect(c.Platform).ToNot(BeNil())
			Expect(c.Platform.GolangArch).To(Equal("arm64"))
		})
		It("leaves the platform empty on a bogus arch", func() {
			c := config.NewConfig(func(c *sdkConfig.Config) {
				c.Platform = nil
				c.Arch = "bogus-arch"
			})
			Expect(c).ToNot(BeNil())
			Expect(c.Platform).To(BeNil())
		})
		It("falls back to the host platform if arch and platform are unset", func() {
			c := config.NewConfig(func(c *sdkConfig.Config) {
				c.Platform = nil
				c.Arch = ""
			})
			Expect(c).ToNot(BeNil())
			Expect(c.Platform).ToNot(BeNil())
		})
	})
	Describe("Scan error paths", func() {
		It("fails when a collector option cannot be applied", func() {
			_, err := config.ScanNoLogs(func(o *collector.Options) error {
				return fmt.Errorf("option boom")
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("option boom"))
		})
		It("fails when the scanned config cannot be unmarshalled into the config struct", func() {
			_, err := config.ScanNoLogs(collector.Readers(strings.NewReader("#cloud-config\nstrict: [1, 2]\n")))
			Expect(err).To(HaveOccurred())
		})
		It("fails on invalid schema when strict validation is on", func() {
			_, err := config.ScanNoLogs(
				collector.Readers(strings.NewReader("#cloud-config\nusers:\n- passwd: foo\n")),
				collector.StrictValidation(true),
			)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("ERROR"))
		})
		It("only warns on invalid schema when strict validation is off", func() {
			c, err := config.Scan(
				collector.Readers(strings.NewReader("#cloud-config\nusers:\n- passwd: foo\n")),
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(c).ToNot(BeNil())
		})
		It("fails on unparseable schema when strict validation is on", func() {
			_, err := config.ScanNoLogs(
				collector.Readers(strings.NewReader("#cloud-config\nusers: not-a-list\n")),
				collector.StrictValidation(true),
			)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("ERROR"))
		})
		It("only warns on unparseable schema when strict validation is off", func() {
			c, err := config.Scan(
				collector.Readers(strings.NewReader("#cloud-config\nusers: not-a-list\n")),
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(c).ToNot(BeNil())
		})
	})
})

var _ = Describe("Specs coverage", Label("types", "config"), func() {
	var err error
	var cleanup func()
	var fs *vfst.TestFS
	var mounter *v1mock.ErrorMounter
	var runner *v1mock.FakeRunner
	var client *v1mock.FakeHTTPClient
	var sysc *v1mock.FakeSyscall
	var logger sdkLogger.KairosLogger
	var ci *v1mock.FakeCloudInitRunner
	var extractor *v1mock.FakeImageExtractor
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
		extractor = v1mock.NewFakeImageExtractor(logger)
		c = config.NewConfig(
			config.WithFs(fs),
			config.WithMounter(mounter),
			config.WithRunner(runner),
			config.WithSyscall(sysc),
			config.WithLogger(logger),
			config.WithCloudInitRunner(ci),
			config.WithClient(client),
			config.WithImageExtractor(extractor),
			config.WithPlatform("linux/amd64"),
		)
		c.Install = &sdkInstall.Install{}
		c.Collector = collector.Config{}
	})
	AfterEach(func() {
		cleanup()
	})

	Describe("resolveTarget via NewInstallSpec", Label("install"), func() {
		It("fails if the target is a partlabel or partuuid", func() {
			c.Install.Device = "/dev/disk/by-partlabel/mypart"
			_, err := config.NewInstallSpec(c)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("looks like its a partition"))
		})
		It("fails if the by-X disk does not exist", func() {
			c.Install.Device = "/dev/disk/by-label/DOES-NOT-EXIST-KAIROS-TEST"
			_, err := config.NewInstallSpec(c)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to read device link"))
		})
	})

	Describe("NewInstallSpec error paths", Label("install"), func() {
		It("fails when the config cannot be unmarshalled into the spec", func() {
			c.Collector = collector.Config{Values: collector.ConfigValues{
				"install": collector.ConfigValues{
					"system": collector.ConfigValues{
						"size": "not-a-number",
					},
				},
			}}
			_, err := config.NewInstallSpec(c)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed unmarshalling the full spec"))
		})
	})

	Describe("ReadSpecFromCloudConfig sanitize errors", Label("install"), func() {
		It("fails to sanitize an install spec without source", func() {
			_, err := config.ReadSpecFromCloudConfig(c, "install")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("sanitizing the install spec"))
		})
	})

	Describe("NewInstallElementalPartitions", Label("install"), func() {
		It("respects user provided sizes when they are big enough", func() {
			spec := &v1.InstallSpec{
				Active:   sdkImages.Image{Size: 100},
				Passive:  sdkImages.Image{Size: 100},
				Recovery: sdkImages.Image{Size: 100},
				Partitions: sdkPartitions.ElementalPartitions{
					OEM:        &sdkPartitions.Partition{Size: 64},
					Recovery:   &sdkPartitions.Partition{Size: 10000},
					State:      &sdkPartitions.Partition{Size: 10000},
					Persistent: &sdkPartitions.Partition{Size: 500},
				},
			}
			pt := config.NewInstallElementalPartitions(logger, spec)
			Expect(pt.OEM.Size).To(Equal(uint(64)))
			Expect(pt.Recovery.Size).To(Equal(uint(10000)))
			Expect(pt.State.Size).To(Equal(uint(10000)))
			Expect(pt.Persistent.Size).To(Equal(uint(500)))
		})
		It("increases user provided sizes when they don't fit the images", func() {
			spec := &v1.InstallSpec{
				Active:   sdkImages.Image{Size: 100},
				Passive:  sdkImages.Image{Size: 100},
				Recovery: sdkImages.Image{Size: 100},
				Partitions: sdkPartitions.ElementalPartitions{
					Recovery: &sdkPartitions.Partition{Size: 10},
					State:    &sdkPartitions.Partition{Size: 10},
				},
			}
			pt := config.NewInstallElementalPartitions(logger, spec)
			// recovery = (recovery image size * 2) + 200
			Expect(pt.Recovery.Size).To(Equal(uint(400)))
			// state = (active image size * 2) + passive image size + 1000
			Expect(pt.State.Size).To(Equal(uint(1300)))
			// defaults for unset partitions
			Expect(pt.OEM.Size).To(Equal(sdkConstants.OEMSize))
			Expect(pt.Persistent.Size).To(Equal(sdkConstants.PersistentSize))
		})
	})

	Describe("UpgradeSpec extra paths", Label("upgrade"), func() {
		var ghwTest ghwMock.GhwMock
		AfterEach(func() {
			ghwTest.Clean()
		})
		It("builds an upgrade spec without recovery, oem or persistent partitions", func() {
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

			spec, err := config.NewUpgradeSpec(c)
			Expect(err).ToNot(HaveOccurred())
			Expect(spec.Partitions.Recovery).To(BeNil())
			Expect(spec.Partitions.OEM).To(BeNil())
			Expect(spec.Partitions.Persistent).To(BeNil())
			Expect(spec.Active.File).To(ContainSubstring(constants.TransitionImgFile))
		})
		It("fails when a temporary dir cannot be created to check squashed recovery", func() {
			mainDisk := sdkPartitions.Disk{
				Name: "device",
				Partitions: []*sdkPartitions.Partition{
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
				},
			}
			ghwTest = ghwMock.GhwMock{}
			ghwTest.AddDisk(mainDisk)
			ghwTest.CreateDevices()

			// Create a regular file on the temp dir path so the temporary dir
			// cannot be created
			_, err := fs.Create(os.TempDir())
			Expect(err).ToNot(HaveOccurred())

			_, err = config.NewUpgradeSpec(c)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed checking for squashed recovery"))
		})
		It("fails when the recovery partition cannot be mounted to check squashed recovery", func() {
			mainDisk := sdkPartitions.Disk{
				Name: "device",
				Partitions: []*sdkPartitions.Partition{
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
				},
			}
			ghwTest = ghwMock.GhwMock{}
			ghwTest.AddDisk(mainDisk)
			ghwTest.CreateDevices()

			mounter.ErrorOnMount = true
			_, err := config.NewUpgradeSpec(c)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed checking for squashed recovery"))
		})
		It("flags recovery as non-squashed if the temporary mount cannot be unmounted", func() {
			mainDisk := sdkPartitions.Disk{
				Name: "device",
				Partitions: []*sdkPartitions.Partition{
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
				},
			}
			ghwTest = ghwMock.GhwMock{}
			ghwTest.AddDisk(mainDisk)
			ghwTest.CreateDevices()

			mounter.ErrorOnUnmount = true
			spec, err := config.NewUpgradeSpec(c)
			Expect(err).ToNot(HaveOccurred())
			Expect(spec.Recovery.FS).To(Equal(sdkConstants.LinuxImgFs))
		})
		It("fails when the source size cannot be calculated", func() {
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

			cfg, err := config.ScanNoLogs(collector.Readers(strings.NewReader("#cloud-config\nupgrade:\n  system:\n    source: file:/does/not/exist\n")))
			Expect(err).ToNot(HaveOccurred())
			c.Collector = cfg.Collector
			_, err = config.NewUpgradeSpec(c)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed calculating size"))
		})
		It("fails when the config cannot be unmarshalled into the spec", func() {
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

			cfg, err := config.ScanNoLogs(collector.Readers(strings.NewReader("#cloud-config\nupgrade:\n  system:\n    size: not-a-number\n")))
			Expect(err).ToNot(HaveOccurred())
			c.Collector = cfg.Collector
			_, err = config.NewUpgradeSpec(c)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed unmarshalling the full spec"))
		})
	})

	Describe("UnmarshalerHook nil source", Label("upgrade"), func() {
		var ghwTest ghwMock.GhwMock
		AfterEach(func() {
			ghwTest.Clean()
		})
		It("creates the image source when unmarshalling into a nil pointer", func() {
			// No recovery partition, so the recovery image source pointer is nil
			// when the config is unmarshalled into the spec
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

			Expect(fsutils.MkdirAll(fs, "/tmp", constants.DirPerm)).To(Succeed())
			Expect(fs.WriteFile("/tmp/waka", []byte("waka"), constants.FilePerm)).To(Succeed())
			cfg, err := config.ScanNoLogs(collector.Readers(strings.NewReader("#cloud-config\nupgrade:\n  recovery-system:\n    source: file:/tmp/waka\n")))
			Expect(err).ToNot(HaveOccurred())
			c.Collector = cfg.Collector
			spec, err := config.NewUpgradeSpec(c)
			Expect(err).ToNot(HaveOccurred())
			Expect(spec.Recovery.Source).ToNot(BeNil())
			Expect(spec.Recovery.Source.Value()).To(Equal("/tmp/waka"))
		})
	})

	Describe("ReadUpgradeSpecFromConfig", Label("upgrade"), func() {
		var ghwTest ghwMock.GhwMock
		BeforeEach(func() {
			mainDisk := sdkPartitions.Disk{
				Name: "device",
				Partitions: []*sdkPartitions.Partition{
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
				},
			}
			ghwTest = ghwMock.GhwMock{}
			ghwTest.AddDisk(mainDisk)
			ghwTest.CreateDevices()
		})
		AfterEach(func() {
			ghwTest.Clean()
		})
		It("returns an upgrade spec from the config", func() {
			Expect(fsutils.MkdirAll(fs, "/tmp", constants.DirPerm)).To(Succeed())
			Expect(fs.WriteFile("/tmp/waka", []byte("waka"), constants.FilePerm)).To(Succeed())
			cfg, err := config.ScanNoLogs(collector.Readers(strings.NewReader("#cloud-config\nupgrade:\n  system:\n    source: file:/tmp/waka\n")))
			Expect(err).ToNot(HaveOccurred())
			c.Collector = cfg.Collector
			spec, err := config.ReadUpgradeSpecFromConfig(c)
			Expect(err).ToNot(HaveOccurred())
			Expect(spec.Active.Source.Value()).To(Equal("/tmp/waka"))
		})
		It("fails when the spec cannot be initialized", func() {
			cfg, err := config.ScanNoLogs(collector.Readers(strings.NewReader("#cloud-config\nupgrade:\n  system:\n    size: not-a-number\n")))
			Expect(err).ToNot(HaveOccurred())
			c.Collector = cfg.Collector
			_, err = config.ReadUpgradeSpecFromConfig(c)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed initializing spec"))
		})
	})

	Describe("ResetSpec extra paths", Label("reset"), func() {
		var ghwTest ghwMock.GhwMock
		BeforeEach(func() {
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
		It("uses the recovery image from the isoscan dir if present", func() {
			mainDisk := sdkPartitions.Disk{
				Name: "device",
				Partitions: []*sdkPartitions.Partition{
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

			recoveryImg2 := filepath.Join(constants.RunningRecoveryStateDir, "cOS", constants.RecoveryImgFile)
			Expect(fsutils.MkdirAll(fs, filepath.Dir(recoveryImg2), constants.DirPerm)).To(Succeed())
			_, err = fs.Create(recoveryImg2)
			Expect(err).ToNot(HaveOccurred())

			spec, err := config.NewResetSpec(c)
			Expect(err).ToNot(HaveOccurred())
			Expect(spec.Active.Source.Value()).To(Equal(recoveryImg2))
		})
		It("finds OEM and Persistent partitions via device mapper", func() {
			mainDisk := sdkPartitions.Disk{
				Name: "device",
				Partitions: []*sdkPartitions.Partition{
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
				},
			}
			ghwTest = ghwMock.GhwMock{}
			ghwTest.AddDisk(mainDisk)
			ghwTest.CreateDevices()

			createDMDevice(fs, "dm-0", "253:0", constants.PersistentLabel)
			createDMDevice(fs, "dm-1", "253:1", constants.OEMLabel)

			spec, err := config.NewResetSpec(c)
			Expect(err).ToNot(HaveOccurred())
			Expect(spec.Partitions.Persistent).ToNot(BeNil())
			Expect(spec.Partitions.Persistent.FilesystemLabel).To(Equal(constants.PersistentLabel))
			Expect(spec.Partitions.Persistent.Path).To(Equal("/dev/mapper/dm-0"))
			Expect(spec.Partitions.OEM).ToNot(BeNil())
			Expect(spec.Partitions.OEM.FilesystemLabel).To(Equal(constants.OEMLabel))
		})
	})

	Describe("ReadResetSpecFromConfig", Label("reset"), func() {
		var ghwTest ghwMock.GhwMock
		BeforeEach(func() {
			mainDisk := sdkPartitions.Disk{
				Name: "device",
				Partitions: []*sdkPartitions.Partition{
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
		})
		AfterEach(func() {
			ghwTest.Clean()
		})
		It("returns a reset spec when booted from recovery", func() {
			runner.SideEffect = func(cmd string, args ...string) ([]byte, error) {
				if cmd == "cat" {
					return []byte(constants.SystemLabel), nil
				}
				return []byte{}, nil
			}
			// Have a recovery image available so the source is valid for sanitize
			recoveryImg := filepath.Join(constants.RunningStateDir, "cOS", constants.RecoveryImgFile)
			Expect(fsutils.MkdirAll(fs, filepath.Dir(recoveryImg), constants.DirPerm)).To(Succeed())
			_, err = fs.Create(recoveryImg)
			Expect(err).ToNot(HaveOccurred())

			spec, err := config.ReadResetSpecFromConfig(c)
			Expect(err).ToNot(HaveOccurred())
			Expect(spec.Active.Source.Value()).To(Equal(recoveryImg))
		})
		It("fails when not booted from recovery", func() {
			runner.SideEffect = func(cmd string, args ...string) ([]byte, error) {
				return []byte{}, nil
			}
			_, err := config.ReadResetSpecFromConfig(c)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("reset can only be called from the recovery system"))
		})
		It("fails when the config cannot be unmarshalled into the spec", func() {
			runner.SideEffect = func(cmd string, args ...string) ([]byte, error) {
				if cmd == "cat" {
					return []byte(constants.SystemLabel), nil
				}
				return []byte{}, nil
			}
			cfg, err := config.ScanNoLogs(collector.Readers(strings.NewReader("#cloud-config\nreset:\n  system:\n    size: not-a-number\n")))
			Expect(err).ToNot(HaveOccurred())
			c.Collector = cfg.Collector
			_, err = config.NewResetSpec(c)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed unmarshalling the full spec"))
		})
	})

	Describe("ReadInstallSpecFromConfig auto target", Label("install"), func() {
		var ghwTest ghwMock.GhwMock
		BeforeEach(func() {
			mainDisk := sdkPartitions.Disk{
				Name:      "biggerdevice",
				SizeBytes: 2097152,
			}
			ghwTest = ghwMock.GhwMock{}
			ghwTest.AddDisk(mainDisk)
			ghwTest.CreateDevices()
		})
		AfterEach(func() {
			ghwTest.Clean()
		})
		It("detects the largest device when no target is given", func() {
			c.Install.Source = "oci:test:latest"
			spec, err := config.ReadInstallSpecFromConfig(c)
			Expect(err).ToNot(HaveOccurred())
			Expect(spec.Target).To(Equal("/dev/biggerdevice"))
		})
	})

	Describe("UKI specs", Label("uki"), func() {
		var ghwTest ghwMock.GhwMock
		AfterEach(func() {
			ghwTest.Clean()
		})
		Describe("NewUkiInstallSpec", func() {
			It("sets defaults with a user provided source", func() {
				ghwTest = ghwMock.GhwMock{}
				ghwTest.CreateDevices()
				c.Install.Source = "oci:test:latest"
				spec, err := config.NewUkiInstallSpec(c)
				Expect(err).ToNot(HaveOccurred())
				Expect(spec.Active.Source.IsDocker()).To(BeTrue())
				Expect(spec.Partitions.EFI).ToNot(BeNil())
				Expect(spec.Partitions.EFI.Size).To(Equal(sdkConstants.ImgSize * 5))
				Expect(spec.Partitions.OEM).ToNot(BeNil())
				Expect(spec.Partitions.Persistent).ToNot(BeNil())
				Expect(spec.SkipEntries).To(ContainElements(constants.UkiDefaultSkipEntries()))
			})
			It("uses the iso source if available", func() {
				ghwTest = ghwMock.GhwMock{}
				ghwTest.CreateDevices()
				setupIsoBaseTreeDetection(fs)
				spec, err := config.NewUkiInstallSpec(c)
				Expect(err).ToNot(HaveOccurred())
				Expect(spec.Active.Source.IsDir()).To(BeTrue())
				Expect(spec.Active.Source.Value()).To(Equal(constants.IsoBaseTree))
			})
			It("sets an empty source if there is nothing available", func() {
				ghwTest = ghwMock.GhwMock{}
				ghwTest.CreateDevices()
				spec, err := config.NewUkiInstallSpec(c)
				Expect(err).ToNot(HaveOccurred())
				Expect(spec.Active.Source.IsEmpty()).To(BeTrue())
			})
			It("keeps the default EFI size if the source size cannot be inferred", func() {
				ghwTest = ghwMock.GhwMock{}
				ghwTest.CreateDevices()
				c.Install.Source = "file:/does/not/exist"
				spec, err := config.NewUkiInstallSpec(c)
				Expect(err).ToNot(HaveOccurred())
				Expect(spec.Partitions.EFI.Size).To(Equal(sdkConstants.ImgSize * 5))
			})
			It("grows the EFI partition if the source is bigger than the default", func() {
				ghwTest = ghwMock.GhwMock{}
				ghwTest.CreateDevices()
				Expect(fsutils.MkdirAll(fs, "/tmp", constants.DirPerm)).To(Succeed())
				Expect(fs.WriteFile("/tmp/bigsource", []byte("data"), constants.FilePerm)).To(Succeed())
				// 6Gb sparse file, bigger than the default 15Gb EFI partition when tripled
				Expect(fs.Truncate("/tmp/bigsource", 6144*1024*1024)).To(Succeed())
				c.Install.Source = "file:/tmp/bigsource"
				spec, err := config.NewUkiInstallSpec(c)
				Expect(err).ToNot(HaveOccurred())
				Expect(spec.Partitions.EFI.Size).To(Equal(uint((6144 + 100) * 3)))
			})
			It("fails when the config cannot be unmarshalled into the spec", func() {
				ghwTest = ghwMock.GhwMock{}
				ghwTest.CreateDevices()
				c.Collector = collector.Config{Values: collector.ConfigValues{
					"install": collector.ConfigValues{
						"system": collector.ConfigValues{
							"size": "not-a-number",
						},
					},
				}}
				_, err := config.ReadUkiInstallSpecFromConfig(c)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("failed initializing spec"))
			})
		})
		Describe("ReadUkiInstallSpecFromConfig", func() {
			It("detects the largest device when no target is given", func() {
				mainDisk := sdkPartitions.Disk{
					Name:      "ukidevice",
					SizeBytes: 2097152,
				}
				ghwTest = ghwMock.GhwMock{}
				ghwTest.AddDisk(mainDisk)
				ghwTest.CreateDevices()

				c.Install.Source = "oci:test:latest"
				spec, err := config.ReadUkiInstallSpecFromConfig(c)
				Expect(err).ToNot(HaveOccurred())
				Expect(spec.Target).To(Equal("/dev/ukidevice"))
			})
		})
		Describe("NewUkiResetSpec", func() {
			BeforeEach(func() {
				runner.SideEffect = func(cmd string, args ...string) ([]byte, error) {
					if cmd == "cat" {
						return []byte("rd.immucore.uki"), nil
					}
					return []byte{}, nil
				}
			})
			It("fails if not booted in uki mode", func() {
				ghwTest = ghwMock.GhwMock{}
				ghwTest.CreateDevices()
				runner.SideEffect = func(cmd string, args ...string) ([]byte, error) {
					return []byte{}, nil
				}
				// The current logic errors out when the uki_boot_mode sentinel exists
				// and the cmdline does not contain rd.immucore.uki
				Expect(fsutils.MkdirAll(fs, "/run/cos", constants.DirPerm)).To(Succeed())
				Expect(fs.WriteFile("/run/cos/uki_boot_mode", []byte{}, constants.FilePerm)).To(Succeed())
				_, err := config.NewUkiResetSpec(c)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("uki reset can only be called"))
			})
			It("fails without a persistent partition", func() {
				ghwTest = ghwMock.GhwMock{}
				ghwTest.CreateDevices()
				_, err := config.NewUkiResetSpec(c)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("persistent partition not found"))
			})
			It("fails through the spec reader without a persistent partition", func() {
				ghwTest = ghwMock.GhwMock{}
				ghwTest.CreateDevices()
				_, err := config.ReadUkiResetSpecFromConfig(c)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("failed initializing spec"))
			})
			It("fails without an oem partition", func() {
				ghwTest = ghwMock.GhwMock{}
				ghwTest.CreateDevices()
				createDMDevice(fs, "dm-0", "253:0", constants.PersistentLabel)
				_, err := config.NewUkiResetSpec(c)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("oem partition not found"))
			})
			It("fails without an efi partition", func() {
				ghwTest = ghwMock.GhwMock{}
				ghwTest.CreateDevices()
				createDMDevice(fs, "dm-0", "253:0", constants.PersistentLabel)
				createDMDevice(fs, "dm-1", "253:1", constants.OEMLabel)
				_, err := config.NewUkiResetSpec(c)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("efi partition not found"))
			})
			It("returns a uki reset spec with all the partitions found", func() {
				mainDisk := sdkPartitions.Disk{
					Name: "device",
					Partitions: []*sdkPartitions.Partition{
						{
							Name:            "device1",
							FilesystemLabel: constants.EfiLabel,
							FS:              "vfat",
						},
					},
				}
				ghwTest = ghwMock.GhwMock{}
				ghwTest.AddDisk(mainDisk)
				ghwTest.CreateDevices()
				createDMDevice(fs, "dm-0", "253:0", constants.PersistentLabel)
				createDMDevice(fs, "dm-1", "253:1", constants.OEMLabel)

				spec, err := config.ReadUkiResetSpecFromConfig(c)
				Expect(err).ToNot(HaveOccurred())
				Expect(spec.FormatPersistent).To(BeTrue())
				Expect(spec.Partitions.Persistent).ToNot(BeNil())
				Expect(spec.Partitions.OEM).ToNot(BeNil())
				Expect(spec.Partitions.EFI).ToNot(BeNil())
				Expect(spec.Partitions.EFI.FilesystemLabel).To(Equal(constants.EfiLabel))
			})
		})
		Describe("NewUkiUpgradeSpec", func() {
			It("fails when the config cannot be unmarshalled into the spec", func() {
				ghwTest = ghwMock.GhwMock{}
				ghwTest.CreateDevices()
				cfg, err := config.ScanNoLogs(collector.Readers(strings.NewReader("#cloud-config\nupgrade:\n  system:\n    size: not-a-number\n")))
				Expect(err).ToNot(HaveOccurred())
				c.Collector = cfg.Collector
				_, err = config.NewUkiUpgradeSpec(c)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("failed unmarshalling full spec"))
			})
			It("fails when there is no EFI partition", func() {
				ghwTest = ghwMock.GhwMock{}
				ghwTest.CreateDevices()
				Expect(fsutils.MkdirAll(fs, "/tmp", constants.DirPerm)).To(Succeed())
				Expect(fs.WriteFile("/tmp/waka", []byte("waka"), constants.FilePerm)).To(Succeed())
				cfg, err := config.ScanNoLogs(collector.Readers(strings.NewReader("#cloud-config\nupgrade:\n  system:\n    source: file:/tmp/waka\n")))
				Expect(err).ToNot(HaveOccurred())
				c.Collector = cfg.Collector
				_, err = config.NewUkiUpgradeSpec(c)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("could not read host partitions"))
			})
			It("fails through the spec reader when there is no EFI partition", func() {
				ghwTest = ghwMock.GhwMock{}
				ghwTest.CreateDevices()
				Expect(fsutils.MkdirAll(fs, "/tmp", constants.DirPerm)).To(Succeed())
				Expect(fs.WriteFile("/tmp/waka", []byte("waka"), constants.FilePerm)).To(Succeed())
				cfg, err := config.ScanNoLogs(collector.Readers(strings.NewReader("#cloud-config\nupgrade:\n  system:\n    source: file:/tmp/waka\n")))
				Expect(err).ToNot(HaveOccurred())
				c.Collector = cfg.Collector
				_, err = config.ReadUkiUpgradeSpecFromConfig(c)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("failed initializing spec"))
			})
			It("falls back to the default image size if the source size cannot be inferred", func() {
				ghwTest = ghwMock.GhwMock{}
				ghwTest.CreateDevices()
				cfg, err := config.ScanNoLogs(collector.Readers(strings.NewReader("#cloud-config\nupgrade:\n  system:\n    source: file:/does/not/exist\n")))
				Expect(err).ToNot(HaveOccurred())
				c.Collector = cfg.Collector
				spec, err := config.NewUkiUpgradeSpec(c)
				Expect(err).To(HaveOccurred())
				Expect(spec.Active.Size).To(Equal(sdkConstants.ImgSize))
			})
			It("fails when the source is bigger than the free space on the EFI partition", func() {
				mainDisk := sdkPartitions.Disk{
					Name: "device",
					Partitions: []*sdkPartitions.Partition{
						{
							Name:            "device1",
							FilesystemLabel: constants.EfiLabel,
							FS:              "vfat",
						},
					},
				}
				ghwTest = ghwMock.GhwMock{}
				ghwTest.AddDisk(mainDisk)
				ghwTest.CreateDevices()

				Expect(fsutils.MkdirAll(fs, "/tmp", constants.DirPerm)).To(Succeed())
				Expect(fs.WriteFile("/tmp/waka", []byte("waka"), constants.FilePerm)).To(Succeed())
				cfg, err := config.ScanNoLogs(collector.Readers(strings.NewReader("#cloud-config\nupgrade:\n  system:\n    source: file:/tmp/waka\n")))
				Expect(err).ToNot(HaveOccurred())
				c.Collector = cfg.Collector
				_, err = config.NewUkiUpgradeSpec(c)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("is bigger than the free space"))
			})
			It("returns a uki upgrade spec when there is enough free space", func() {
				mainDisk := sdkPartitions.Disk{
					Name: "device",
					Partitions: []*sdkPartitions.Partition{
						{
							Name:            "device1",
							FilesystemLabel: constants.EfiLabel,
							FS:              "vfat",
							MountPoint:      "/tmp",
						},
					},
				}
				ghwTest = ghwMock.GhwMock{}
				ghwTest.AddDisk(mainDisk)
				ghwTest.CreateDevices()

				Expect(fsutils.MkdirAll(fs, "/tmp", constants.DirPerm)).To(Succeed())
				Expect(fs.WriteFile("/tmp/waka", []byte("waka"), constants.FilePerm)).To(Succeed())
				cfg, err := config.ScanNoLogs(collector.Readers(strings.NewReader("#cloud-config\nupgrade:\n  system:\n    source: file:/tmp/waka\n")))
				Expect(err).ToNot(HaveOccurred())
				c.Collector = cfg.Collector
				spec, err := config.ReadUkiUpgradeSpecFromConfig(c)
				Expect(err).ToNot(HaveOccurred())
				Expect(spec.EfiPartition).ToNot(BeNil())
				Expect(spec.EfiPartition.MountPoint).To(Equal("/tmp"))
			})
		})
	})
})

var _ = Describe("GetSourceSize extra paths", Label("GetSourceSize"), func() {
	var logger sdkLogger.KairosLogger
	var conf *sdkConfig.Config
	var memLog bytes.Buffer

	BeforeEach(func() {
		memLog = bytes.Buffer{}
		logger = sdkLogger.NewBufferLogger(&memLog)
		logger.SetLevel("debug")
		conf = config.NewConfig(
			config.WithLogger(logger),
			config.WithImageExtractor(v1mock.NewFakeImageExtractor(logger)),
		)
	})

	It("calculates the size of docker sources through the image extractor", func() {
		size, err := config.GetSourceSize(conf, sdkImages.NewDockerSrc("test/image:latest"))
		Expect(err).ToNot(HaveOccurred())
		// The fake extractor reports 0 size
		Expect(size).To(Equal(int64(0)))
	})
	It("fails for file sources that do not exist", func() {
		_, err := config.GetSourceSize(conf, sdkImages.NewFileSrc("/does/not/exist"))
		Expect(err).To(HaveOccurred())
	})
	It("follows relative symlinks and ignores dangling ones", func() {
		tempDir, err := os.MkdirTemp("", "kairos-size-test")
		Expect(err).ToNot(HaveOccurred())
		defer os.RemoveAll(tempDir)

		Expect(createFileOfSizeInMB(filepath.Join(tempDir, "200MB.txt"), 200)).To(Succeed())
		// Relative symlink to an existing file, should not be counted twice
		Expect(os.Symlink("200MB.txt", filepath.Join(tempDir, "relative-link.txt"))).To(Succeed())
		// Dangling symlink, should be ignored
		Expect(os.Symlink("missing-file.txt", filepath.Join(tempDir, "dangling-link.txt"))).To(Succeed())

		size, err := config.GetSourceSize(conf, sdkImages.NewDirSrc(tempDir))
		Expect(err).ToNot(HaveOccurred())
		// 200MB of content + 100MB extra added by GetSourceSize
		Expect(size).To(Equal(int64(300)))
	})
	It("fails when a directory cannot be read while walking the source", func() {
		tempDir, err := os.MkdirTemp("", "kairos-size-test")
		Expect(err).ToNot(HaveOccurred())
		defer os.RemoveAll(tempDir)

		unreadable := filepath.Join(tempDir, "unreadable")
		Expect(os.Mkdir(unreadable, os.ModePerm)).To(Succeed())
		Expect(os.Chmod(unreadable, 0000)).To(Succeed())
		defer os.Chmod(unreadable, os.ModePerm) // nolint:errcheck

		_, err = config.GetSourceSize(conf, sdkImages.NewDirSrc(tempDir))
		Expect(err).To(HaveOccurred())
	})
	It("fails when a symlink target cannot be stat", func() {
		tempDir, err := os.MkdirTemp("", "kairos-size-test")
		Expect(err).ToNot(HaveOccurred())
		defer os.RemoveAll(tempDir)

		// Target lives in a directory that we make unsearchable, so stat fails
		// with a permission error instead of a not-exist one
		hiddenDir, err := os.MkdirTemp("", "kairos-size-hidden")
		Expect(err).ToNot(HaveOccurred())
		defer os.RemoveAll(hiddenDir)
		target := filepath.Join(hiddenDir, "target.txt")
		Expect(os.WriteFile(target, []byte("data"), os.ModePerm)).To(Succeed())
		Expect(os.Symlink(target, filepath.Join(tempDir, "link.txt"))).To(Succeed())
		Expect(os.Chmod(hiddenDir, 0000)).To(Succeed())
		defer os.Chmod(hiddenDir, os.ModePerm) // nolint:errcheck

		_, err = config.GetSourceSize(conf, sdkImages.NewDirSrc(tempDir))
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("DetectPreConfiguredDevice", Label("detect"), func() {
	var logger sdkLogger.KairosLogger
	var ghwTest ghwMock.GhwMock

	BeforeEach(func() {
		logger = sdkLogger.NewNullLogger()
	})
	AfterEach(func() {
		ghwTest.Clean()
	})

	It("returns the disk with a COS_STATE partition", func() {
		mainDisk := sdkPartitions.Disk{
			Name: "device",
			Partitions: []*sdkPartitions.Partition{
				{
					Name:            "device4",
					FilesystemLabel: "COS_STATE",
					FS:              "ext4",
				},
			},
		}
		ghwTest = ghwMock.GhwMock{}
		ghwTest.AddDisk(mainDisk)
		ghwTest.CreateDevices()

		device, err := config.DetectPreConfiguredDevice(logger)
		Expect(err).ToNot(HaveOccurred())
		Expect(device).To(Equal("/dev/device"))
	})
	It("returns an empty string when no COS_STATE partition is found", func() {
		mainDisk := sdkPartitions.Disk{
			Name: "device",
			Partitions: []*sdkPartitions.Partition{
				{
					Name:            "device1",
					FilesystemLabel: "SOMETHING_ELSE",
					FS:              "ext4",
				},
			},
		}
		ghwTest = ghwMock.GhwMock{}
		ghwTest.AddDisk(mainDisk)
		ghwTest.CreateDevices()

		device, err := config.DetectPreConfiguredDevice(logger)
		Expect(err).ToNot(HaveOccurred())
		Expect(device).To(Equal(""))
	})
})
