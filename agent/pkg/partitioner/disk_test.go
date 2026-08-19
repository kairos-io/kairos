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

package partitioner

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/diskfs/go-diskfs"
	"github.com/diskfs/go-diskfs/partition/gpt"
	"github.com/gofrs/uuid"
	sdkConstants "github.com/kairos-io/kairos-sdk/constants"
	"github.com/kairos-io/kairos-sdk/types/logger"
	"github.com/kairos-io/kairos-sdk/types/partitions"
	"github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	cnst "github.com/kairos-io/kairos-agent/v2/pkg/constants"
)

const (
	mib        = 1024 * 1024
	sectorSize = 512
)

// createDiskImage creates a sparse file of the given size to act as a fake disk.
func createDiskImage(sizeMiB int64) string {
	dir := ginkgo.GinkgoT().TempDir()
	img := filepath.Join(dir, "disk.img")
	f, err := os.Create(img)
	Expect(err).ToNot(HaveOccurred())
	Expect(f.Truncate(sizeMiB * mib)).To(Succeed())
	Expect(f.Close()).To(Succeed())
	return img
}

var _ = ginkgo.Describe("Disk", ginkgo.Label("disk"), func() {
	var log logger.KairosLogger

	ginkgo.BeforeEach(func() {
		log = logger.NewNullLogger()
	})

	ginkgo.Describe("NewDisk", func() {
		ginkgo.It("initializes a disk from a device image", func() {
			img := createDiskImage(10)
			d, err := NewDisk(img)
			Expect(err).ToNot(HaveOccurred())
			Expect(d).ToNot(BeNil())
			Expect(d.Size).To(Equal(int64(10 * mib)))
			Expect(d.LogicalBlocksize).To(Equal(int64(sectorSize)))
		})

		ginkgo.It("applies the WithLogger option", func() {
			img := createDiskImage(10)
			d, err := NewDisk(img, WithLogger(log))
			Expect(err).ToNot(HaveOccurred())
			Expect(d).ToNot(BeNil())
			Expect(d.logger).To(Equal(log))
		})

		ginkgo.It("fails when the device does not exist", func() {
			d, err := NewDisk("/some/nonexisting/device")
			Expect(err).To(HaveOccurred())
			Expect(d).To(BeNil())
		})

		ginkgo.It("fails when an option returns an error", func() {
			img := createDiskImage(10)
			failingOpt := func(d *Disk) error { return errors.New("option failed") }
			d, err := NewDisk(img, failingOpt)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("option failed"))
			Expect(d).To(BeNil())
		})
	})

	ginkgo.Describe("NewPartitionTable", func() {
		ginkgo.It("fails with an invalid partition table type", func() {
			img := createDiskImage(10)
			d, err := NewDisk(img, WithLogger(log))
			Expect(err).ToNot(HaveOccurred())
			err = d.NewPartitionTable("msdos", partitions.PartitionList{})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid partition type: msdos"))
		})

		ginkgo.It("creates a GPT partition table with the expected partitions", func() {
			img := createDiskImage(100)
			d, err := NewDisk(img, WithLogger(log))
			Expect(err).ToNot(HaveOccurred())

			parts := partitions.PartitionList{
				{Name: sdkConstants.EfiPartName, FilesystemLabel: "COS_GRUB", Size: 20, FS: sdkConstants.EfiFs},
				{Name: sdkConstants.BiosPartName, FilesystemLabel: "bios", Size: 2, FS: ""},
				{Name: "state", FilesystemLabel: "COS_STATE", Size: 0, FS: "ext4"},
			}
			Expect(d.NewPartitionTable(sdkConstants.GPT, parts)).To(Succeed())

			table, err := d.GetPartitionTable()
			Expect(err).ToNot(HaveOccurred())
			gptTable, ok := table.(*gpt.Table)
			Expect(ok).To(BeTrue())
			// GUIDs are read back from disk in uppercase
			Expect(strings.ToLower(gptTable.GUID)).To(Equal(cnst.DiskUUID))

			tableParts := gptTable.Partitions
			Expect(len(tableParts)).To(BeNumerically(">=", 3))

			// EFI partition, 1MiB aligned start, system partition attribute
			Expect(tableParts[0].Name).To(Equal(sdkConstants.EfiPartName))
			Expect(tableParts[0].Type).To(Equal(gpt.EFISystemPartition))
			Expect(tableParts[0].Attributes).To(Equal(uint64(0x1)))
			Expect(tableParts[0].Start).To(Equal(uint64(mib / sectorSize)))
			Expect(tableParts[0].End).To(Equal(uint64(20*mib/sectorSize + mib/sectorSize - 1)))
			Expect(strings.ToLower(tableParts[0].GUID)).To(Equal(uuid.NewV5(uuid.NamespaceURL, "COS_GRUB").String()))

			// BIOS partition, starts right after EFI, legacy bios bootable attribute
			Expect(tableParts[1].Name).To(Equal(sdkConstants.BiosPartName))
			Expect(tableParts[1].Type).To(Equal(gpt.BIOSBoot))
			Expect(tableParts[1].Attributes).To(Equal(uint64(0x4)))
			Expect(tableParts[1].Start).To(Equal(tableParts[0].End + 1))
			Expect(tableParts[1].End).To(Equal(tableParts[1].Start + 2*mib/sectorSize - 1))

			// State partition fills the rest of the disk minus the exact backup GPT tail reservation.
			Expect(tableParts[2].Name).To(Equal("state"))
			Expect(tableParts[2].Type).To(Equal(gpt.LinuxFilesystem))
			Expect(tableParts[2].Start).To(Equal(tableParts[1].End + 1))
			expectedStateSize := uint64(100*mib) - uint64(23*mib) - gptBackupTailSectors(sectorSize)*uint64(sectorSize)
			Expect(tableParts[2].End).To(Equal(tableParts[2].Start + expectedStateSize/sectorSize - 1))
			Expect(strings.ToLower(tableParts[2].GUID)).To(Equal(uuid.NewV5(uuid.NamespaceURL, "COS_STATE").String()))
		})

		ginkgo.It("fails to write the partition table on a read-only device", func() {
			img := createDiskImage(100)
			roDisk, err := diskfs.Open(img, diskfs.WithOpenMode(diskfs.ReadOnly))
			Expect(err).ToNot(HaveOccurred())
			d := &Disk{roDisk, log}

			parts := partitions.PartitionList{
				{Name: "oem", FilesystemLabel: "COS_OEM", Size: 10, FS: "ext4"},
			}
			err = d.NewPartitionTable(sdkConstants.GPT, parts)
			Expect(err).To(HaveOccurred())
		})
	})

	ginkgo.Describe("getSectorEndFromSize", func() {
		ginkgo.It("computes the end sector from start and size", func() {
			// 1MiB at sector 2048 with 512 sector size occupies 2048 sectors
			Expect(getSectorEndFromSize(2048, mib, sectorSize)).To(Equal(uint64(4095)))
			Expect(getSectorEndFromSize(0, mib, sectorSize)).To(Equal(uint64(2047)))
			Expect(getSectorEndFromSize(2048, 20*mib, sectorSize)).To(Equal(uint64(43007)))
		})
	})

	ginkgo.Describe("kairosPartsToDiskfsGPTParts", func() {
		ginkgo.It("returns an empty list for no partitions", func() {
			Expect(kairosPartsToDiskfsGPTParts(partitions.PartitionList{}, 100*mib, sectorSize)).To(BeEmpty())
		})

		ginkgo.It("reserves the backup GPT tail when the last partition has an explicit size", func() {
			parts := partitions.PartitionList{
				{Name: "oem", FilesystemLabel: "COS_OEM", Size: 10, FS: "ext4"},
			}
			gptParts := kairosPartsToDiskfsGPTParts(parts, 100*mib, sectorSize)
			Expect(gptParts).To(HaveLen(1))
			Expect(gptParts[0].Start).To(Equal(uint64(2048)))
			expectedSize := uint64(10*mib) - gptBackupTailSectors(sectorSize)*uint64(sectorSize)
			Expect(gptParts[0].Size).To(Equal(expectedSize))
			Expect(gptParts[0].End).To(Equal(uint64(2048 + expectedSize/sectorSize - 1)))
			Expect(gptParts[0].Type).To(Equal(gpt.LinuxFilesystem))
			Expect(gptParts[0].Index).To(Equal(1))
			Expect(gptParts[0].Attributes).To(BeZero())
		})

		ginkgo.It("expands a zero-sized partition to fill the remaining disk", func() {
			parts := partitions.PartitionList{
				{Name: "oem", FilesystemLabel: "COS_OEM", Size: 30, FS: "ext4"},
				{Name: "persistent", FilesystemLabel: "COS_PERSISTENT", Size: 0, FS: "ext4"},
			}
			gptParts := kairosPartsToDiskfsGPTParts(parts, 100*mib, sectorSize)
			Expect(gptParts).To(HaveLen(2))
			Expect(gptParts[0].Size).To(Equal(uint64(30 * mib)))
			expectedSize := uint64(100*mib) - uint64(mib) - uint64(30*mib) - gptBackupTailSectors(sectorSize)*uint64(sectorSize)
			Expect(gptParts[1].Size).To(Equal(expectedSize))
			Expect(gptParts[1].Start).To(Equal(gptParts[0].End + 1))
			Expect(gptParts[1].Index).To(Equal(2))
		})

		ginkgo.It("sets EFI specific type and attributes only for vfat efi partitions", func() {
			parts := partitions.PartitionList{
				{Name: sdkConstants.EfiPartName, FilesystemLabel: "COS_GRUB", Size: 20, FS: sdkConstants.EfiFs},
				{Name: sdkConstants.EfiPartName, FilesystemLabel: "COS_GRUB2", Size: 20, FS: "ext4"},
			}
			gptParts := kairosPartsToDiskfsGPTParts(parts, 100*mib, sectorSize)
			Expect(gptParts).To(HaveLen(2))
			Expect(gptParts[0].Type).To(Equal(gpt.EFISystemPartition))
			Expect(gptParts[0].Attributes).To(Equal(uint64(0x1)))
			// efi name without vfat filesystem is treated as a regular partition
			Expect(gptParts[1].Type).To(Equal(gpt.LinuxFilesystem))
			Expect(gptParts[1].Attributes).To(BeZero())
		})

		ginkgo.It("sets BIOS specific type and attributes for the bios partition", func() {
			parts := partitions.PartitionList{
				{Name: sdkConstants.BiosPartName, FilesystemLabel: "bios", Size: 1},
				{Name: "oem", FilesystemLabel: "COS_OEM", Size: 10, FS: "ext4"},
			}
			gptParts := kairosPartsToDiskfsGPTParts(parts, 100*mib, sectorSize)
			Expect(gptParts).To(HaveLen(2))
			Expect(gptParts[0].Type).To(Equal(gpt.BIOSBoot))
			Expect(gptParts[0].Attributes).To(Equal(uint64(0x4)))
			Expect(gptParts[0].Name).To(Equal(sdkConstants.BiosPartName))
		})

		ginkgo.It("assigns predictable UUIDs based on the filesystem label", func() {
			parts := partitions.PartitionList{
				{Name: "oem", FilesystemLabel: "COS_OEM", Size: 10, FS: "ext4"},
			}
			gptParts := kairosPartsToDiskfsGPTParts(parts, 100*mib, sectorSize)
			Expect(gptParts[0].GUID).To(Equal(uuid.NewV5(uuid.NamespaceURL, "COS_OEM").String()))
		})
	})
})

