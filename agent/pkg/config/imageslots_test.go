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
	"path/filepath"
	"strings"

	"github.com/kairos-io/kairos/v4/agent/pkg/config"
	sdkAgentConstants "github.com/kairos-io/kairos/v4/agent/pkg/constants"
	fsutils "github.com/kairos-io/kairos/v4/agent/pkg/utils/fs"
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

// The install block is decoded over the image slots NewInstallSpec built, so
// install.system.label and install.system.fs reach the installer and are
// written to the image filesystem. The boot path reads neither: the initramfs
// finds each image at /dev/disk/by-label/<constant> and mounts it as ext4. A
// node installed with either set reboots into an emergency shell, and nothing
// on the install path says why. See kairos-io/kairos#4914.
var _ = Describe("Install image slots", Label("install", "config"), func() {
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

	It("builds a spec when the install block leaves the slots alone", func() {
		spec, err := installSpecFor("#cloud-config\ninstall:\n  device: /dev/sda\n  system:\n    size: 4096\n")
		Expect(err).ToNot(HaveOccurred())
		Expect(spec).ToNot(BeNil())
	})

	DescribeTable("refuses a label the initramfs will not look for",
		func(cc, key, label, boundTo string) {
			_, err := installSpecFor(cc)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring(key + ".label cannot be set"))
			Expect(err.Error()).To(ContainSubstring("/dev/disk/by-label/" + boundTo))
			Expect(err.Error()).To(ContainSubstring(label))
		},
		Entry("install.system",
			"#cloud-config\ninstall:\n  device: /dev/sda\n  system:\n    label: MY_ACTIVE\n",
			"install.system", "MY_ACTIVE", sdkConstants.ActiveLabel),
		Entry("install.passive",
			"#cloud-config\ninstall:\n  device: /dev/sda\n  passive:\n    label: MY_PASSIVE\n",
			"install.passive", "MY_PASSIVE", sdkConstants.PassiveLabel),
		Entry("install.recovery-system",
			"#cloud-config\ninstall:\n  device: /dev/sda\n  recovery-system:\n    label: MY_RECOVERY\n",
			"install.recovery-system", "MY_RECOVERY", sdkConstants.SystemLabel),
	)

	DescribeTable("refuses a filesystem the initramfs cannot mount",
		func(cc, key, wanted string) {
			_, err := installSpecFor(cc)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring(key + ".fs cannot be \"" + wanted + "\""))
			Expect(err.Error()).To(ContainSubstring("ext2, ext3, ext4"))
		},
		Entry("xfs on install.system",
			"#cloud-config\ninstall:\n  device: /dev/sda\n  system:\n    fs: xfs\n", "install.system", "xfs"),
		Entry("btrfs on install.passive",
			"#cloud-config\ninstall:\n  device: /dev/sda\n  passive:\n    fs: btrfs\n", "install.passive", "btrfs"),
		Entry("squashfs on install.recovery-system, which has no squashfs to deploy here",
			"#cloud-config\ninstall:\n  device: /dev/sda\n  recovery-system:\n    fs: squashfs\n", "install.recovery-system", "squashfs"),
	)

	DescribeTable("keeps accepting the filesystems the ext4 driver reads",
		func(fsName string) {
			_, err := installSpecFor("#cloud-config\ninstall:\n  device: /dev/sda\n  system:\n    fs: " + fsName + "\n")
			Expect(err).ToNot(HaveOccurred())
		},
		Entry("ext2, which is the default", "ext2"),
		Entry("ext3", "ext3"),
		Entry("ext4", "ext4"),
	)
})

// An ISO that ships recovery.squashfs leaves recoveryImg.Label empty, because
// the squashfs is deployed with no label and gets SystemLabel later, in
// elemental.DeployImage. A config that spells out the label the boot path
// already looks for is a no-op, not a broken install, so it has to keep
// working. See https://github.com/kairos-io/kairos/pull/4915#discussion_r4164668785
var _ = Describe("Install image slots with the recovery squashfs present", Label("install", "config"), func() {
	var c *sdkConfig.Config
	var fs *vfst.TestFS
	var cleanup func()

	installSpecFor := func(cc string) (interface{ GetTarget() string }, error) {
		cfg, err := config.ScanNoLogs(collector.Readers(strings.NewReader(cc)))
		Expect(err).ToNot(HaveOccurred())
		c.Collector = cfg.Collector
		return config.NewInstallSpec(c)
	}

	BeforeEach(func() {
		var err error
		var memLog bytes.Buffer
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
		setupRecoverySquashfs(fs)
	})

	AfterEach(func() { cleanup() })

	It("accepts the label the initramfs already looks for", func() {
		_, err := installSpecFor("#cloud-config\ninstall:\n  device: /dev/sda\n  recovery-system:\n    label: " + sdkConstants.SystemLabel + "\n")
		Expect(err).ToNot(HaveOccurred())
	})

	It("still refuses a label the initramfs will not look for", func() {
		_, err := installSpecFor("#cloud-config\ninstall:\n  device: /dev/sda\n  recovery-system:\n    label: MY_RECOVERY\n")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("install.recovery-system.label cannot be set"))
	})
})

// setupRecoverySquashfs makes NewInstallSpec take the recoveryExists branch,
// the one a default ISO install takes.
func setupRecoverySquashfs(fs *vfst.TestFS) {
	recoveryImgFile := filepath.Join(sdkAgentConstants.LiveDir, sdkAgentConstants.RecoverySquashFile)
	Expect(fsutils.MkdirAll(fs, filepath.Dir(recoveryImgFile), sdkAgentConstants.DirPerm)).ToNot(HaveOccurred())
	_, err := fs.Create(recoveryImgFile)
	Expect(err).ToNot(HaveOccurred())
}
