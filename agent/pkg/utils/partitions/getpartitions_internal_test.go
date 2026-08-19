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

package partitions

import (
	"bytes"
	"os"

	"github.com/kairos-io/kairos-agent/v2/pkg/constants"
	ghwMock "github.com/kairos-io/kairos-sdk/ghw/mocks"
	sdkFS "github.com/kairos-io/kairos-sdk/types/fs"
	"github.com/kairos-io/kairos-sdk/types/logger"
	sdkPartitions "github.com/kairos-io/kairos-sdk/types/partitions"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/twpayne/go-vfs/v5/vfst"
)

var _ = Describe("parseMountEntry", func() {
	DescribeTable("parses /proc/mounts style entries",
		func(line, expectedDev, expectedMount string) {
			dev, mount := parseMountEntry(line)
			Expect(dev).To(Equal(expectedDev))
			Expect(mount).To(Equal(expectedMount))
		},
		// Happy path: a typical root mount entry.
		Entry("simple root entry",
			"/dev/sda6 / ext4 rw,relatime,errors=remount-ro,data=ordered 0 0",
			"/dev/sda6", "/"),
		// Happy path: a by-label device path (the case GetMountPointByLabel cares about).
		Entry("by-label device",
			"/dev/disk/by-label/COS_STATE /run/cos/state ext4 rw,relatime 0 0",
			"/dev/disk/by-label/COS_STATE", "/run/cos/state"),
		// Octal-escaped space (\040) in the mountpoint is decoded to a real space.
		Entry("octal escaped space in mountpoint",
			`/dev/sdb1 /mnt/My\040Disk vfat rw 0 0`,
			"/dev/sdb1", "/mnt/My Disk"),
		// Octal-escaped tab (\011) is decoded.
		Entry("octal escaped tab in mountpoint",
			`/dev/sdb1 /mnt/tab\011here vfat rw 0 0`,
			"/dev/sdb1", "/mnt/tab\there"),
		// Octal-escaped newline (\012) is decoded.
		Entry("octal escaped newline in mountpoint",
			`/dev/sdb1 /mnt/new\012line vfat rw 0 0`,
			"/dev/sdb1", "/mnt/new\nline"),
		// Escaped backslash (\\) is decoded to a single backslash.
		Entry("escaped backslash in mountpoint",
			`/dev/sdb1 /mnt/back\\slash vfat rw 0 0`,
			"/dev/sdb1", `/mnt/back\slash`),
		// Multiple escapes combined in a single mountpoint.
		Entry("multiple escapes combined",
			`/dev/sdb1 /mnt/a\040b\011c vfat rw 0 0`,
			"/dev/sdb1", "/mnt/a b\tc"),
		// Extra whitespace between fields is collapsed by strings.Fields.
		Entry("extra whitespace between fields",
			"/dev/sda1    /boot     ext4   rw 0 0",
			"/dev/sda1", "/boot"),
		// Exactly 4 fields is the minimum accepted (dev mount fstype opts).
		Entry("exactly four fields",
			"/dev/sda2 /home xfs rw",
			"/dev/sda2", "/home"),
		// Lines not starting with '/' (e.g. pseudo filesystems) are ignored.
		Entry("non-slash first char (pseudo fs)",
			"proc /proc proc rw,nosuid 0 0",
			"", ""),
		Entry("tmpfs entry ignored",
			"tmpfs /run tmpfs rw,nosuid 0 0",
			"", ""),
		// Comment-like line that does not start with '/' is ignored.
		Entry("comment line ignored",
			"# this is a comment",
			"", ""),
		// Too few fields (only 3) returns empty even though it starts with '/'.
		Entry("too few fields (three)",
			"/dev/sda1 /boot ext4",
			"", ""),
		Entry("too few fields (two)",
			"/dev/sda1 /boot",
			"", ""),
		Entry("too few fields (one)",
			"/onlyonefield",
			"", ""),
	)

	It("returns empty strings for an empty line", func() {
		dev, mp := parseMountEntry("")
		Expect(dev).To(Equal(""))
		Expect(mp).To(Equal(""))
	})
})