// diskSectorsFromBytes divides bytes by sector size, floor.
func diskSectorsFromBytes(b, sector int64) uint64 {
	return uint64(b / sector)
}

// TestBug4257LastPartitionFixedSize reproduces kairos-io/kairos#4257: when
// persistent.size is fixed and an extra partition is defined, the extra
// partition ends up as the LAST partition. The reported symptom is that the
// GPT is left inconsistent — the on-disk partition exceeds its configured
// size and the backup GPT overlaps the last partition by 33 blocks.
//
// This test focuses on what kairosPartsToDiskfsGPTParts returns, since that
// function is where partition Start/End/Size are computed for go-diskfs to
// write.
func TestBug4257LastPartitionFixedSize(t *testing.T) {
	const sector int64 = 512
	// Mirror the disk from the bug report: 3750748848 sectors of 512 B.
	const diskSize int64 = 3750748848 * 512
	diskSectors := diskSectorsFromBytes(diskSize, sector)

	// Same order that ElementalPartitions.PartitionsByInstallOrder produces
	// when persistent has a fixed size and extras follow.
	parts := partitions.PartitionList{
		{Name: sdkConstants.EfiPartName, FS: sdkConstants.EfiFs, FilesystemLabel: sdkConstants.EfiLabel, Size: 64},
		{Name: sdkConstants.OEMPartName, FilesystemLabel: sdkConstants.OEMLabel, Size: 5000},
		{Name: sdkConstants.RecoveryPartName, FilesystemLabel: sdkConstants.RecoveryLabel, Size: 20200},
		{Name: sdkConstants.StatePartName, FilesystemLabel: sdkConstants.StateLabel, Size: 25576},
		{Name: sdkConstants.PersistentPartName, FilesystemLabel: sdkConstants.PersistentLabel, Size: 300000},
		{Name: "data_partition", FilesystemLabel: "SYSTEM_DATA", FS: "ext4", Size: 300000},
	}

	got := kairosPartsToDiskfsGPTParts(parts, diskSize, sector)

	if len(got) != len(parts) {
		t.Fatalf("expected %d partitions, got %d", len(parts), len(got))
	}

	// go-diskfs derives lastDataSector from the disk size and the partition
	// array footprint (128 entries * 128 bytes / sectorSize = 32 sectors).
	// Every partition's End sector MUST be <= lastDataSector or the primary
	// GPT will overlap the backup partition array/header.
	const partitionArraySectors uint64 = 128 * 128 / 512
	secondaryHeader := diskSectors - 1
	lastDataSector := secondaryHeader - partitionArraySectors - 1

	// The extras partition should keep its configured size and stay within
	// the usable data area.
	last := got[len(got)-1]
	if last.Name != "data_partition" {
		t.Fatalf("expected last partition to be data_partition, got %q", last.Name)
	}

	wantMaxBytes := uint64(300000) * 1024 * 1024
	if last.Size > wantMaxBytes {
		t.Errorf("last partition size %d bytes exceeds configured 300000 MiB (%d bytes)",
			last.Size, wantMaxBytes)
	}
	if last.End > lastDataSector {
		t.Errorf("last partition End=%d exceeds lastDataSector=%d — GPT will be corrupted",
			last.End, lastDataSector)
	}
	if last.End >= diskSectors {
		t.Errorf("last partition End=%d is at/past disk end sector %d",
			last.End, diskSectors-1)
	}

	// Every partition should also be consistent: End-Start+1 sectors * sector
	// size must equal Size, or go-diskfs's toByteArray refuses to write.
	for i, p := range got {
		calculated := (p.End - p.Start + 1) * uint64(sector)
		if p.Size != calculated {
			t.Errorf("partition %d (%s): declared Size=%d but End-Start+1 implies %d",
				i, p.Name, p.Size, calculated)
		}
	}

	// Sanity: persistent (index 4) should be exactly 300000 MiB.
	persistent := got[4]
	if persistent.Name != sdkConstants.PersistentPartName {
		t.Fatalf("expected partition 4 to be persistent, got %q", persistent.Name)
	}
	if persistent.Size != uint64(300000)*1024*1024 {
		t.Errorf("persistent size = %d bytes, want %d",
			persistent.Size, uint64(300000)*1024*1024)
	}
}

