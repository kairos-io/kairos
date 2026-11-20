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

package action_test

import (
	"fmt"
	"path/filepath"

	"github.com/kairos-io/kairos/v4/agent/pkg/action"
	"github.com/kairos-io/kairos/v4/agent/pkg/config"
	"github.com/kairos-io/kairos/v4/agent/pkg/constants"
	v1 "github.com/kairos-io/kairos/v4/agent/pkg/implementations/spec"
	v1mock "github.com/kairos-io/kairos/v4/agent/tests/mocks"
	"github.com/kairos-io/kairos/v4/sdk/collector"
	ghwMock "github.com/kairos-io/kairos/v4/sdk/ghw/mocks"
	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
	sdkImages "github.com/kairos-io/kairos/v4/sdk/types/images"
	sdkLogger "github.com/kairos-io/kairos/v4/sdk/types/logger"
	sdkPartitions "github.com/kairos-io/kairos/v4/sdk/types/partitions"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/twpayne/go-vfs/v5"
	"github.com/twpayne/go-vfs/v5/vfst"
	"k8s.io/mount-utils"
)

// isoSizeMounter supplies the files a real ISO mount would expose.
type isoSizeMounter struct {
	mount.Interface
	fs            vfs.FS
	recovery      bool
	missingRootfs bool
}

func (m *isoSizeMounter) Mount(source, target, fsType string, options []string) error {
	if filepath.Base(target) == "rootfs" {
		if m.missingRootfs {
			return m.fs.RemoveAll(target)
		}
		if err := m.fs.WriteFile(filepath.Join(target, "os"), []byte("rootfs"), 0644); err != nil {
			return err
		}
		if err := m.fs.Truncate(filepath.Join(target, "os"), 200*1024*1024); err != nil {
			return err
		}
		if m.recovery {
			path := filepath.Join(filepath.Dir(target), "iso", constants.RecoverySquashFile)
			if err := m.fs.WriteFile(path, []byte("recovery"), 0644); err != nil {
				return err
			}
			if err := m.fs.Truncate(path, 50*1024*1024); err != nil {
				return err
			}
		}
	}
	return m.Interface.Mount(source, target, fsType, options)
}

