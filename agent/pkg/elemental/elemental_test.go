/*
   Copyright © 2021 SUSE LLC

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

package elemental_test

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	sc "syscall"
	"testing"

	"github.com/diskfs/go-diskfs"
	fileBackend "github.com/diskfs/go-diskfs/backend/file"
	"github.com/diskfs/go-diskfs/partition/gpt"
	"github.com/gofrs/uuid"
	agentConfig "github.com/kairos-io/kairos-agent/v2/pkg/config"
	cnst "github.com/kairos-io/kairos-agent/v2/pkg/constants"
	"github.com/kairos-io/kairos-agent/v2/pkg/elemental"
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/implementations/spec"
	"github.com/kairos-io/kairos-agent/v2/pkg/utils"
	fsutils "github.com/kairos-io/kairos-agent/v2/pkg/utils/fs"
	v1mock "github.com/kairos-io/kairos-agent/v2/tests/mocks"
	sdkConstants "github.com/kairos-io/kairos-sdk/constants"
	ghwMock "github.com/kairos-io/kairos-sdk/ghw/mocks"
	sdkConfig "github.com/kairos-io/kairos-sdk/types/config"
	sdkImages "github.com/kairos-io/kairos-sdk/types/images"
	Collector "github.com/kairos-io/kairos-sdk/types/logger"
	SdkPartitions "github.com/kairos-io/kairos-sdk/types/partitions"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/sanity-io/litter"
	"github.com/twpayne/go-vfs/v5/vfst"
	"golang.org/x/sys/unix"
)

func TestElementalSuite(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Elemental test suite")
}

var _ = Describe("Elemental", Label("elemental"), func() {
	var config *sdkConfig.Config
	var runner *v1mock.FakeRunner
	var logger Collector.KairosLogger
	var syscall *v1mock.FakeSyscall
	var cl *v1mock.FakeHTTPClient
	var mounter *v1mock.ErrorMounter
	var fs *vfst.TestFS
	var cleanup func()
	var extractor *v1mock.FakeImageExtractor
	var memLog *bytes.Buffer
	var devLoopInt int

	BeforeEach(func() {
		memLog = &bytes.Buffer{}
		logger = Collector.NewBufferLogger(memLog)
		logger.SetLevel("debug")
		runner = v1mock.NewFakeRunner()
		syscall = &v1mock.FakeSyscall{}
		devLoopInt = 44
		syscall.SideEffectSyscall = func(trap, a1, a2, a3 uintptr) (r1, r2 uintptr, err sc.Errno) {
			// Trap the call for getting a free loop device number
			if trap == sc.SYS_IOCTL && a2 == unix.LOOP_CTL_GET_FREE {
				// This is a "get free loop device" syscall
				// We return 44 so it gets the /dev/loop44 because we are cool like that
				// Also we can check below that indeed it set that device as expected
				return uintptr(devLoopInt), 0, sc.Errno(syscall.ReturnValue)
			}
			return 0, 0, sc.Errno(syscall.ReturnValue)
		}
		mounter = v1mock.NewErrorMounter()
		cl = &v1mock.FakeHTTPClient{}
		fs, cleanup, _ = vfst.NewTestFS(map[string]interface{}{
			"/dev/loop-control":                    "",
			fmt.Sprintf("/dev/loop%d", devLoopInt): "",
		})
		extractor = v1mock.NewFakeImageExtractor(logger)
		config = agentConfig.NewConfig(
			agentConfig.WithFs(fs),
			agentConfig.WithRunner(runner),
			agentConfig.WithLogger(logger),
			agentConfig.WithMounter(mounter),
			agentConfig.WithSyscall(syscall),
			agentConfig.WithClient(cl),
			agentConfig.WithImageExtractor(extractor),
		)
	})
	AfterEach(func() { cleanup() })
	Describe("MountRWPartition", Label("mount"), func() {
		var el *elemental.Elemental
		var parts SdkPartitions.ElementalPartitions
		BeforeEach(func() {
			spec := &v1.InstallSpec{}
			parts = agentConfig.NewInstallElementalPartitions(logger, spec)

			err := fsutils.MkdirAll(fs, "/some", cnst.DirPerm)
			Expect(err).ToNot(HaveOccurred())
			_, err = fs.Create("/some/device")
			Expect(err).ToNot(HaveOccurred())

			parts.OEM.Path = "/dev/device1"

			el = elemental.NewElemental(config)
		})

		It("Mounts and umounts a partition with RW", func() {
			umount, err := el.MountRWPartition(parts.OEM)
			Expect(err).To(BeNil())
			lst, _ := mounter.List()
			Expect(len(lst)).To(Equal(1))
			Expect(lst[0].Opts).To(Equal([]string{"rw"}))

			Expect(umount()).ShouldNot(HaveOccurred())
			lst, _ = mounter.List()
			Expect(len(lst)).To(Equal(0))
		})
		It("Remounts a partition with RW", func() {
			err := el.MountPartition(parts.OEM)
			Expect(err).To(BeNil())
			lst, _ := mounter.List()
			Expect(len(lst)).To(Equal(1))

			umount, err := el.MountRWPartition(parts.OEM)
			Expect(err).To(BeNil())
			lst, _ = mounter.List()
			// fake mounter is not merging remounts it just appends
			Expect(len(lst)).To(Equal(2))
			Expect(lst[1].Opts).To(Equal([]string{"remount", "rw"}))

			Expect(umount()).ShouldNot(HaveOccurred())
			// This went to syscall so it wont appears on the mounter list
			Expect(syscall.WasMountCalledWith(parts.OEM.MountPoint, parts.OEM.MountPoint, "", unix.MS_REMOUNT|unix.MS_RDONLY|unix.MS_BIND, "")).To(BeTrue())
		})
		It("Fails to mount a partition", func() {
			mounter.ErrorOnMount = true
			_, err := el.MountRWPartition(parts.OEM)
			Expect(err).Should(HaveOccurred())
		})
		It("Fails to remount a partition", func() {
			err := el.MountPartition(parts.OEM)
			Expect(err).To(BeNil())
			lst, _ := mounter.List()
			Expect(len(lst)).To(Equal(1))

			mounter.ErrorOnMount = true
			_, err = el.MountRWPartition(parts.OEM)
			Expect(err).Should(HaveOccurred())
			lst, _ = mounter.List()
			Expect(len(lst)).To(Equal(1))
		})
	})
	Describe("MountPartitions", Label("MountPartitions", "disk", "partition", "mount"), func() {
		var el *elemental.Elemental
		var parts SdkPartitions.ElementalPartitions
		BeforeEach(func() {
			spec := &v1.InstallSpec{}
			parts = agentConfig.NewInstallElementalPartitions(logger, spec)

			err := fsutils.MkdirAll(fs, "/some", cnst.DirPerm)
			Expect(err).ToNot(HaveOccurred())
			_, err = fs.Create("/some/device")
			Expect(err).ToNot(HaveOccurred())

			parts.OEM.Path = "/dev/device2"
			parts.Recovery.Path = "/dev/device3"
			parts.State.Path = "/dev/device4"
			parts.Persistent.Path = "/dev/device5"

			el = elemental.NewElemental(config)
		})

		It("Mounts disk partitions", func() {
			err := el.MountPartitions(parts.PartitionsByMountPoint(false))
			Expect(err).To(BeNil())
			lst, _ := mounter.List()
			Expect(len(lst)).To(Equal(4))
		})

		It("Mounts disk partitions excluding recovery", func() {
			err := el.MountPartitions(parts.PartitionsByMountPoint(false, parts.Recovery))
			Expect(err).To(BeNil())
			lst, _ := mounter.List()
			for _, i := range lst {
				Expect(i.Path).NotTo(Equal("/dev/device3"))
			}
		})

		It("Fails if some partition resists to mount ", func() {
			mounter.ErrorOnMount = true
			err := el.MountPartitions(parts.PartitionsByMountPoint(false))
			Expect(err).NotTo(BeNil())
		})

		It("Fails if oem partition is not found ", func() {
			parts.OEM.Path = ""
			err := el.MountPartitions(parts.PartitionsByMountPoint(false))
			Expect(err).NotTo(BeNil())
		})
	})
	Describe("UnmountPartitions", Label("UnmountPartitions", "disk", "partition", "unmount"), func() {
		var el *elemental.Elemental
		var parts SdkPartitions.ElementalPartitions
		BeforeEach(func() {
			spec := &v1.InstallSpec{}
			parts = agentConfig.NewInstallElementalPartitions(logger, spec)

			err := fsutils.MkdirAll(fs, "/some", cnst.DirPerm)
			Expect(err).ToNot(HaveOccurred())
			_, err = fs.Create("/some/device")
			Expect(err).ToNot(HaveOccurred())

			parts.OEM.Path = "/dev/device2"
			parts.Recovery.Path = "/dev/device3"
			parts.State.Path = "/dev/device4"
			parts.Persistent.Path = "/dev/device5"

			el = elemental.NewElemental(config)
			err = el.MountPartitions(parts.PartitionsByMountPoint(false))
			Expect(err).ToNot(HaveOccurred())
		})

		It("Unmounts disk partitions", func() {
			err := el.UnmountPartitions(parts.PartitionsByMountPoint(true))
			Expect(err).To(BeNil())
			lst, _ := mounter.List()
			Expect(len(lst)).To(Equal(0))
		})

		It("Fails to unmount disk partitions", func() {
			mounter.ErrorOnUnmount = true
			err := el.UnmountPartitions(parts.PartitionsByMountPoint(true))
			Expect(err).NotTo(BeNil())
		})
	})
	Describe("MountImage", Label("MountImage", "mount", "image"), func() {
		var el *elemental.Elemental
		var img *sdkImages.Image
		BeforeEach(func() {
			el = elemental.NewElemental(config)
			img = &sdkImages.Image{MountPoint: "/some/mountpoint", File: "/image.file"}
			Expect(fs.WriteFile("/image.file", []byte{}, cnst.FilePerm)).To(Succeed())
		})

		It("Mounts file system image", func() {
			err := el.MountImage(img)
			Expect(err).To(BeNil())
			Expect(img.LoopDevice).To(Equal(fmt.Sprintf("/dev/loop%d", devLoopInt)), litter.Sdump(img))
		})

		It("Fails to set a loop device", Label("loop"), func() {
			// Return error on syscall call
			syscall.ReturnValue = 10
			Expect(el.MountImage(img)).NotTo(BeNil())
			Expect(img.LoopDevice).To(Equal(""))
		})

		It("Fails to mount a loop device", Label("loop"), func() {
			unloopCalled := false
			syscall.SideEffectSyscall = func(trap, a1, a2, a3 uintptr) (r1, r2 uintptr, err sc.Errno) {
				if trap == sc.SYS_IOCTL && a2 == unix.LOOP_CTL_GET_FREE {
					return uintptr(devLoopInt), 0, 0
				}
				if trap == sc.SYS_IOCTL && a2 == unix.LOOP_CLR_FD {
					unloopCalled = true
				}
				return 0, 0, 0
			}
			mounter.ErrorOnMount = true
			Expect(el.MountImage(img)).NotTo(BeNil())
			Expect(unloopCalled).To(BeTrue())
			Expect(img.LoopDevice).To(Equal(""))
		})

		It("Reports mount and loop cleanup errors", Label("loop"), func() {
			syscall.SideEffectSyscall = func(trap, a1, a2, a3 uintptr) (r1, r2 uintptr, err sc.Errno) {
				if trap == sc.SYS_IOCTL && a2 == unix.LOOP_CTL_GET_FREE {
					return uintptr(devLoopInt), 0, 0
				}
				if trap == sc.SYS_IOCTL && a2 == unix.LOOP_CLR_FD {
					return 0, 0, sc.EIO
				}
				return 0, 0, 0
			}
			mounter.ErrorOnMount = true

			err := el.MountImage(img)

			Expect(err).To(MatchError(And(
				ContainSubstring("mount error"),
				ContainSubstring("input/output error"),
			)))
			Expect(img.LoopDevice).To(Equal(""))
		})
	})
	Describe("UnmountImage", Label("UnmountImage", "mount", "image"), func() {
		var el *elemental.Elemental
		var img *sdkImages.Image
		BeforeEach(func() {
			el = elemental.NewElemental(config)
			img = &sdkImages.Image{MountPoint: "/some/mountpoint", File: "/image.file"}
			Expect(fs.WriteFile("/image.file", []byte{}, cnst.FilePerm)).To(Succeed())
			Expect(el.MountImage(img)).To(BeNil())
			Expect(img.LoopDevice).To(Equal(fmt.Sprintf("/dev/loop%d", devLoopInt)))
		})

		It("Unmounts file system image", func() {
			Expect(el.UnmountImage(img)).To(BeNil())
			Expect(img.LoopDevice).To(Equal(""))
		})

		It("Fails to unmount a mountpoint", func() {
			mounter.ErrorOnUnmount = true
			Expect(el.UnmountImage(img)).NotTo(BeNil())
		})

		It("Fails to unset a loop device", Label("loop"), func() {
			syscall.ReturnValue = 10
			Expect(el.UnmountImage(img)).NotTo(BeNil())
		})
	})
	Describe("CreateFileSystemImage", Label("CreateFileSystemImage", "image"), func() {
		var el *elemental.Elemental
		var img *sdkImages.Image
		BeforeEach(func() {
			img = &sdkImages.Image{
				Label:      cnst.ActiveLabel,
				Size:       32,
				File:       filepath.Join(cnst.StateDir, "cOS", cnst.ActiveImgFile),
				FS:         cnst.LinuxImgFs,
				MountPoint: cnst.ActiveDir,
				Source:     sdkImages.NewDirSrc(cnst.IsoBaseTree),
			}
			_ = fsutils.MkdirAll(fs, cnst.IsoBaseTree, cnst.DirPerm)
			el = elemental.NewElemental(config)
		})

		It("Creates a new file system image", func() {
			_, err := fs.Stat(img.File)
			Expect(err).NotTo(BeNil())
			err = el.CreateFileSystemImage(img)
			Expect(err).To(BeNil())
			stat, err := fs.Stat(img.File)
			Expect(err).To(BeNil())
			Expect(stat.Size()).To(Equal(int64(32 * 1024 * 1024)))
		})

		It("Fails formatting a file system image", Label("format"), func() {
			runner.ReturnError = errors.New("run error")
			_, err := fs.Stat(img.File)
			Expect(err).NotTo(BeNil())
			err = el.CreateFileSystemImage(img)
			Expect(err).NotTo(BeNil())
			_, err = fs.Stat(img.File)
			Expect(err).NotTo(BeNil())
		})
	})
	Describe("FormatPartition", Label("FormatPartition", "partition", "format"), func() {
		It("Reformats an already existing partition", func() {
			el := elemental.NewElemental(config)
			part := &SdkPartitions.Partition{
				Path:            "/dev/device1",
				FS:              "ext4",
				FilesystemLabel: "MY_LABEL",
			}
			Expect(el.FormatPartition(part)).To(BeNil())
		})

	})
	Describe("PartitionAndFormatDevice", Label("PartitionAndFormatDevice", "partition", "format"), func() {
		var cInit *v1mock.FakeCloudInitRunner
		var install *v1.InstallSpec
		var err error
		var el *elemental.Elemental
		var tmpDir string

		BeforeEach(func() {
			cInit = &v1mock.FakeCloudInitRunner{ExecStages: []string{}, Error: false}
			config.CloudInitRunner = cInit
			tmpDir, err = os.MkdirTemp("", "elements-*")
			Expect(err).To(BeNil())
			Expect(os.RemoveAll(filepath.Join(tmpDir, "/test.img"))).ToNot(HaveOccurred())
			// at least 2Gb in size as state is set to 1G
			_, err = fileBackend.CreateFromPath(filepath.Join(tmpDir, "/test.img"), 2*1024*1024*1024)
			Expect(err).ToNot(HaveOccurred())
			config.Install.Device = filepath.Join(tmpDir, "/test.img")
			install, err = agentConfig.NewInstallSpec(config)
			Expect(err).ToNot(HaveOccurred())
			install.Target = filepath.Join(tmpDir, "/test.img")
			el = elemental.NewElemental(config)
		})

		AfterEach(func() {
			Expect(os.RemoveAll(tmpDir)).ToNot(HaveOccurred())
		})

		It("Successfully creates partitions and formats them, EFI boot", func() {
			install.PartTable = sdkConstants.GPT
			install.Firmware = sdkConstants.EFI
			Expect(install.Partitions.SetFirmwarePartitions(sdkConstants.EFI, sdkConstants.GPT)).To(BeNil())
			Expect(el.PartitionAndFormatDevice(install)).To(BeNil())
			disk, err := fileBackend.OpenFromPath(filepath.Join(tmpDir, "/test.img"), true)
			defer disk.Close()
			table, err := gpt.Read(disk, int(diskfs.SectorSize512), int(diskfs.SectorSize512))
			Expect(err).ToNot(HaveOccurred())
			// check that its type GPT
			Expect(table.Type()).To(Equal("gpt"))
			// Expect the disk UUID to be constant
			Expect(strings.ToLower(table.UUID())).To(Equal(strings.ToLower(cnst.DiskUUID)))
			// 5 partitions (boot, oem, recovery, state and persistent)
			Expect(len(table.GetPartitions())).To(Equal(5))
			// Cast the boot partition into specific type to check the type and such
			part := table.GetPartitions()[0]
			partition, ok := part.(*gpt.Partition)
			Expect(ok).To(BeTrue())
			// Should be efi type
			Expect(partition.Type).To(Equal(gpt.EFISystemPartition))
			// should have boot label
			Expect(partition.Name).To(Equal(cnst.EfiPartName))
			// Should have predictable UUID
			Expect(strings.ToLower(partition.UUID())).To(Equal(strings.ToLower(uuid.NewV5(uuid.NamespaceURL, cnst.EfiLabel).String())))
			// Check the rest have the proper types
			for i := 1; i < len(table.GetPartitions()); i++ {
				part := table.GetPartitions()[i]
				partition, ok := part.(*gpt.Partition)
				Expect(ok).To(BeTrue())
				// all of them should have the Linux fs type
				Expect(partition.Type).To(Equal(gpt.LinuxFilesystem))
			}
		})
		It("Preserves PARTLABEL on an extra partition when persistent has a fixed size (kairos-io/kairos#4257)", func() {
			// Re-create a bigger backing file: the default 2GiB one is not
			// large enough to hold the state/recovery defaults plus a fixed
			// persistent + a fixed extra partition.
			imgPath := filepath.Join(tmpDir, "/test.img")
			Expect(os.RemoveAll(imgPath)).ToNot(HaveOccurred())
			_, err = fileBackend.CreateFromPath(imgPath, 8*1024*1024*1024)
			Expect(err).ToNot(HaveOccurred())

			install.PartTable = sdkConstants.GPT
			install.Firmware = sdkConstants.EFI
			Expect(install.Partitions.SetFirmwarePartitions(sdkConstants.EFI, sdkConstants.GPT)).To(BeNil())

			// This is the config from the bug report, scaled down: both
			// persistent and the extra partition have a fixed size, so the
			// extra is written to disk as the LAST partition with a fixed
			// size instead of expanding to fill.
			install.Partitions.Persistent.Size = 1024
			install.ExtraPartitions = SdkPartitions.PartitionList{
				{
					Name:            "data_partition",
					FilesystemLabel: "SYSTEM_DATA",
					FS:              "ext4",
					Size:            1024,
				},
			}

			Expect(el.PartitionAndFormatDevice(install)).To(BeNil())

			disk, err := fileBackend.OpenFromPath(imgPath, true)
			Expect(err).ToNot(HaveOccurred())
			defer disk.Close()
			table, err := gpt.Read(disk, int(diskfs.SectorSize512), int(diskfs.SectorSize512))
			Expect(err).ToNot(HaveOccurred())

			var data *gpt.Partition
			var persistent *gpt.Partition
			for _, raw := range table.GetPartitions() {
				p := raw.(*gpt.Partition)
				switch p.Name {
				case "data_partition":
					data = p
				case sdkConstants.PersistentPartName:
					persistent = p
				}
			}

			Expect(data).ToNot(BeNil(),
				"extra partition data_partition missing from on-disk GPT — PARTLABEL will not resolve via /dev/disk/by-partlabel")
			Expect(data.Name).To(Equal("data_partition"))

			const sectorSize uint64 = 512
			const mib = uint64(1024 * 1024)
			dataBytes := (data.End - data.Start + 1) * sectorSize
			Expect(dataBytes).To(BeNumerically("<=", 1024*mib),
				"extra data_partition grew beyond its configured 1024 MiB size")

			Expect(persistent).ToNot(BeNil())
			persistentBytes := (persistent.End - persistent.Start + 1) * sectorSize
			Expect(persistentBytes).To(Equal(1024 * mib),
				"persistent should keep its configured 1024 MiB size")
		})
		It("Refuses config when persistent + extras exceed target disk size", func() {
			// 4 GiB disk is not enough to hold state/recovery defaults plus a
			// 3 GiB persistent + 3 GiB extra partition. Sanitize() must
			// catch it before we go anywhere near the partitioner.
			imgPath := filepath.Join(tmpDir, "/test.img")
			Expect(os.RemoveAll(imgPath)).ToNot(HaveOccurred())
			_, err = fileBackend.CreateFromPath(imgPath, 4*1024*1024*1024)
			Expect(err).ToNot(HaveOccurred())

			install.PartTable = sdkConstants.GPT
			install.Firmware = sdkConstants.EFI
			Expect(install.Partitions.SetFirmwarePartitions(sdkConstants.EFI, sdkConstants.GPT)).To(BeNil())

			install.Partitions.Persistent.Size = 3 * 1024
			install.ExtraPartitions = SdkPartitions.PartitionList{
				{
					Name:            "data_partition",
					FilesystemLabel: "SYSTEM_DATA",
					FS:              "ext4",
					Size:            3 * 1024,
				},
			}

			// Sanitize can't consult a ghw fixture in this test, so
			// checkPartitionsFitTargetDisk is skipped there — exercise
			// the disk-open path instead by going straight to
			// PartitionAndFormatDevice on a too-small image.
			err = el.PartitionAndFormatDevice(install)
			if err == nil {
				// Not failing outright means partitions ended up outside
				// the disk. Read the GPT back and confirm the corruption
				// so the test names the exact failure mode.
				disk, dErr := fileBackend.OpenFromPath(imgPath, true)
				Expect(dErr).ToNot(HaveOccurred())
				defer disk.Close()
				table, tErr := gpt.Read(disk, int(diskfs.SectorSize512), int(diskfs.SectorSize512))
				Expect(tErr).ToNot(HaveOccurred())
				diskSectors := uint64(4*1024*1024*1024) / 512
				for _, raw := range table.GetPartitions() {
					p := raw.(*gpt.Partition)
					if p.Name == "" {
						continue
					}
					Expect(p.End).To(BeNumerically("<", diskSectors),
						fmt.Sprintf("partition %q end=%d overflows disk sector count %d — GPT is corrupted",
							p.Name, p.End, diskSectors))
				}
				Fail("PartitionAndFormatDevice accepted a layout larger than the target disk; " +
					"kairos-io/kairos#4257: silent overflow corrupts the on-disk GPT and drops PARTLABELs")
			}
			Expect(err).To(HaveOccurred())
		})
		It("Writes a partition-fit-checkable disk when persistent + extras fit", func() {
			// Positive control for the previous case: same layout but
			// on a disk that can actually hold everything.
			imgPath := filepath.Join(tmpDir, "/test.img")
			Expect(os.RemoveAll(imgPath)).ToNot(HaveOccurred())
			_, err = fileBackend.CreateFromPath(imgPath, 12*1024*1024*1024)
			Expect(err).ToNot(HaveOccurred())

			install.PartTable = sdkConstants.GPT
			install.Firmware = sdkConstants.EFI
			Expect(install.Partitions.SetFirmwarePartitions(sdkConstants.EFI, sdkConstants.GPT)).To(BeNil())

			install.Partitions.Persistent.Size = 3 * 1024
			install.ExtraPartitions = SdkPartitions.PartitionList{
				{
					Name:            "data_partition",
					FilesystemLabel: "SYSTEM_DATA",
					FS:              "ext4",
					Size:            3 * 1024,
				},
			}

			Expect(el.PartitionAndFormatDevice(install)).To(BeNil())
		})
		It("Successfully creates partitions and formats them, BIOS boot", func() {
			install.PartTable = sdkConstants.GPT
			install.Firmware = sdkConstants.BIOS
			Expect(install.Partitions.SetFirmwarePartitions(sdkConstants.BIOS, sdkConstants.GPT)).To(BeNil())
			Expect(el.PartitionAndFormatDevice(install)).To(BeNil())
			disk, err := fileBackend.OpenFromPath(filepath.Join(tmpDir, "/test.img"), true)
			defer disk.Close()
			Expect(err).ToNot(HaveOccurred())
			table, err := gpt.Read(disk, int(diskfs.SectorSize512), int(diskfs.SectorSize512))
			Expect(err).ToNot(HaveOccurred())
			// check that its type GPT
			Expect(table.Type()).To(Equal("gpt"))
			// Expect the disk UUID to be constant
			Expect(strings.ToLower(table.UUID())).To(Equal(strings.ToLower(cnst.DiskUUID)))
			// 5 partitions (boot, oem, recovery, state and persistent)
			Expect(len(table.GetPartitions())).To(Equal(5))
			// Cast the boot partition into specific type to check the type and such
			part := table.GetPartitions()[0]
			partition, ok := part.(*gpt.Partition)
			Expect(ok).To(BeTrue())
			// Should be BIOS boot type
			Expect(partition.Type).To(Equal(gpt.BIOSBoot))
			// should have boot label
			Expect(partition.Name).To(Equal(cnst.BiosPartName))
			// Should have predictable UUID
			Expect(strings.ToLower(partition.UUID())).To(Equal(strings.ToLower(uuid.NewV5(uuid.NamespaceURL, cnst.EfiLabel).String())))
			for i := 1; i < len(table.GetPartitions()); i++ {
				part := table.GetPartitions()[i]
				partition, ok := part.(*gpt.Partition)
				Expect(ok).To(BeTrue())
				// all of them should have the Linux fs type
				Expect(partition.Type).To(Equal(gpt.LinuxFilesystem))
			}
		})
		It("defaults an extra partition with no fs set to ext2", func() {
			install.PartTable = sdkConstants.GPT
			install.Firmware = sdkConstants.EFI
			Expect(install.Partitions.SetFirmwarePartitions(sdkConstants.EFI, sdkConstants.GPT)).To(BeNil())
			install.ExtraPartitions = SdkPartitions.PartitionList{
				&SdkPartitions.Partition{
					Name:            "data_partition",
					FilesystemLabel: "SYSTEM_DATA",
					Size:            100,
				},
			}
			runner.ClearCmds()
			Expect(el.PartitionAndFormatDevice(install)).To(BeNil())

			disk, err := fileBackend.OpenFromPath(filepath.Join(tmpDir, "/test.img"), true)
			Expect(err).ToNot(HaveOccurred())
			defer disk.Close()
			table, err := gpt.Read(disk, int(diskfs.SectorSize512), int(diskfs.SectorSize512))
			Expect(err).ToNot(HaveOccurred())
			// The extra partition is created, on top of the 5 default ones
			Expect(len(table.GetPartitions())).To(Equal(6))
			names := []string{}
			for _, p := range table.GetPartitions() {
				names = append(names, p.(*gpt.Partition).Name)
			}
			Expect(names).To(ContainElement("data_partition"))
			Expect(runner.IncludesCmds([][]string{{"mkfs.ext2", "-L", "SYSTEM_DATA"}})).To(Succeed())
		})
		It("leaves extra partitions with noformat filesystem values unformatted", func() {
			install.PartTable = sdkConstants.GPT
			install.Firmware = sdkConstants.EFI
			Expect(install.Partitions.SetFirmwarePartitions(sdkConstants.EFI, sdkConstants.GPT)).To(BeNil())
			install.ExtraPartitions = SdkPartitions.PartitionList{
				&SdkPartitions.Partition{Name: "dash_partition", FS: "-", Size: 100},
				&SdkPartitions.Partition{Name: "none_partition", FS: "none", Size: 100},
				&SdkPartitions.Partition{Name: "noformat_partition", FS: "noformat", Size: 100},
			}
			runner.ClearCmds()
			Expect(el.PartitionAndFormatDevice(install)).To(BeNil())

			for _, fs := range []string{"-", "none", "noformat"} {
				Expect(runner.IncludesCmds([][]string{{"mkfs." + fs}})).To(HaveOccurred())
			}
		})
	})
	Describe("DeployImage", Label("DeployImage"), func() {
		var el *elemental.Elemental
		var img *sdkImages.Image
		var cmdFail string
		BeforeEach(func() {
			sourceDir, err := fsutils.TempDir(fs, "", "elemental")
			Expect(err).ShouldNot(HaveOccurred())
			destDir, err := fsutils.TempDir(fs, "", "elemental")
			Expect(err).ShouldNot(HaveOccurred())
			cmdFail = ""
			el = elemental.NewElemental(config)
			img = &sdkImages.Image{
				FS:         cnst.LinuxImgFs,
				Size:       16,
				Source:     sdkImages.NewDirSrc(sourceDir),
				MountPoint: destDir,
				File:       filepath.Join(destDir, "image.img"),
				Label:      "some_label",
			}
			runner.SideEffect = func(cmd string, args ...string) ([]byte, error) {
				if cmdFail == cmd {
					return []byte{}, errors.New("Command failed")
				}
				switch cmd {
				default:
					GinkgoWriter.Println(fmt.Sprintf("Command %s called but we dont catch it", cmd))
					return []byte{}, nil
				}
			}
		})
		It("Deploys an image from a directory and leaves it mounted", func() {
			Expect(el.DeployImage(img, true)).To(BeNil())
		})
		It("Deploys an image from a directory and leaves it unmounted", func() {
			Expect(el.DeployImage(img, false)).To(BeNil())
		})
		It("Deploys an squashfs image from a directory", func() {
			img.FS = cnst.SquashFs
			Expect(el.DeployImage(img, true)).To(BeNil())
			Expect(runner.MatchMilestones([][]string{
				{
					"mksquashfs", "/tmp/elemental-tmp", "/tmp/elemental/image.img",
					"-b", "1024k", "-comp", "gzip",
				},
			}))
		})
		It("Deploys a file image and mounts it", func() {
			sourceImg := "/source.img"
			_, err := fs.Create(sourceImg)
			Expect(err).To(BeNil())
			destDir, err := fsutils.TempDir(fs, "", "elemental")
			Expect(err).To(BeNil())
			img.Source = sdkImages.NewFileSrc(sourceImg)
			img.MountPoint = destDir
			Expect(el.DeployImage(img, true)).To(BeNil())
		})
		It("Deploys a file image and fails to mount it", func() {
			sourceImg := "/source.img"
			_, err := fs.Create(sourceImg)
			Expect(err).To(BeNil())
			destDir, err := fsutils.TempDir(fs, "", "elemental")
			Expect(err).To(BeNil())
			img.Source = sdkImages.NewFileSrc(sourceImg)
			img.MountPoint = destDir
			mounter.ErrorOnMount = true
			_, err = el.DeployImage(img, true)
			Expect(err).NotTo(BeNil())
		})
		It("Deploys a file image and fails to label it", func() {
			sourceImg := "/source.img"
			_, err := fs.Create(sourceImg)
			Expect(err).To(BeNil())
			destDir, err := fsutils.TempDir(fs, "", "elemental")
			Expect(err).To(BeNil())
			img.Source = sdkImages.NewFileSrc(sourceImg)
			img.MountPoint = destDir
			cmdFail = "tune2fs"
			_, err = el.DeployImage(img, true)
			Expect(err).NotTo(BeNil())
		})
		It("Fails creating the squashfs filesystem", func() {
			cmdFail = "mksquashfs"
			img.FS = cnst.SquashFs
			_, err := el.DeployImage(img, true)
			Expect(err).NotTo(BeNil())
			Expect(runner.MatchMilestones([][]string{
				{
					"mksquashfs", "/tmp/elemental-tmp", "/tmp/elemental/image.img",
					"-b", "1024k", "-comp", "gzip",
				},
			}))
		})
		It("Fails formatting the image", func() {
			cmdFail = "mkfs.ext2"
			_, err := el.DeployImage(img, true)
			Expect(err).NotTo(BeNil())
		})
		It("Fails mounting the image", func() {
			mounter.ErrorOnMount = true
			_, err := el.DeployImage(img, true)
			Expect(err).NotTo(BeNil())
		})
		It("Fails unmounting the image after copying", func() {
			mounter.ErrorOnUnmount = true
			_, err := el.DeployImage(img, false)
			Expect(err).NotTo(BeNil())
		})
	})
	Describe("DumpSource", Label("dump"), func() {
		var e *elemental.Elemental
		var destDir string
		BeforeEach(func() {
			var err error
			e = elemental.NewElemental(config)
			destDir, err = fsutils.TempDir(fs, "", "elemental")
			Expect(err).ShouldNot(HaveOccurred())
		})
		It("Copies files from a directory source", func() {
			rsyncCount := 0
			src := ""
			dest := ""
			runner.SideEffect = func(cmd string, args ...string) ([]byte, error) {
				if cmd == cnst.Rsync {
					rsyncCount += 1
					src = args[len(args)-2]
					dest = args[len(args)-1]

				}
				return []byte{}, nil
			}
			_, err := e.DumpSource("/dest", sdkImages.NewDirSrc("/source"))
			Expect(err).ShouldNot(HaveOccurred())
			Expect(rsyncCount).To(Equal(1))
			Expect(src).To(HaveSuffix("/source/"))
			Expect(dest).To(HaveSuffix("/dest/"))
		})
		It("Unpacks a docker image to target", Label("docker"), func() {
			_, err := e.DumpSource(destDir, sdkImages.NewDockerSrc("docker/image:latest"))
			Expect(err).To(BeNil())
		})
		It("Unpacks a docker image to target with cosign validation", Label("docker", "cosign"), func() {
			config.Cosign = true
			_, err := e.DumpSource(destDir, sdkImages.NewDockerSrc("docker/image:latest"))
			Expect(err).To(BeNil())
			Expect(runner.CmdsMatch([][]string{{"cosign", "verify", "docker/image:latest"}}))
		})
		It("Fails cosign validation", Label("cosign"), func() {
			runner.ReturnError = errors.New("cosign error")
			config.Cosign = true
			_, err := e.DumpSource(destDir, sdkImages.NewDockerSrc("docker/image:latest"))
			Expect(err).NotTo(BeNil())
			Expect(runner.CmdsMatch([][]string{{"cosign", "verify", "docker/image:latest"}}))
		})
		It("Unpacks a locally saved docker image file to target", Label("docker"), func() {
			// Clear any previous commands
			runner.ClearCmds()

			ociTarPath := "/tmp/oci-image.tar"
			// Create a dummy OCI image tar file
			err := fs.WriteFile(ociTarPath, []byte("dummy oci image content"), 0644)
			Expect(err).To(BeNil())

			runner.SideEffect = func(cmd string, args ...string) ([]byte, error) {
				fullCmd := fmt.Sprintf("Running command: %s %s", cmd, strings.Join(args, " "))
				_, _ = GinkgoWriter.Write([]byte(fullCmd + "\n"))
				if cmd == "tar" && len(args) >= 2 && args[0] == "-xf" {
					_, _ = GinkgoWriter.Write([]byte("Simulating successful tar extraction\n"))
					return []byte{}, nil
				}
				return []byte{}, nil
			}
			_, err = e.DumpSource(destDir, sdkImages.NewOCIFileSrc(ociTarPath))
			Expect(err).To(BeNil())
			Expect(runner.IncludesCmds([][]string{{"tar", "-xf", ociTarPath}})).To(BeNil())
		})

		It("Copies image file to target", func() {
			sourceImg := "/source.img"
			destFile := filepath.Join(destDir, "active.img")
			_, err := fs.Create(sourceImg)
			Expect(err).To(BeNil())
			_, err = e.DumpSource(destFile, sdkImages.NewFileSrc(sourceImg))
			Expect(err).To(BeNil())
			Expect(runner.IncludesCmds([][]string{{cnst.Rsync}}))
		})
		It("Fails to copy, source file is not present", func() {
			_, err := e.DumpSource("whatever", sdkImages.NewFileSrc("/source.img"))
			Expect(err).NotTo(BeNil())
		})
	})
	Describe("CheckActiveDeployment", Label("check"), func() {
		It("deployment found", func() {
			ghwTest := ghwMock.GhwMock{}
			disk := SdkPartitions.Disk{Name: "device", Partitions: []*SdkPartitions.Partition{
				{
					Name:            "device1",
					FilesystemLabel: cnst.ActiveLabel,
				},
			}}
			ghwTest.AddDisk(disk)
			ghwTest.CreateDevices()

			runner.ReturnValue = []byte(
				fmt.Sprintf(
					`{"blockdevices": [{"label": "%s", "type": "loop", "path": "/some/device"}]}`,
					cnst.ActiveLabel,
				),
			)
			e := elemental.NewElemental(config)
			Expect(e.CheckActiveDeployment([]string{cnst.ActiveLabel, cnst.PassiveLabel})).To(BeTrue())

			ghwTest.Clean()
		})

		It("Should not error out", func() {
			runner.ReturnValue = []byte("")
			e := elemental.NewElemental(config)
			Expect(e.CheckActiveDeployment([]string{cnst.ActiveLabel, cnst.PassiveLabel})).To(BeFalse())
		})
	})
	Describe("SelinuxRelabel", Label("SelinuxRelabel", "selinux"), func() {
		var policyFile string
		var relabelCmd []string
		BeforeEach(func() {
			// to mock the existance of setfiles command on non selinux hosts
			err := fsutils.MkdirAll(fs, "/usr/sbin", cnst.DirPerm)
			Expect(err).ShouldNot(HaveOccurred())
			sbin, err := fs.RawPath("/usr/sbin")
			Expect(err).ShouldNot(HaveOccurred())

			path := os.Getenv("PATH")
			os.Setenv("PATH", fmt.Sprintf("%s:%s", sbin, path))
			_, err = fs.Create("/usr/sbin/setfiles")
			Expect(err).ShouldNot(HaveOccurred())
			err = fs.Chmod("/usr/sbin/setfiles", 0777)
			Expect(err).ShouldNot(HaveOccurred())

			// to mock SELinux policy files
			policyFile = filepath.Join(cnst.SELinuxTargetedPolicyPath, "policy.31")
			err = fsutils.MkdirAll(fs, filepath.Dir(cnst.SELinuxTargetedContextFile), cnst.DirPerm)
			Expect(err).ShouldNot(HaveOccurred())
			_, err = fs.Create(cnst.SELinuxTargetedContextFile)
			Expect(err).ShouldNot(HaveOccurred())
			err = fsutils.MkdirAll(fs, cnst.SELinuxTargetedPolicyPath, cnst.DirPerm)
			Expect(err).ShouldNot(HaveOccurred())
			_, err = fs.Create(policyFile)
			Expect(err).ShouldNot(HaveOccurred())

			relabelCmd = []string{
				"setfiles", "-c", policyFile, "-e", "/dev", "-e", "/proc", "-e", "/sys",
				"-F", cnst.SELinuxTargetedContextFile, "/",
			}
		})
		It("does nothing if the context file is not found", func() {
			err := fs.Remove(cnst.SELinuxTargetedContextFile)
			Expect(err).ShouldNot(HaveOccurred())

			c := elemental.NewElemental(config)
			Expect(c.SelinuxRelabel("/", true)).To(BeNil())
			Expect(runner.CmdsMatch([][]string{{}}))
		})
		It("does nothing if the policy file is not found", func() {
			err := fs.Remove(policyFile)
			Expect(err).ShouldNot(HaveOccurred())

			c := elemental.NewElemental(config)
			Expect(c.SelinuxRelabel("/", true)).To(BeNil())
			Expect(runner.CmdsMatch([][]string{{}}))
		})
		It("relabels the current root", func() {
			c := elemental.NewElemental(config)
			Expect(c.SelinuxRelabel("", true)).To(BeNil())
			Expect(runner.CmdsMatch([][]string{relabelCmd})).To(BeNil())

			runner.ClearCmds()
			Expect(c.SelinuxRelabel("/", true)).To(BeNil())
			Expect(runner.CmdsMatch([][]string{relabelCmd})).To(BeNil())
		})
		It("fails to relabel the current root", func() {
			runner.ReturnError = errors.New("setfiles failure")
			c := elemental.NewElemental(config)
			Expect(c.SelinuxRelabel("", true)).NotTo(BeNil())
			Expect(runner.CmdsMatch([][]string{relabelCmd})).To(BeNil())
		})
		It("ignores relabel failures", func() {
			runner.ReturnError = errors.New("setfiles failure")
			c := elemental.NewElemental(config)
			Expect(c.SelinuxRelabel("", false)).To(BeNil())
			Expect(runner.CmdsMatch([][]string{relabelCmd})).To(BeNil())
		})
		It("relabels the given root-tree path", func() {
			contextFile := filepath.Join("/root", cnst.SELinuxTargetedContextFile)
			err := fsutils.MkdirAll(fs, filepath.Dir(contextFile), cnst.DirPerm)
			Expect(err).ShouldNot(HaveOccurred())
			_, err = fs.Create(contextFile)
			Expect(err).ShouldNot(HaveOccurred())
			policyFile = filepath.Join("/root", policyFile)
			err = fsutils.MkdirAll(fs, filepath.Join("/root", cnst.SELinuxTargetedPolicyPath), cnst.DirPerm)
			Expect(err).ShouldNot(HaveOccurred())
			_, err = fs.Create(policyFile)
			Expect(err).ShouldNot(HaveOccurred())

			relabelCmd = []string{
				"setfiles", "-c", policyFile, "-F", "-r", "/root", contextFile, "/root",
			}

			c := elemental.NewElemental(config)
			Expect(c.SelinuxRelabel("/root", true)).To(BeNil())
			Expect(runner.CmdsMatch([][]string{relabelCmd})).To(BeNil())
		})
	})
	Describe("GetIso", Label("GetIso", "iso"), func() {
		var e *elemental.Elemental
		BeforeEach(func() {
			e = elemental.NewElemental(config)
		})
		It("Gets the iso and returns the temporary where it is stored", func() {
			tmpDir, err := fsutils.TempDir(fs, "", "elemental-test")
			Expect(err).To(BeNil())
			err = fs.WriteFile(fmt.Sprintf("%s/fake.iso", tmpDir), []byte("Hi"), cnst.FilePerm)
			Expect(err).To(BeNil())
			iso := fmt.Sprintf("%s/fake.iso", tmpDir)
			isoDir, err := e.GetIso(iso)
			Expect(err).To(BeNil())
			// Confirm that the iso is stored in isoDir
			fsutils.Exists(fs, filepath.Join(isoDir, "cOs.iso"))
		})
		It("Fails if it cant find the iso", func() {
			iso := "http://whatever"
			cl.Error = true
			e := elemental.NewElemental(config)
			_, err := e.GetIso(iso)
			Expect(err).ToNot(BeNil())
		})
		It("Fails if it cannot mount the iso", func() {
			mounter.ErrorOnMount = true
			tmpDir, err := fsutils.TempDir(fs, "", "elemental-test")
			Expect(err).To(BeNil())
			err = fs.WriteFile(fmt.Sprintf("%s/fake.iso", tmpDir), []byte("Hi"), cnst.FilePerm)
			Expect(err).To(BeNil())
			iso := fmt.Sprintf("%s/fake.iso", tmpDir)
			_, err = e.GetIso(iso)
			Expect(err).ToNot(BeNil())
			Expect(err.Error()).To(ContainSubstring("mount error"))
		})
	})
	Describe("UpdateSourcesFormDownloadedISO", Label("iso"), func() {
		var e *elemental.Elemental
		var activeImg, recoveryImg *sdkImages.Image
		BeforeEach(func() {
			activeImg, recoveryImg = nil, nil
			e = elemental.NewElemental(config)
		})
		It("updates active image", func() {
			activeImg = &sdkImages.Image{}
			err := e.UpdateSourcesFormDownloadedISO("/some/dir", activeImg, recoveryImg)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(activeImg.Source.IsDir()).To(BeTrue())
			Expect(activeImg.Source.Value()).To(Equal("/some/dir/rootfs"))
			Expect(recoveryImg).To(BeNil())
		})
		It("updates active and recovery image", func() {
			activeImg = &sdkImages.Image{File: "activeFile"}
			recoveryImg = &sdkImages.Image{}
			err := e.UpdateSourcesFormDownloadedISO("/some/dir", activeImg, recoveryImg)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(recoveryImg.Source.IsFile()).To(BeTrue())
			Expect(recoveryImg.Source.Value()).To(Equal("activeFile"))
			Expect(recoveryImg.Label).To(Equal(cnst.SystemLabel))
			Expect(activeImg.Source.IsDir()).To(BeTrue())
			Expect(activeImg.Source.Value()).To(Equal("/some/dir/rootfs"))
		})
		It("updates recovery only image", func() {
			recoveryImg = &sdkImages.Image{}
			isoMnt := "/some/dir/iso"
			err := fsutils.MkdirAll(fs, isoMnt, cnst.DirPerm)
			Expect(err).ShouldNot(HaveOccurred())
			recoverySquash := filepath.Join(isoMnt, cnst.RecoverySquashFile)
			_, err = fs.Create(recoverySquash)
			Expect(err).ShouldNot(HaveOccurred())
			err = e.UpdateSourcesFormDownloadedISO("/some/dir", activeImg, recoveryImg)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(recoveryImg.Source.IsFile()).To(BeTrue())
			Expect(recoveryImg.Source.Value()).To(Equal(recoverySquash))
			Expect(activeImg).To(BeNil())
		})
		It("fails to update recovery from active file", func() {
			recoveryImg = &sdkImages.Image{}
			err := e.UpdateSourcesFormDownloadedISO("/some/dir", activeImg, recoveryImg)
			Expect(err).Should(HaveOccurred())
		})
	})
	Describe("CloudConfig", Label("CloudConfig", "cloud-config"), func() {
		var e *elemental.Elemental
		BeforeEach(func() {
			e = elemental.NewElemental(config)
		})
		It("Copies the cloud config file", func() {
			testString := "In a galaxy far far away..."
			cloudInit := []string{"/config.yaml"}
			err := fs.WriteFile(cloudInit[0], []byte(testString), cnst.FilePerm)
			Expect(err).To(BeNil())
			Expect(err).To(BeNil())

			err = e.CopyCloudConfig(cloudInit)
			Expect(err).To(BeNil())
			configFilePath := fmt.Sprintf("%s/90_custom.yaml", cnst.OEMDir)
			copiedFile, err := fs.ReadFile(configFilePath)
			Expect(err).To(BeNil())
			Expect(copiedFile).To(ContainSubstring(testString))
			stat, err := fs.Stat(configFilePath)
			Expect(err).To(BeNil())
			Expect(int(stat.Mode().Perm())).To(Equal(cnst.ConfigPerm))

		})
		It("Doesnt do anything if the config file is not set", func() {
			err := e.CopyCloudConfig([]string{})
			Expect(err).To(BeNil())
		})
	})
	Describe("SetDefaultGrubEntry", Label("SetDefaultGrubEntry", "grub"), func() {
		It("Sets the default grub entry without issues", func() {
			el := elemental.NewElemental(config)
			Expect(config.Fs.Mkdir("/tmp", cnst.DirPerm)).To(BeNil())
			Expect(el.SetDefaultGrubEntry("/tmp", "/imgMountpoint", "dio")).To(BeNil())
			varsParsed, err := utils.ReadPersistentVariables(filepath.Join("/tmp", cnst.GrubOEMEnv), config)
			Expect(err).To(BeNil())
			Expect(varsParsed["default_menu_entry"]).To(Equal("dio"))
		})
		It("does nothing on empty default entry and no /etc/kairos-release", func() {
			el := elemental.NewElemental(config)
			Expect(config.Fs.Mkdir("/mountpoint", cnst.DirPerm)).To(BeNil())
			Expect(el.SetDefaultGrubEntry("/mountpoint", "/imgMountPoint", "")).To(BeNil())
			_, err := utils.ReadPersistentVariables(filepath.Join("/tmp", cnst.GrubEnv), config)
			// Because it didnt do anything due to the entry being empty, the file should not be there
			Expect(err).ToNot(BeNil())
			_, err = config.Fs.Stat(filepath.Join("/tmp", cnst.GrubOEMEnv))
			Expect(err).ToNot(BeNil())
		})
		It("loads /etc/kairos-release on empty default entry", func() {
			err := fsutils.MkdirAll(config.Fs, "/imgMountPoint/etc", cnst.DirPerm)
			Expect(err).ShouldNot(HaveOccurred())
			err = config.Fs.WriteFile("/imgMountPoint/etc/kairos-release", []byte("GRUB_ENTRY_NAME=test"), cnst.FilePerm)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(config.Fs.Mkdir("/mountpoint", cnst.DirPerm)).To(BeNil())

			el := elemental.NewElemental(config)
			Expect(el.SetDefaultGrubEntry("/mountpoint", "/imgMountPoint", "")).To(BeNil())
			varsParsed, err := utils.ReadPersistentVariables(filepath.Join("/mountpoint", cnst.GrubOEMEnv), config)
			Expect(err).To(BeNil())
			Expect(varsParsed["default_menu_entry"]).To(Equal("test"))

		})
		It("Fails setting grubenv", func() {
			el := elemental.NewElemental(config)
			Expect(el.SetDefaultGrubEntry("nonexisting", "nonexisting", "default_entry")).NotTo(BeNil())
		})
	})
	Describe("FindKernelInitrd", Label("find"), func() {
		BeforeEach(func() {
			err := fsutils.MkdirAll(fs, "/path/boot", cnst.DirPerm)
			Expect(err).ShouldNot(HaveOccurred())
		})
		It("finds kernel and initrd files", func() {
			_, err := fs.Create("/path/boot/initrd")
			Expect(err).ShouldNot(HaveOccurred())

			_, err = fs.Create("/path/boot/vmlinuz")
			Expect(err).ShouldNot(HaveOccurred())

			el := elemental.NewElemental(config)
			k, i, err := el.FindKernelInitrd("/path")
			Expect(err).ShouldNot(HaveOccurred())
			Expect(k).To(Equal("/path/boot/vmlinuz"))
			Expect(i).To(Equal("/path/boot/initrd"))
		})
		It("fails if no initrd is found", func() {
			_, err := fs.Create("/path/boot/vmlinuz")
			Expect(err).ShouldNot(HaveOccurred())

			el := elemental.NewElemental(config)
			_, _, err = el.FindKernelInitrd("/path")
			Expect(err).Should(HaveOccurred())
		})
		It("fails if no kernel is found", func() {
			_, err := fs.Create("/path/boot/initrd")
			Expect(err).ShouldNot(HaveOccurred())

			el := elemental.NewElemental(config)
			_, _, err = el.FindKernelInitrd("/path")
			Expect(err).Should(HaveOccurred())
		})
	})
	Describe("DeactivateDevices", Label("blkdeactivate"), func() {
		It("calls blkdeactivat", func() {
			el := elemental.NewElemental(config)
			err := el.DeactivateDevices()
			Expect(err).ShouldNot(HaveOccurred())
			Expect(runner.CmdsMatch([][]string{{
				"blkdeactivate", "--lvmoptions", "retry,wholevg",
				"--dmoptions", "force,retry", "--errors",
			}})).To(BeNil())
		})
	})
})