// TestExpandingPersistentWorks confirms the "working" scenario from the bug
// report: when persistent.size is unset (0), PartitionsByInstallOrder places
// persistent last; the extra partition sits before it with its configured
// fixed size.
func TestExpandingPersistentWorks(t *testing.T) {
	const sector int64 = 512
	const diskSize int64 = 3750748848 * 512
	diskSectors := diskSectorsFromBytes(diskSize, sector)

	// Order when persistent expands: [efi, oem, recovery, state, data_partition, persistent].
	parts := partitions.PartitionList{
		{Name: sdkConstants.EfiPartName, FS: sdkConstants.EfiFs, FilesystemLabel: sdkConstants.EfiLabel, Size: 64},
		{Name: sdkConstants.OEMPartName, FilesystemLabel: sdkConstants.OEMLabel, Size: 5000},
		{Name: sdkConstants.RecoveryPartName, FilesystemLabel: sdkConstants.RecoveryLabel, Size: 20200},
		{Name: sdkConstants.StatePartName, FilesystemLabel: sdkConstants.StateLabel, Size: 25576},
		{Name: "data_partition", FilesystemLabel: "SYSTEM_DATA", FS: "ext4", Size: 300000},
		{Name: sdkConstants.PersistentPartName, FilesystemLabel: sdkConstants.PersistentLabel, Size: 0},
	}

	got := kairosPartsToDiskfsGPTParts(parts, diskSize, sector)

	const partitionArraySectors uint64 = 128 * 128 / 512
	secondaryHeader := diskSectors - 1
	lastDataSector := secondaryHeader - partitionArraySectors - 1

	last := got[len(got)-1]
	if last.Name != sdkConstants.PersistentPartName {
		t.Fatalf("expected last partition to be persistent, got %q", last.Name)
	}
	if last.End > lastDataSector {
		t.Errorf("expanding persistent End=%d exceeds lastDataSector=%d",
			last.End, lastDataSector)
	}

	// data_partition (not last) should have exactly its configured size.
	data := got[4]
	if data.Name != "data_partition" {
		t.Fatalf("expected partition 4 to be data_partition, got %q", data.Name)
	}
	if data.Size != uint64(300000)*1024*1024 {
		t.Errorf("data_partition size = %d bytes, want %d",
			data.Size, uint64(300000)*1024*1024)
	}
}