var _ = Describe("Install ISO sizing", func() {
	var cfg *sdkConfig.Config
	var sp *v1.InstallSpec
	var mounter *isoSizeMounter
	var runner *v1mock.FakeRunner
	var stages *v1mock.FakeCloudInitRunner
	var fs *vfst.TestFS

	BeforeEach(func() {
		var cleanup func()
		var err error
		fs, cleanup, err = vfst.NewTestFS(map[string]interface{}{"/install.iso": "ISO", "/passive": "passive"})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(cleanup)
		mounter = &isoSizeMounter{Interface: &mount.FakeMounter{}, fs: fs}
		runner = v1mock.NewFakeRunner()
		runner.SideEffect = func(cmd string, args ...string) ([]byte, error) {
			if cmd == "blkdeactivate" {
				return nil, fmt.Errorf("stopped before partitioning")
			}
			if cmd == "lsblk" {
				return []byte(`{"blockdevices":[{"label":"COS_ACTIVE","type":"loop","path":"/dev/loop0"}]}`), nil
			}
			return nil, nil
		}
		stages = &v1mock.FakeCloudInitRunner{}
		cfg = config.NewConfig(config.WithFs(fs), config.WithLogger(sdkLogger.NewNullLogger()), config.WithMounter(mounter), config.WithRunner(runner), config.WithCloudInitRunner(stages))
		cfg.Collector = collector.Config{Values: collector.ConfigValues{"install": map[string]interface{}{
			"iso":     "file:/install.iso",
			"passive": map[string]interface{}{"source": "file:/passive", "size": 800},
		}}}
		sp, err = config.NewInstallSpec(cfg)
		Expect(err).NotTo(HaveOccurred())
	})

	DescribeTable("sizes ISO sources before no-format and honors caller passive minimums",
		func(minimum uint, recovery bool, expectedRecovery uint) {
			sp.NoFormat = true
			sp.Passive.Size = minimum
			mounter.recovery = recovery
			// A tiny disk must not introduce a new no-format capacity check.
			disks := ghwMock.GhwMock{}
			disks.AddDisk(sdkPartitions.Disk{Name: "tiny", SizeBytes: 1, Partitions: sdkPartitions.PartitionList{{Name: "tiny1", FilesystemLabel: "COS_ACTIVE", FS: "ext4"}}})
			disks.CreateDevices()
			DeferCleanup(disks.Clean)
			sp.Target = "/dev/tiny"
			err := action.NewInstallAction(cfg, sp).Run()
			Expect(err).To(MatchError(ContainSubstring("use `force` flag")))
			Expect(sp.Active.Size).To(Equal(uint(300)))
			Expect(sp.Recovery.Size).To(Equal(expectedRecovery))
			Expect(sp.Passive.Size).To(Equal(minimum))
			Expect(sp.Passive.Source.String()).To(Equal("file:///passive"))
			Expect(sp.Partitions.State.Size).To(Equal(1600 + minimum))
			Expect(sp.Partitions.Recovery.Size).To(Equal(2*expectedRecovery + 200))
			Expect(stages.ExecStages).NotTo(ContainElement("before-install"))
		},
		Entry("smaller caller size, recovery copy", uint(400), false, uint(300)),
		Entry("larger caller size, distinct ISO recovery", uint(900), true, uint(150)),
	)

	It("commits sizes before disk deactivation", func() {
		err := action.NewInstallAction(cfg, sp).Run()
		Expect(err).To(MatchError(ContainSubstring("stopped before partitioning")))
		Expect(sp.Active.Size).To(Equal(uint(300)))
		Expect(sp.Recovery.Size).To(Equal(uint(300)))
		Expect(sp.Passive.Size).To(Equal(uint(800)))
		Expect(sp.Partitions.State.Size).To(Equal(uint(2400)))
	})

	DescribeTable("preserves caller data and stops installation on sizing failure",
		func(failure string) {
			switch failure {
			case "rootfs":
				mounter.missingRootfs = true
			case "passive":
				sp.Passive.Source = sdkImages.NewFileSrc("/missing")
			case "disk", "mounted":
				disks := ghwMock.GhwMock{}
				disk := sdkPartitions.Disk{Name: "tiny", SizeBytes: 1}
				if failure == "mounted" {
					disk.Partitions = sdkPartitions.PartitionList{{Name: "tiny1", MountPoint: "/mounted", FS: "ext4"}}
				}
				disks.AddDisk(disk)
				disks.CreateDevices()
				DeferCleanup(disks.Clean)
				sp.Target = "/dev/tiny"
			}
			sp.Active.Source = sdkImages.NewDockerSrc("caller-active:latest")
			sp.Recovery.Source = sdkImages.NewDockerSrc("caller-recovery:latest")
			sp.Passive.Size = 400
			before := *sp
			stateBefore, recoveryBefore := *sp.Partitions.State, *sp.Partitions.Recovery
			err := action.NewInstallAction(cfg, sp).Run()
			Expect(err).To(HaveOccurred())
			if failure == "disk" {
				Expect(err.Error()).To(ContainSubstring("does not fit in the target disk"))
			} else if failure == "mounted" {
				Expect(err.Error()).To(ContainSubstring("has mounted partitions"))
			} else {
				Expect(err.Error()).To(ContainSubstring("sizing installation source"))
			}
			Expect(*sp).To(Equal(before))
			Expect(*sp.Partitions.State).To(Equal(stateBefore))
			Expect(*sp.Partitions.Recovery).To(Equal(recoveryBefore))
			Expect(runner.IncludesCmds([][]string{{"blkdeactivate"}})).NotTo(Succeed())
			Expect(stages.ExecStages).NotTo(BeEmpty())
			for _, stage := range stages.ExecStages {
				Expect(stage).To(HavePrefix("kairos-install.pre"))
			}
			mounts, err := mounter.List()
			Expect(err).NotTo(HaveOccurred())
			for _, mounted := range mounts {
				Expect(mounted.Path).NotTo(Equal(constants.StateDir))
			}
		},
		Entry("ISO rootfs", "rootfs"), Entry("independent passive source", "passive"), Entry("target capacity", "disk"), Entry("mounted target", "mounted"),
	)
})