var _ = Describe("GetMountPointByLabel", func() {
	It("returns empty string for a label that is not mounted", func() {
		// Reads the real /proc/mounts; a clearly bogus label will never match,
		// so this exercises the not-found path safely.
		Expect(GetMountPointByLabel("THIS_LABEL_DOES_NOT_EXIST_KAIROS_TEST")).To(Equal(""))
	})
})

var _ = Describe("GetPartitionViaDM", func() {
	var fs sdkFS.KairosFS
	var cleanup func()

	BeforeEach(func() {
		var err error
		fs, cleanup, err = vfst.NewTestFS(nil)
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		cleanup()
	})

	// NOTE: GetPartitionViaDM builds its sysfs/udev lookup paths via
	// ghw.NewPaths(fs.RawPath("/")). With vfst, RawPath("/") returns the test
	// FS's real temp-dir root, and ghw prefixes /sys/block and /run/udev/data
	// with it. vfst then re-prefixes that already-absolute path with its own
	// root again, so the resulting doubled path never resolves inside the test
	// FS. This makes the "device found" branches unreachable with a vfst-backed
	// FS, so we can only meaningfully assert the not-found path here.
	It("returns nil when sysfs contains no resolvable dm- devices", func() {
		// /sys/block is empty/absent under the doubled prefix, so the loop finds
		// nothing matching the dm- prefix and returns a nil partition.
		Expect(GetPartitionViaDM(fs, "COS_STATE")).To(BeNil())
	})
})

