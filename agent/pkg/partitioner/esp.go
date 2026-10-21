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
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math/bits"

	sdkConstants "github.com/kairos-io/kairos/v4/sdk/constants"
	"github.com/kairos-io/kairos/v4/sdk/types/fs"
	"github.com/kairos-io/kairos/v4/sdk/types/logger"
	"github.com/kairos-io/kairos/v4/sdk/types/partitions"
)

// fat16MaxClusters is the cluster count from which a FAT volume can only be
// FAT32 (FAT_MAX_FAT16_CLUSTER in EDK2's FatPkg).
const fat16MaxClusters = 65525

var errNotFAT = errors.New("not a FAT boot sector")

// IsESP tells whether p is laid out as the EFI system partition. The name
// alone is not enough: a BIOS install may carry an extra partition named efi
// with any filesystem.
func IsESP(p *partitions.Partition) bool {
	return p.Name == sdkConstants.EfiPartName && p.FS == sdkConstants.EfiFs
}

// BPB is the BIOS parameter block, which starts at byte 11 of a FAT boot
// sector. The fields follow the order and sizes of the FAT specification.
type BPB struct {
	BytesPerSector    uint16
	SectorsPerCluster uint8
	ReservedSectors   uint16
	NumFATs           uint8
	RootEntries       uint16
	TotalSectors16    uint16
	Media             uint8
	FATSize16         uint16
	SectorsPerTrack   uint16
	NumHeads          uint16
	HiddenSectors     uint32
	TotalSectors32    uint32
	FATSize32         uint32
}

func (b BPB) bootSector() []byte {
	bs := make([]byte, 512)
	_, _ = binary.Encode(bs[11:], binary.LittleEndian, b)
	bs[510], bs[511] = 0x55, 0xAA
	return bs
}

// FAT32BootSector returns the boot sector of a FAT32 volume with the given
// number of clusters, for tests that need an ESP UEFI does or does not mount.
func FAT32BootSector(clusters uint32) []byte {
	const bytesPerSector, reserved, numFATs = 512, 32, 2
	fatSize := ((clusters+2)*4 + bytesPerSector - 1) / bytesPerSector
	return BPB{
		BytesPerSector: bytesPerSector, SectorsPerCluster: 1, ReservedSectors: reserved, NumFATs: numFATs,
		TotalSectors32: reserved + numFATs*fatSize + clusters, FATSize32: fatSize,
	}.bootSector()
}

// checkFATBootSector tells whether UEFI firmware will mount the FAT
// filesystem whose boot sector is bs. The firmware reads the FAT type off the
// boot sector (FAT32 when the 16-bit FAT size is zero) and refuses the volume
// when its cluster count belongs to the other type. Linux mounts either, so a
// mkfs that gets this wrong goes unnoticed until the machine does not boot.
// The rule follows FatOpenDevice in EDK2's FatPkg/EnhancedFatDxe/Init.c.
func checkFATBootSector(bs []byte) error {
	if len(bs) < 512 {
		return errNotFAT
	}
	var b BPB
	if _, err := binary.Decode(bs[11:], binary.LittleEndian, &b); err != nil {
		return errNotFAT
	}
	bytesPerSector := uint64(b.BytesPerSector)
	sectorsPerCluster := uint64(b.SectorsPerCluster)
	reserved := uint64(b.ReservedSectors)
	numFATs := uint64(b.NumFATs)
	sectors := uint64(b.TotalSectors16)
	if sectors == 0 {
		sectors = uint64(b.TotalSectors32)
	}
	fat32 := false
	fatSize := uint64(b.FATSize16)
	if fatSize == 0 {
		fatSize = uint64(b.FATSize32)
		fat32 = true
	}

	if bits.OnesCount64(bytesPerSector) != 1 || bits.OnesCount64(sectorsPerCluster) != 1 ||
		reserved == 0 || numFATs == 0 || fatSize == 0 {
		return errNotFAT
	}
	firstDataSector := reserved + numFATs*fatSize + (uint64(b.RootEntries)*32+bytesPerSector-1)/bytesPerSector
	if sectors <= firstDataSector {
		return errNotFAT
	}
	clusters := (sectors - firstDataSector) / sectorsPerCluster

	switch {
	case fat32 && clusters < fat16MaxClusters:
		return fmt.Errorf("FAT32 with %d clusters, fewer than %d", clusters, fat16MaxClusters)
	case !fat32 && clusters >= fat16MaxClusters:
		return fmt.Errorf("FAT16 with %d clusters, more than %d", clusters, fat16MaxClusters-1)
	}
	return nil
}

// CheckESP fails when UEFI firmware would refuse the FAT filesystem just
// written to device. A device it cannot read is logged and passed: the check
// exists to catch a broken mkfs, not to add a failure of its own.
func CheckESP(log logger.KairosLogger, fsys fs.KairosFS, device string) error {
	f, err := fsys.Open(device)
	if err != nil {
		log.Warnf("Skipping the EFI system partition check, cannot open %s: %s", device, err)
		return nil
	}
	defer f.Close()
	bs := make([]byte, 512)
	if _, err := io.ReadFull(f, bs); err != nil {
		log.Warnf("Skipping the EFI system partition check, cannot read %s: %s", device, err)
		return nil
	}
	if err := checkFATBootSector(bs); err != nil {
		return fmt.Errorf("UEFI firmware will not mount the EFI system partition on %s: %w", device, err)
	}
	return nil
}