// TestBug4257FullDiskWrite covers the same layout end-to-end: it writes a real
// GPT to a sparse temp file via go-diskfs and reads it back, mirroring what
// happens on a real install. If the primary GPT that lands on disk has the
// last partition's End sector past lastDataSector, this test catches it.
//
// The bug report used a 1.7 TiB disk, but the pathological case (extras
// partition is last and fixed size) triggers regardless of scale. We use a
// small sparse file so the test is fast and portable.
func TestBug4257FullDiskWrite(t *testing.T) {
	// 2 GiB is enough to hold all six partitions with plausible sizes.
	const diskBytes int64 = 2 * 1024 * 1024 * 1024
	const sectorSize int64 = 512

	dir := t.TempDir()
	imgPath := filepath.Join(dir, "disk.img")
	f, err := os.Create(imgPath)
	if err != nil {
		t.Fatalf("create img: %v", err)
	}
	if err := f.Truncate(diskBytes); err != nil {
		t.Fatalf("truncate img: %v", err)
	}
	_ = f.Close()

	parts := partitions.PartitionList{
		{Name: sdkConstants.EfiPartName, FS: sdkConstants.EfiFs, FilesystemLabel: sdkConstants.EfiLabel, Size: 64},
		{Name: sdkConstants.OEMPartName, FilesystemLabel: sdkConstants.OEMLabel, Size: 100},
		{Name: sdkConstants.RecoveryPartName, FilesystemLabel: sdkConstants.RecoveryLabel, Size: 200},
		{Name: sdkConstants.StatePartName, FilesystemLabel: sdkConstants.StateLabel, Size: 200},
		// Persistent fixed size — reproduces the bug trigger.
		{Name: sdkConstants.PersistentPartName, FilesystemLabel: sdkConstants.PersistentLabel, Size: 500},
		// Extras partition is now LAST with a fixed size.
		{Name: "data_partition", FilesystemLabel: "SYSTEM_DATA", FS: "ext4", Size: 500},
	}

	d, err := NewDisk(imgPath)
	if err != nil {
		t.Fatalf("NewDisk: %v", err)
	}
	if err := d.NewPartitionTable(sdkConstants.GPT, parts); err != nil {
		t.Fatalf("NewPartitionTable: %v", err)
	}
	if err := d.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Re-open and read the on-disk GPT.
	d2, err := diskfs.Open(imgPath)
	if err != nil {
		t.Fatalf("diskfs.Open: %v", err)
	}
	defer func() { _ = d2.Close() }()

	tbl, err := d2.GetPartitionTable()
	if err != nil {
		t.Fatalf("GetPartitionTable: %v", err)
	}
	gptTable, ok := tbl.(*gpt.Table)
	if !ok {
		t.Fatalf("expected *gpt.Table, got %T", tbl)
	}

	diskSectors := uint64(diskBytes / sectorSize)
	const partitionArraySectors uint64 = 128 * 128 / 512
	lastDataSector := diskSectors - 1 - partitionArraySectors - 1

	tableParts := gptTable.Partitions
	// Find our named partitions inside the fixed-size (128 entries) array.
	byName := map[string]*gpt.Partition{}
	for _, p := range tableParts {
		if p == nil || p.Name == "" {
			continue
		}
		byName[p.Name] = p
	}

	last := byName["data_partition"]
	if last == nil {
		t.Fatal("data_partition missing from on-disk GPT")
	}
	if last.End > lastDataSector {
		t.Errorf("on-disk data_partition End=%d > lastDataSector=%d — backup GPT will overlap",
			last.End, lastDataSector)
	}
	wantMax := uint64(500) * 1024 * 1024
	onDiskSize := (last.End - last.Start + 1) * uint64(sectorSize)
	if onDiskSize > wantMax {
		t.Errorf("on-disk data_partition size %d bytes > configured %d bytes", onDiskSize, wantMax)
	}
	if last.Name != "data_partition" {
		t.Errorf("on-disk last partition PARTLABEL missing/wrong: got %q, want data_partition", last.Name)
	}

	persistent := byName[sdkConstants.PersistentPartName]
	if persistent == nil {
		t.Fatal("persistent missing from on-disk GPT")
	}
	wantPersistent := uint64(500) * 1024 * 1024
	onDiskPersistent := (persistent.End - persistent.Start + 1) * uint64(sectorSize)
	if onDiskPersistent != wantPersistent {
		t.Errorf("persistent on-disk size = %d, want %d", onDiskPersistent, wantPersistent)
	}
}