// These specs work around the doubled-prefix problem described above by
// setting GHW_CHROOT to the empty string: ghw.NewPaths gives the env var
// precedence over the rootPath argument, so the lookup paths stay as plain
// /sys/block and /run/udev/data, which resolve correctly inside the vfst FS.
var _ = Describe("GetPartitionViaDM with a populated sysfs", func() {
	var fs sdkFS.KairosFS
	var cleanup func()

	// Udev database content for the dm device with a DM_NAME (unlocked LUKS/LVM).
	dmUdevData := "E:ID_FS_LABEL=COS_OEM\nE:ID_FS_TYPE=ext4\nE:DM_LV_NAME=oem\nE:DM_NAME=vg-oem\nX:ignored\nE:NOEQUALSIGN\n"

	newFS := func(files map[string]interface{}) {
		var err error
		fs, cleanup, err = vfst.NewTestFS(files)
		Expect(err).ToNot(HaveOccurred())
	}

	BeforeEach(func() {
		Expect(os.Setenv("GHW_CHROOT", "")).To(Succeed())
	})

	AfterEach(func() {
		Expect(os.Unsetenv("GHW_CHROOT")).To(Succeed())
		if cleanup != nil {
			cleanup()
		}
	})

	It("returns a fully populated partition for a matching dm device", func() {
		newFS(map[string]interface{}{
			// The dm device we are looking for.
			"/sys/block/dm-0/dev":                      "253:0\n",
			"/sys/block/dm-0/size":                     "8192\n",
			"/sys/block/dm-0/queue/logical_block_size": "512\n",
			"/sys/block/dm-0/slaves/sda3/dev":          "8:3\n",
			"/run/udev/data/b253:0":                    dmUdevData,
			// Parent disk udev data. ID_PATH set to ../../.. so that
			// /dev/disk/by-path/../../.. cleans to / which always resolves.
			"/run/udev/data/b8:0": "E:ID_PATH=../../..\n",
			"/proc/mounts":        "/dev/mapper/vg-oem /run/cos/oem ext4 rw 0 0\n",
		})

		part := GetPartitionViaDM(fs, "COS_OEM")
		Expect(part).ToNot(BeNil())
		Expect(part.Name).To(Equal("oem"))
		Expect(part.FilesystemLabel).To(Equal("COS_OEM"))
		Expect(part.FS).To(Equal("ext4"))
		// DM_NAME is set, so the mapper path is preferred over by-label.
		Expect(part.Path).To(Equal("/dev/mapper/vg-oem"))
		Expect(part.Size).To(Equal(uint(8192 * 512)))
		Expect(part.Disk).To(Equal("/"))
		Expect(part.MountPoint).To(Equal("/run/cos/oem"))
	})

	It("falls back to the by-label path when DM_NAME is not set", func() {
		newFS(map[string]interface{}{
			"/sys/block/dm-0/dev":                      "253:0\n",
			"/sys/block/dm-0/size":                     "8192\n",
			"/sys/block/dm-0/queue/logical_block_size": "512\n",
			"/sys/block/dm-0/slaves/sda3/dev":          "8:3\n",
			"/run/udev/data/b253:0":                    "E:ID_FS_LABEL=COS_OEM\nE:ID_FS_TYPE=ext4\nE:DM_LV_NAME=oem\n",
			"/run/udev/data/b8:0":                      "E:ID_PATH=../../..\n",
			"/proc/mounts":                             "/dev/disk/by-label/COS_OEM /run/cos/oem ext4 rw 0 0\nshortline\n",
		})

		part := GetPartitionViaDM(fs, "COS_OEM")
		Expect(part).ToNot(BeNil())
		Expect(part.Path).To(Equal("/dev/disk/by-label/COS_OEM"))
		Expect(part.MountPoint).To(Equal("/run/cos/oem"))
	})

	It("leaves the size unset when sysfs size files are missing", func() {
		newFS(map[string]interface{}{
			"/sys/block/dm-0/dev":   "253:0\n",
			"/run/udev/data/b253:0": dmUdevData,
		})

		part := GetPartitionViaDM(fs, "COS_OEM")
		Expect(part).ToNot(BeNil())
		Expect(part.Size).To(Equal(uint(0)))
		// No slaves dir either, so the parent disk is unknown and no
		// mountpoint lookup happens.
		Expect(part.Disk).To(Equal(""))
		Expect(part.MountPoint).To(Equal(""))
	})

	It("leaves the size unset when sysfs size files contain garbage", func() {
		newFS(map[string]interface{}{
			"/sys/block/dm-0/dev":                      "253:0\n",
			"/sys/block/dm-0/size":                     "notanumber\n",
			"/sys/block/dm-0/queue/logical_block_size": "512\n",
			"/run/udev/data/b253:0":                    dmUdevData,
		})

		part := GetPartitionViaDM(fs, "COS_OEM")
		Expect(part).ToNot(BeNil())
		Expect(part.Size).To(Equal(uint(0)))
	})

	It("skips the parent disk lookup when there is more than one slave", func() {
		newFS(map[string]interface{}{
			"/sys/block/dm-0/dev":             "253:0\n",
			"/sys/block/dm-0/slaves/sda3/dev": "8:3\n",
			"/sys/block/dm-0/slaves/sdb3/dev": "8:19\n",
			"/run/udev/data/b253:0":           dmUdevData,
		})

		part := GetPartitionViaDM(fs, "COS_OEM")
		Expect(part).ToNot(BeNil())
		Expect(part.Disk).To(Equal(""))
	})

	It("leaves the disk unset when the parent disk path does not resolve", func() {
		newFS(map[string]interface{}{
			"/sys/block/dm-0/dev":             "253:0\n",
			"/sys/block/dm-0/slaves/sda3/dev": "8:3\n",
			"/run/udev/data/b253:0":           dmUdevData,
			"/run/udev/data/b8:0":             "E:ID_PATH=nonexistent-path-kairos-test\n",
		})

		part := GetPartitionViaDM(fs, "COS_OEM")
		Expect(part).ToNot(BeNil())
		Expect(part.Disk).To(Equal(""))
		Expect(part.MountPoint).To(Equal(""))
	})

	It("leaves the mountpoint unset when the partition is not in /proc/mounts", func() {
		newFS(map[string]interface{}{
			"/sys/block/dm-0/dev":             "253:0\n",
			"/sys/block/dm-0/slaves/sda3/dev": "8:3\n",
			"/run/udev/data/b253:0":           dmUdevData,
			"/run/udev/data/b8:0":             "E:ID_PATH=../../..\n",
			"/proc/mounts":                    "/dev/sda1 /boot ext4 rw 0 0\n",
		})

		part := GetPartitionViaDM(fs, "COS_OEM")
		Expect(part).ToNot(BeNil())
		Expect(part.Disk).To(Equal("/"))
		Expect(part.MountPoint).To(Equal(""))
	})

	It("returns nil when no dm device matches the label", func() {
		newFS(map[string]interface{}{
			// dm device with a non-matching label.
			"/sys/block/dm-0/dev":   "253:0\n",
			"/run/udev/data/b253:0": "E:ID_FS_LABEL=SOMETHING_ELSE\n",
			// dm device with an empty dev file, skipped.
			"/sys/block/dm-1/dev": "",
			// Non-dm device, skipped by the dm- prefix filter.
			"/sys/block/sda/dev": "8:0\n",
		})

		Expect(GetPartitionViaDM(fs, "COS_OEM")).To(BeNil())
	})
})

