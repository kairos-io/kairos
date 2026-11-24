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
	"path/filepath"

	"github.com/kairos-io/kairos/v4/agent/pkg/config"
	"github.com/kairos-io/kairos/v4/agent/pkg/constants"
	v1 "github.com/kairos-io/kairos/v4/agent/pkg/implementations/spec"
	fsutils "github.com/kairos-io/kairos/v4/agent/pkg/utils/fs"
	"github.com/kairos-io/kairos/v4/sdk/collector"
	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
	sdkImages "github.com/kairos-io/kairos/v4/sdk/types/images"
	sdkInstall "github.com/kairos-io/kairos/v4/sdk/types/install"
	sdkLogger "github.com/kairos-io/kairos/v4/sdk/types/logger"
	sdkPartitions "github.com/kairos-io/kairos/v4/sdk/types/partitions"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/twpayne/go-vfs/v5/vfst"
)

var _ = Describe("ResizeInstallSpecForSource", Label("install", "iso"), func() {
	const rootfs = "/iso/rootfs"
	var fs *vfst.TestFS
	var cleanup func()
	var c *sdkConfig.Config
	var sp *v1.InstallSpec

	BeforeEach(func() {
		var err error
		fs, cleanup, err = vfst.NewTestFS(nil)
		Expect(err).ToNot(HaveOccurred())
		var memLog bytes.Buffer
		logger := sdkLogger.NewBufferLogger(&memLog)
		c = config.NewConfig(config.WithFs(fs), config.WithLogger(logger))
		c.Install = &sdkInstall.Install{}
		c.Collector = collector.Config{}

		Expect(fsutils.MkdirAll(fs, rootfs, constants.DirPerm)).To(Succeed())
		// 5MiB of data is measured as 105MiB
		Expect(fs.WriteFile(filepath.Join(rootfs, "payload"), make([]byte, 5*1024*1024), constants.FilePerm)).To(Succeed())

		// A spec sized from a bigger provisional source
		sp = &v1.InstallSpec{
			Active:   sdkImages.Image{Size: 3072, Source: sdkImages.NewDirSrc(rootfs)},
			Passive:  sdkImages.Image{Size: 3072},
			Recovery: sdkImages.Image{Size: 3072},
			Partitions: sdkPartitions.ElementalPartitions{
				Recovery: &sdkPartitions.Partition{Size: 6344, MountPoint: constants.RecoveryDir},
				State:    &sdkPartitions.Partition{Size: 10216, MountPoint: constants.StateDir},
				OEM:      &sdkPartitions.Partition{Size: 64},
				EFI:      &sdkPartitions.Partition{Size: 64},
			},
		}
	})
	AfterEach(func() {
		cleanup()
	})

	It("replaces sizes calculated from an earlier source", func() {
		Expect(config.ResizeInstallSpecForSource(c, sp)).To(Succeed())
		Expect(sp.Active.Size).To(Equal(uint(105)))
		Expect(sp.Passive.Size).To(Equal(uint(105)))
		Expect(sp.Recovery.Size).To(Equal(uint(105)))
		Expect(sp.Partitions.Recovery.Size).To(Equal(uint(105*2 + 200)))
		Expect(sp.Partitions.State.Size).To(Equal(uint(105*3 + 1000)))
		// Partitions that do not hold images are left as they were
		Expect(sp.Partitions.Recovery.MountPoint).To(Equal(constants.RecoveryDir))
		Expect(sp.Partitions.OEM.Size).To(Equal(uint(64)))
		Expect(sp.Partitions.EFI.Size).To(Equal(uint(64)))
	})

	It("grows the images when the new source is bigger than the earlier one", func() {
		sp.Active.Size = 50
		sp.Passive.Size = 50
		sp.Recovery.Size = 50
		sp.Partitions.State.Size = 1150
		sp.Partitions.Recovery.Size = 300
		Expect(config.ResizeInstallSpecForSource(c, sp)).To(Succeed())
		Expect(sp.Active.Size).To(Equal(uint(105)))
		Expect(sp.Partitions.State.Size).To(Equal(uint(105*3 + 1000)))
		Expect(sp.Partitions.Recovery.Size).To(Equal(uint(105*2 + 200)))
	})

	It("keeps the sizes set in the config as minimums", func() {
		c.Collector = collector.Config{Values: collector.ConfigValues{
			"install": collector.ConfigValues{
				"system":          collector.ConfigValues{"size": 2000},
				"recovery-system": collector.ConfigValues{"size": 50},
				"partitions": collector.ConfigValues{
					"state":    collector.ConfigValues{"size": 20000},
					"recovery": collector.ConfigValues{"size": 100},
				},
			},
		}}
		Expect(config.ResizeInstallSpecForSource(c, sp)).To(Succeed())
		Expect(sp.Active.Size).To(Equal(uint(2000)))
		Expect(sp.Passive.Size).To(Equal(uint(105)))
		// Smaller than the measured source, so the measurement wins
		Expect(sp.Recovery.Size).To(Equal(uint(105)))
		Expect(sp.Partitions.State.Size).To(Equal(uint(20000)))
		// Too small for the recovery image, so it is raised
		Expect(sp.Partitions.Recovery.Size).To(Equal(uint(105*2 + 200)))
	})

	It("fails and leaves the spec alone when the source cannot be measured", func() {
		sp.Active.Source = sdkImages.NewFileSrc("/does/not/exist")
		before := *sp
		beforeState := *sp.Partitions.State
		beforeRecovery := *sp.Partitions.Recovery
		err := config.ResizeInstallSpecForSource(c, sp)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("measuring the install source"))
		Expect(sp.Active.Size).To(Equal(before.Active.Size))
		Expect(sp.Passive.Size).To(Equal(before.Passive.Size))
		Expect(sp.Recovery.Size).To(Equal(before.Recovery.Size))
		Expect(*sp.Partitions.State).To(Equal(beforeState))
		Expect(*sp.Partitions.Recovery).To(Equal(beforeRecovery))
	})

	It("fails when the source holds no data", func() {
		Expect(fs.RemoveAll(filepath.Join(rootfs, "payload"))).To(Succeed())
		err := config.ResizeInstallSpecForSource(c, sp)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("no data found"))
		Expect(sp.Active.Size).To(Equal(uint(3072)))
	})

	It("fails when there is no source", func() {
		sp.Active.Source = nil
		Expect(config.ResizeInstallSpecForSource(c, sp)).ToNot(Succeed())
		Expect(sp.Active.Size).To(Equal(uint(3072)))
	})

	It("fails and leaves the spec alone when the config cannot be read", func() {
		c.Collector = collector.Config{Values: collector.ConfigValues{
			"install": collector.ConfigValues{
				"system": collector.ConfigValues{"size": "not-a-number"},
			},
		}}
		Expect(config.ResizeInstallSpecForSource(c, sp)).ToNot(Succeed())
		Expect(sp.Active.Size).To(Equal(uint(3072)))
		Expect(sp.Partitions.State.Size).To(Equal(uint(10216)))
	})
})