// TestBug4257ExactDiskFromReport uses the exact disk geometry from
// kairos-io/kairos#4257 (3750748848 * 512 sectors ≈ 1.7 TiB) via a sparse
// temp file. If any code path depends on the specific disk size or on
// partition sizes near the reported 300000 MiB, this catches it.
func TestBug4257ExactDiskFromReport(t *testing.T) {
	const sectorSize int64 = 512
	const diskBytes int64 = 3750748848 * sectorSize

	dir := t.TempDir()
	imgPath := filepath.Join(dir, "disk.img")
	f, err := os.Create(imgPath)
	if err != nil {
		t.Fatalf("create img: %v", err)
	}
	// Truncate produces a sparse file — no real 1.7 TiB allocation.
	if err := f.Truncate(diskBytes); err != nil {
		t.Fatalf("truncate img: %v", err)
	}
	_ = f.Close()

	parts := partitions.PartitionList{
		{Name: sdkConstants.EfiPartName, FS: sdkConstants.EfiFs, FilesystemLabel: sdkConstants.EfiLabel, Size: 64},
		{Name: sdkConstants.OEMPartName, FilesystemLabel: sdkConstants.OEMLabel, Size: 5000},
		{Name: sdkConstants.RecoveryPartName, FilesystemLabel: sdkConstants.RecoveryLabel, Size: 20200},
		{Name: sdkConstants.StatePartName, FilesystemLabel: sdkConstants.StateLabel, Size: 25576},
		{Name: sdkConstants.PersistentPartName, FilesystemLabel: sdkConstants.PersistentLabel, Size: 300000},
		{Name: "data_partition", FilesystemLabel: "SYSTEM_DATA", FS: "ext4", Size: 300000},
	}

	d, err := NewDisk(imgPath)
	if err != nil {
		t.Fatalf("NewDisk: %v", err)
	}
	if err := d.NewPartitionTable(sdkConstants.GPT, parts); err != nil {
		t.Fatalf("NewPartitionTable: %v", err)
	}
	if err := d.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	d2, err := diskfs.Open(imgPath)
	if err != nil {
		t.Fatalf("diskfs.Open: %v", err)
	}
	defer func() { _ = d2.Close() }()

	tbl, err := d2.GetPartitionTable()
	if err != nil {
		t.Fatalf("GetPartitionTable: %v", err)
	}
	gptTable := tbl.(*gpt.Table)

	byName := map[string]*gpt.Partition{}
	for _, p := range gptTable.Partitions {
		if p == nil || p.Name == "" {
			continue
		}
		byName[p.Name] = p
	}

	last := byName["data_partition"]
	if last == nil {
		t.Fatal("data_partition missing from on-disk GPT — the bug the report describes")
	}
	wantMax := uint64(300000) * 1024 * 1024
	onDiskSize := (last.End - last.Start + 1) * uint64(sectorSize)
	if onDiskSize > wantMax {
		t.Errorf("on-disk data_partition = %d bytes (%d MiB) > configured 300000 MiB — this is the bug",
			onDiskSize, onDiskSize/(1024*1024))
	}

	diskSectors := uint64(diskBytes / sectorSize)
	const partitionArraySectors uint64 = 128 * 128 / 512
	lastDataSector := diskSectors - 1 - partitionArraySectors - 1
	if last.End > lastDataSector {
		t.Errorf("on-disk data_partition End=%d > lastDataSector=%d — GPT will be corrupted",
			last.End, lastDataSector)
	}
	t.Logf("data_partition: Start=%d End=%d size_bytes=%d name=%q",
		last.Start, last.End, onDiskSize, last.Name)
	t.Logf("lastDataSector=%d diskSectors=%d", lastDataSector, diskSectors)
	t.Logf("img path (kept in temp dir for inspection): %s", imgPath)
}