var _ = Describe("GetAllPartitions", func() {
	var ghwTest ghwMock.GhwMock
	var log logger.KairosLogger

	BeforeEach(func() {
		log = logger.NewBufferLogger(&bytes.Buffer{})
		ghwTest = ghwMock.GhwMock{}
	})

	AfterEach(func() {
		ghwTest.Clean()
	})

	It("returns the partitions of all disks, skipping LUKS ones", func() {
		ghwTest.AddDisk(sdkPartitions.Disk{Name: "sda", Partitions: []*sdkPartitions.Partition{
			{Name: "sda1", FilesystemLabel: "COS_STATE", FS: "ext4"},
			{Name: "sda2", FilesystemLabel: "COS_PERSISTENT", FS: "crypto_LUKS"},
		}})
		ghwTest.AddDisk(sdkPartitions.Disk{Name: "sdb", Partitions: []*sdkPartitions.Partition{
			{Name: "sdb1", FilesystemLabel: "COS_RECOVERY", FS: "ext4"},
		}})
		ghwTest.CreateDevices()

		parts, err := GetAllPartitions(&log)
		Expect(err).ToNot(HaveOccurred())

		var names []string
		for _, p := range parts {
			names = append(names, p.Name)
		}
		Expect(names).To(ContainElement("sda1"))
		Expect(names).To(ContainElement("sdb1"))
		// LUKS partitions are skipped, GetPartitionViaDM handles them.
		Expect(names).ToNot(ContainElement("sda2"))
	})

	It("returns no partitions when there are no disks", func() {
		ghwTest.CreateDevices()

		parts, err := GetAllPartitions(&log)
		Expect(err).ToNot(HaveOccurred())
		Expect(parts).To(BeEmpty())
	})
})

var _ = Describe("GetEfiPartition", func() {
	var ghwTest ghwMock.GhwMock
	var log logger.KairosLogger

	BeforeEach(func() {
		log = logger.NewBufferLogger(&bytes.Buffer{})
		ghwTest = ghwMock.GhwMock{}
	})

	AfterEach(func() {
		ghwTest.Clean()
	})

	It("returns the partition labeled COS_GRUB", func() {
		ghwTest.AddDisk(sdkPartitions.Disk{Name: "sda", Partitions: []*sdkPartitions.Partition{
			{Name: "sda1", FilesystemLabel: constants.EfiLabel, FS: "vfat"},
			{Name: "sda2", FilesystemLabel: "COS_STATE", FS: "ext4"},
		}})
		ghwTest.CreateDevices()

		part, err := GetEfiPartition(&log)
		Expect(err).ToNot(HaveOccurred())
		Expect(part).ToNot(BeNil())
		Expect(part.Name).To(Equal("sda1"))
		Expect(part.FilesystemLabel).To(Equal(constants.EfiLabel))
	})

	It("errors out when there is no EFI partition", func() {
		ghwTest.AddDisk(sdkPartitions.Disk{Name: "sda", Partitions: []*sdkPartitions.Partition{
			{Name: "sda1", FilesystemLabel: "COS_STATE", FS: "ext4"},
		}})
		ghwTest.CreateDevices()

		part, err := GetEfiPartition(&log)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("could not find EFI partition"))
		Expect(part).To(BeNil())
	})
})
