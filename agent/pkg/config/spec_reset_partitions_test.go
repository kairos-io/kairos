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

	"github.com/kairos-io/kairos/v4/agent/pkg/config"
	"github.com/kairos-io/kairos/v4/agent/pkg/constants"
	v1mock "github.com/kairos-io/kairos/v4/agent/tests/mocks"
	"github.com/kairos-io/kairos/v4/sdk/collector"
	sdkConstants "github.com/kairos-io/kairos/v4/sdk/constants"
	ghwMock "github.com/kairos-io/kairos/v4/sdk/ghw/mocks"
	"github.com/kairos-io/kairos/v4/sdk/kcrypt"
	sdkBundles "github.com/kairos-io/kairos/v4/sdk/types/bundles"
	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
	sdkInstall "github.com/kairos-io/kairos/v4/sdk/types/install"
	sdkLogger "github.com/kairos-io/kairos/v4/sdk/types/logger"
	sdkPartitions "github.com/kairos-io/kairos/v4/sdk/types/partitions"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/twpayne/go-vfs/v5/vfst"
)

// NewResetSpec says in so many words that OEM and persistent are optional: it
// looks both up through the device mapper when ghw does not list them, and it
// warns instead of failing when they are still missing. A machine can be reset
// without either one: install.extra-partitions can lay out a disk with no OEM,
// and reset is the documented way back from a persistent partition that is
// gone or unreadable. So reading a mount point off them has to survive a nil.
var _ = Describe("NewResetSpec with optional partitions missing", Label("types", "config", "reset"), func() {
	var cleanup func()
	var c *sdkConfig.Config
	var ghwTest ghwMock.GhwMock
	var memLog bytes.Buffer

	// resetDisk is a recovery-bootable layout: state and recovery are the two
	// partitions NewResetSpec refuses to work without, and nothing else.
	resetDisk := func(extra ...*sdkPartitions.Partition) sdkPartitions.Disk {
		parts := []*sdkPartitions.Partition{
			{Name: "device3", FilesystemLabel: constants.RecoveryLabel, FS: "ext4"},
			{Name: "device4", FilesystemLabel: constants.StateLabel, FS: "ext4"},
		}
		return sdkPartitions.Disk{Name: "device", Partitions: append(parts, extra...)}
	}

	BeforeEach(func() {
		var err error
		var fs *vfst.TestFS

		memLog = bytes.Buffer{}
		logger := sdkLogger.NewBufferLogger(&memLog)
		logger.SetLevel("debug")

		fs, cleanup, err = vfst.NewTestFS(nil)
		Expect(err).ToNot(HaveOccurred())

		runner := v1mock.NewFakeRunner()
		// Report the recovery system, so the "reset can only be called from
		// the recovery system" guard lets us through to the partition lookups.
		runner.SideEffect = func(cmd string, args ...string) ([]byte, error) {
			if cmd == "cat" {
				return []byte(constants.SystemLabel), nil
			}
			return []byte{}, nil
		}

		c = config.NewConfig(
			config.WithFs(fs),
			config.WithMounter(v1mock.NewErrorMounter()),
			config.WithRunner(runner),
			config.WithSyscall(&v1mock.FakeSyscall{}),
			config.WithLogger(logger),
			config.WithCloudInitRunner(&v1mock.FakeCloudInitRunner{}),
			config.WithClient(&v1mock.FakeHTTPClient{}),
			config.WithPlatform("linux/amd64"),
		)
		c.Install = &sdkInstall.Install{}
		c.Bundles = sdkBundles.Bundles{}
		c.Collector = collector.Config{}
	})

	AfterEach(func() {
		ghwTest.Clean()
		cleanup()
	})

	It("builds a spec when there is no persistent partition", func() {
		ghwTest = ghwMock.GhwMock{}
		ghwTest.AddDisk(resetDisk(&sdkPartitions.Partition{
			Name: "device2", FilesystemLabel: constants.OEMLabel, FS: "ext4",
		}))
		ghwTest.CreateDevices()

		spec, err := config.NewResetSpec(c)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(spec.Partitions.Persistent).To(BeNil())
		Expect(spec.Partitions.OEM).ToNot(BeNil())
	})

	It("builds a spec when there is no OEM partition", func() {
		ghwTest = ghwMock.GhwMock{}
		ghwTest.AddDisk(resetDisk(&sdkPartitions.Partition{
			Name: "device5", FilesystemLabel: constants.PersistentLabel, FS: "ext4",
		}))
		ghwTest.CreateDevices()

		spec, err := config.NewResetSpec(c)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(spec.Partitions.OEM).To(BeNil())
		Expect(spec.Partitions.Persistent).ToNot(BeNil())
	})

	It("builds a spec when neither is there, and says so", func() {
		ghwTest = ghwMock.GhwMock{}
		ghwTest.AddDisk(resetDisk())
		ghwTest.CreateDevices()

		spec, err := config.NewResetSpec(c)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(spec.Partitions.OEM).To(BeNil())
		Expect(spec.Partitions.Persistent).To(BeNil())
		// FormatPersistent defaults to true, so the existing warning has to fire
		// rather than the run ending in a panic.
		Expect(memLog.String()).To(ContainSubstring("no Persistent partition found"))
	})

	// A locked LUKS container reaches NewResetSpec as the same nil entry an
	// absent partition does: ghw matches the plaintext COS_PERSISTENT label,
	// which lives inside the container, and GetPartitionViaDM only sees a
	// mapper that is already open. Warning here would reset the OS and exit 0
	// while leaving the data untouched, which is worse than the panic this PR
	// removes, so these two specs pin the error instead.
	It("refuses to reset a persistent partition that is a locked LUKS container", func() {
		ghwTest = ghwMock.GhwMock{}
		ghwTest.AddDisk(resetDisk(&sdkPartitions.Partition{
			Name: "device5", PartitionLabel: "persistent",
			FilesystemLabel: sdkConstants.PersistentLUKSLabel, FS: sdkConstants.LUKSFs,
		}))
		ghwTest.CreateDevices()

		spec, err := config.NewResetSpec(c)
		Expect(err).Should(HaveOccurred())
		Expect(spec).To(BeNil())
		Expect(err.Error()).To(ContainSubstring(sdkConstants.PersistentLabel))
		Expect(err.Error()).To(ContainSubstring("no open mapper"))
		Expect(err.Error()).To(ContainSubstring("reset-persistent"))
		// The warning is the branch this replaces, so it must not also fire.
		Expect(memLog.String()).ToNot(ContainSubstring("no Persistent partition found"))
	})

	// A container only a passphrase opens was left locked in recovery on
	// purpose (the person forgot the passphrase). The spec takes the raw
	// container so the reset preflight can erase it and create a new one.
	It("takes a locked passphrase container instead of refusing", func() {
		orig := kcrypt.LUKSSubsystem
		DeferCleanup(func() { kcrypt.LUKSSubsystem = orig })
		var probed string
		kcrypt.LUKSSubsystem = func(device string) (string, error) {
			probed = device
			return kcrypt.PassphraseSubsystem, nil
		}
		ghwTest = ghwMock.GhwMock{}
		ghwTest.AddDisk(resetDisk(&sdkPartitions.Partition{
			Name: "device5", PartitionLabel: "persistent",
			FilesystemLabel: sdkConstants.PersistentLUKSLabel, FS: sdkConstants.LUKSFs,
		}))
		ghwTest.CreateDevices()

		spec, err := config.NewResetSpec(c)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(spec.Partitions.Persistent).ToNot(BeNil())
		Expect(spec.Partitions.Persistent.FS).To(Equal(sdkConstants.LUKSFs))
		Expect(spec.Partitions.Persistent.Path).ToNot(BeEmpty())
		Expect(probed).To(Equal(spec.Partitions.Persistent.Path))
		Expect(memLog.String()).To(ContainSubstring("the reset will erase it"))
	})

	It("still refuses a locked container with another key", func() {
		orig := kcrypt.LUKSSubsystem
		DeferCleanup(func() { kcrypt.LUKSSubsystem = orig })
		kcrypt.LUKSSubsystem = func(string) (string, error) { return "", nil }
		ghwTest = ghwMock.GhwMock{}
		ghwTest.AddDisk(resetDisk(&sdkPartitions.Partition{
			Name: "device5", PartitionLabel: "persistent",
			FilesystemLabel: sdkConstants.PersistentLUKSLabel, FS: sdkConstants.LUKSFs,
		}))
		ghwTest.CreateDevices()

		_, err := config.NewResetSpec(c)
		Expect(err).To(MatchError(ContainSubstring("no open mapper")))
	})

	It("refuses to reset an OEM partition that is a locked LUKS container when asked to format it", func() {
		ghwTest = ghwMock.GhwMock{}
		ghwTest.AddDisk(resetDisk(
			&sdkPartitions.Partition{
				Name: "device2", PartitionLabel: "oem",
				FilesystemLabel: sdkConstants.OEMLUKSLabel, FS: sdkConstants.LUKSFs,
			},
			// Plaintext, so the persistent branch is out of the way and the
			// error under test can only come from the OEM one.
			&sdkPartitions.Partition{Name: "device5", FilesystemLabel: constants.PersistentLabel, FS: "ext4"},
		))
		ghwTest.CreateDevices()

		// reset-oem is off by default, so the operator has to ask for it
		// before a locked OEM container is in the way of anything.
		c.Collector = collector.Config{Values: collector.ConfigValues{
			"reset": map[string]interface{}{"reset-oem": true},
		}}

		spec, err := config.NewResetSpec(c)
		Expect(err).Should(HaveOccurred())
		Expect(spec).To(BeNil())
		Expect(err.Error()).To(ContainSubstring(sdkConstants.OEMLabel))
		Expect(err.Error()).To(ContainSubstring("reset-oem"))
		Expect(memLog.String()).ToNot(ContainSubstring("no OEM partition found"))
	})

	// The branch only fires for a container that is there. An absent partition
	// has to keep warning, or this change would turn every diskless-OEM reset
	// into a hard failure.
	It("still only warns when the missing partition is not an encrypted container", func() {
		ghwTest = ghwMock.GhwMock{}
		ghwTest.AddDisk(resetDisk())
		ghwTest.CreateDevices()

		c.Collector = collector.Config{Values: collector.ConfigValues{
			"reset": map[string]interface{}{"reset-oem": true},
		}}

		spec, err := config.NewResetSpec(c)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(spec.Partitions.OEM).To(BeNil())
		Expect(spec.Partitions.Persistent).To(BeNil())
		Expect(memLog.String()).To(ContainSubstring("no OEM partition found"))
		Expect(memLog.String()).To(ContainSubstring("no Persistent partition found"))
	})
})
