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
	"strings"

	"github.com/kairos-io/kairos/v4/agent/pkg/config"
	v1mock "github.com/kairos-io/kairos/v4/agent/tests/mocks"
	"github.com/kairos-io/kairos/v4/sdk/collector"
	sdkConstants "github.com/kairos-io/kairos/v4/sdk/constants"
	sdkBundles "github.com/kairos-io/kairos/v4/sdk/types/bundles"
	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
	sdkInstall "github.com/kairos-io/kairos/v4/sdk/types/install"
	sdkLogger "github.com/kairos-io/kairos/v4/sdk/types/logger"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/twpayne/go-vfs/v5/vfst"
)

// install.partitions decodes a name, a label and a filesystem onto every
// system partition, and NewInstallElementalPartitions then rebuilds those
// partitions from Kairos constants and reads only the size back. The docs say
// the filesystem of the default partitions is configurable, so a node
// installed with install.partitions.oem.fs set to xfs comes up with ext4 and
// nothing on the install path says so. See kairos-io/kairos#2159.
var _ = Describe("Install system partitions", Label("install", "config", "partitions"), func() {
	var c *sdkConfig.Config
	var fs *vfst.TestFS
	var cleanup func()
	var memLog bytes.Buffer

	// installSpecFor runs the real collector over cc and builds the install
	// spec from it, the same way runInstall does.
	installSpecFor := func(cc string) (interface{ GetTarget() string }, error) {
		cfg, err := config.ScanNoLogs(collector.Readers(strings.NewReader(cc)))
		Expect(err).ToNot(HaveOccurred())
		c.Collector = cfg.Collector
		return config.NewInstallSpec(c)
	}

	BeforeEach(func() {
		var err error
		memLog = bytes.Buffer{}
		logger := sdkLogger.NewBufferLogger(&memLog)
		logger.SetLevel("debug")

		fs, cleanup, err = vfst.NewTestFS(nil)
		Expect(err).ToNot(HaveOccurred())

		c = config.NewConfig(
			config.WithFs(fs),
			config.WithMounter(v1mock.NewErrorMounter()),
			config.WithRunner(v1mock.NewFakeRunner()),
			config.WithSyscall(&v1mock.FakeSyscall{}),
			config.WithLogger(logger),
			config.WithCloudInitRunner(&v1mock.FakeCloudInitRunner{}),
			config.WithClient(&v1mock.FakeHTTPClient{}),
			config.WithPlatform("linux/amd64"),
		)
		c.Install = &sdkInstall.Install{}
		c.Bundles = sdkBundles.Bundles{}
		c.Collector = collector.Config{}

		setupIsoBaseTreeDetection(fs)
	})

	AfterEach(func() { cleanup() })

	It("keeps building a spec when install.partitions only sets sizes", func() {
		spec, err := installSpecFor("#cloud-config\ninstall:\n  device: /dev/sda\n  partitions:\n    oem:\n      size: 512\n    persistent:\n      size: 500\n")
		Expect(err).ToNot(HaveOccurred())
		Expect(spec).ToNot(BeNil())
	})

	It("keeps building a spec when install.partitions is absent", func() {
		spec, err := installSpecFor("#cloud-config\ninstall:\n  device: /dev/sda\n")
		Expect(err).ToNot(HaveOccurred())
		Expect(spec).ToNot(BeNil())
	})

	DescribeTable("refuses a filesystem the installer does not create",
		func(cc, key, wanted, created string) {
			_, err := installSpecFor(cc)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring(key + ".fs cannot be \"" + wanted + "\""))
			Expect(err.Error()).To(ContainSubstring("always creates " + created))
		},
		Entry("xfs on oem, the case the docs invite",
			"#cloud-config\ninstall:\n  device: /dev/sda\n  partitions:\n    oem:\n      size: 512\n      fs: xfs\n",
			"install.partitions.oem", "xfs", sdkConstants.LinuxFs),
		Entry("btrfs on persistent",
			"#cloud-config\ninstall:\n  device: /dev/sda\n  partitions:\n    persistent:\n      fs: btrfs\n",
			"install.partitions.persistent", "btrfs", sdkConstants.LinuxFs),
		Entry("xfs on state",
			"#cloud-config\ninstall:\n  device: /dev/sda\n  partitions:\n    state:\n      fs: xfs\n",
			"install.partitions.state", "xfs", sdkConstants.LinuxFs),
		Entry("xfs on recovery",
			"#cloud-config\ninstall:\n  device: /dev/sda\n  partitions:\n    recovery:\n      fs: xfs\n",
			"install.partitions.recovery", "xfs", sdkConstants.LinuxFs),
		Entry("ext4 on the ESP, which is vfat",
			"#cloud-config\ninstall:\n  device: /dev/sda\n  partitions:\n    efi:\n      size: 128\n      fs: ext4\n",
			"install.partitions.efi", "ext4", sdkConstants.EfiFs),
	)

	DescribeTable("refuses a partition name the installer does not use",
		func(cc, key, wanted, used string) {
			_, err := installSpecFor(cc)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring(key + ".name cannot be \"" + wanted + "\""))
			Expect(err.Error()).To(ContainSubstring("always names that partition \"" + used + "\""))
		},
		Entry("oem", "#cloud-config\ninstall:\n  device: /dev/sda\n  partitions:\n    oem:\n      name: myoem\n",
			"install.partitions.oem", "myoem", sdkConstants.OEMPartName),
		Entry("persistent", "#cloud-config\ninstall:\n  device: /dev/sda\n  partitions:\n    persistent:\n      name: data\n",
			"install.partitions.persistent", "data", sdkConstants.PersistentPartName),
	)

	DescribeTable("refuses a filesystem label the initramfs will not look for",
		func(cc, key, wanted, used string) {
			_, err := installSpecFor(cc)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring(key + ".label cannot be \"" + wanted + "\""))
			Expect(err.Error()).To(ContainSubstring("/dev/disk/by-label/" + used))
		},
		Entry("oem", "#cloud-config\ninstall:\n  device: /dev/sda\n  partitions:\n    oem:\n      label: MY_OEM\n",
			"install.partitions.oem", "MY_OEM", sdkConstants.OEMLabel),
		Entry("persistent", "#cloud-config\ninstall:\n  device: /dev/sda\n  partitions:\n    persistent:\n      label: MY_DATA\n",
			"install.partitions.persistent", "MY_DATA", sdkConstants.PersistentLabel),
		Entry("state", "#cloud-config\ninstall:\n  device: /dev/sda\n  partitions:\n    state:\n      label: MY_STATE\n",
			"install.partitions.state", "MY_STATE", sdkConstants.StateLabel),
	)

	DescribeTable("accepts a value that matches what the installer uses, so a config that spells out the default keeps working",
		func(cc string) {
			_, err := installSpecFor(cc)
			Expect(err).ToNot(HaveOccurred())
		},
		// This is the config the partitioning docs show.
		Entry("the documented ext4 example",
			"#cloud-config\ninstall:\n  device: /dev/sda\n  partitions:\n    oem:\n      size: 512\n      fs: ext4\n    recovery:\n      size: 10000\n      fs: ext4\n"),
		Entry("the name the installer already uses",
			"#cloud-config\ninstall:\n  device: /dev/sda\n  partitions:\n    oem:\n      name: oem\n"),
		Entry("the label the installer already uses",
			"#cloud-config\ninstall:\n  device: /dev/sda\n  partitions:\n    persistent:\n      label: COS_PERSISTENT\n"),
		Entry("vfat on the ESP",
			"#cloud-config\ninstall:\n  device: /dev/sda\n  partitions:\n    efi:\n      size: 128\n      fs: vfat\n"),
	)

	// The check has to run on the values the config left, not on the ones
	// NewInstallElementalPartitions writes, so guard the ordering: moving the
	// call after that rebuild would make every entry above pass vacuously,
	// because the rebuild always writes the fixed values the check compares to.
	It("still applies the configured size, which is the one key the installer does read", func() {
		cfg, err := config.ScanNoLogs(collector.Readers(strings.NewReader(
			"#cloud-config\ninstall:\n  device: /dev/sda\n  partitions:\n    oem:\n      size: 512\n")))
		Expect(err).ToNot(HaveOccurred())
		c.Collector = cfg.Collector
		spec, err := config.NewInstallSpec(c)
		Expect(err).ToNot(HaveOccurred())
		Expect(spec.Partitions.OEM).ToNot(BeNil())
		Expect(spec.Partitions.OEM.Size).To(Equal(uint(512)))
		Expect(spec.Partitions.OEM.Name).To(Equal(sdkConstants.OEMPartName))
		Expect(spec.Partitions.OEM.FS).To(Equal(sdkConstants.LinuxFs))
	})
})
