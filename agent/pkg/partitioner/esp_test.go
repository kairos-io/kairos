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
	"bytes"
	"encoding/binary"
	"encoding/hex"

	"github.com/kairos-io/kairos/v4/sdk/types/logger"
	"github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/twpayne/go-vfs/v5/vfst"
)

// Boot sectors (first 64 bytes) written to 64 MiB images by mkfs.fat 4.2
// with -S 4096 and by BusyBox 1.37 mkdosfs.
const (
	mkfsFATSector      = "eb3c906d6b66732e66617400100404000200020040f80400200002000000000000000000800029fc780b274e4f204e414d452020202046415431362020200e1f"
	busyboxFAT32Sector = "eb58906d6b646f7366730000020106000200000000f800003f00ff000000000000000200f1030000000000000200000001000300000000000000000000000000"
)

func realSector(h string) []byte {
	b, err := hex.DecodeString(h)
	Expect(err).ToNot(HaveOccurred())
	bs := make([]byte, 512)
	copy(bs, b)
	return bs
}

var _ = ginkgo.Describe("BPB", ginkgo.Label("esp"), func() {
	ginkgo.It("maps the fields of a mkfs.fat FAT16 boot sector", func() {
		var b BPB
		_, err := binary.Decode(realSector(mkfsFATSector)[11:], binary.LittleEndian, &b)
		Expect(err).ToNot(HaveOccurred())
		Expect(b).To(Equal(BPB{
			BytesPerSector: 4096, SectorsPerCluster: 4, ReservedSectors: 4, NumFATs: 2,
			RootEntries: 512, TotalSectors16: 16384, Media: 0xf8, FATSize16: 4,
			SectorsPerTrack: 32, NumHeads: 2, FATSize32: 0xfc290080,
		}))
	})

	ginkgo.It("maps the fields of a BusyBox FAT32 boot sector", func() {
		var b BPB
		_, err := binary.Decode(realSector(busyboxFAT32Sector)[11:], binary.LittleEndian, &b)
		Expect(err).ToNot(HaveOccurred())
		Expect(b).To(Equal(BPB{
			BytesPerSector: 512, SectorsPerCluster: 1, ReservedSectors: 6, NumFATs: 2,
			Media: 0xf8, SectorsPerTrack: 63, NumHeads: 255, TotalSectors32: 131072, FATSize32: 1009,
		}))
	})

	ginkgo.It("covers bytes 11 to 39 and writes back what it read", func() {
		for _, h := range []string{mkfsFATSector, busyboxFAT32Sector} {
			bs := realSector(h)
			var b BPB
			n, err := binary.Decode(bs[11:], binary.LittleEndian, &b)
			Expect(err).ToNot(HaveOccurred())
			Expect(n).To(Equal(29))
			Expect(b.bootSector()[11:40]).To(Equal(bs[11:40]))
		}
	})
})

var _ = ginkgo.Describe("FAT32BootSector", ginkgo.Label("esp"), func() {
	ginkgo.It("describes a FAT32 volume with the given cluster count", func() {
		var b BPB
		_, err := binary.Decode(FAT32BootSector(65525)[11:], binary.LittleEndian, &b)
		Expect(err).ToNot(HaveOccurred())
		Expect(b.FATSize32*uint32(b.BytesPerSector)/4).To(BeNumerically(">=", 65525+2), "the FAT has an entry for every cluster")
		Expect(checkFATBootSector(FAT32BootSector(65525))).To(Succeed())
		err = checkFATBootSector(FAT32BootSector(65524))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("FAT32 with 65524 clusters"))
	})
})

// The busybox and dosfstools cases carry the fields those tools wrote on a
// 4096-byte-sector device, with the cluster count fsck.vfat reported.
var _ = ginkgo.Describe("CheckFATBootSector", ginkgo.Label("esp"), func() {
	ginkgo.It("rejects FAT32 with fewer than 65525 clusters (busybox, 64 MiB)", func() {
		// 16346 clusters
		err := checkFATBootSector(BPB{
			BytesPerSector: 4096, SectorsPerCluster: 1, ReservedSectors: 6, NumFATs: 2,
			TotalSectors16: 16384, TotalSectors32: 16384, FATSize32: 16,
		}.bootSector())
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("FAT32 with 16346 clusters"))
	})

	ginkgo.It("rejects FAT32 with fewer than 65525 clusters (busybox, 260 MiB)", func() {
		// 8317 clusters
		err := checkFATBootSector(BPB{
			BytesPerSector: 4096, SectorsPerCluster: 8, ReservedSectors: 6, NumFATs: 2,
			TotalSectors32: 66560, FATSize32: 9,
		}.bootSector())
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("FAT32 with 8317 clusters"))
	})

	ginkgo.It("accepts FAT32 with exactly 65525 clusters", func() {
		Expect(checkFATBootSector(BPB{
			BytesPerSector: 512, SectorsPerCluster: 1, ReservedSectors: 32, NumFATs: 2,
			TotalSectors32: 32 + 2*512 + 65525, FATSize32: 512,
		}.bootSector())).To(Succeed())
	})

	ginkgo.It("rejects FAT32 with 65524 clusters", func() {
		Expect(checkFATBootSector(BPB{
			BytesPerSector: 512, SectorsPerCluster: 1, ReservedSectors: 32, NumFATs: 2,
			TotalSectors32: 32 + 2*512 + 65524, FATSize32: 512,
		}.bootSector())).ToNot(Succeed())
	})

	ginkgo.It("accepts FAT16 (dosfstools, 64 MiB)", func() {
		// 4092 clusters
		Expect(checkFATBootSector(BPB{
			BytesPerSector: 4096, SectorsPerCluster: 4, ReservedSectors: 4, NumFATs: 2,
			RootEntries: 512, TotalSectors16: 16384, FATSize16: 4,
		}.bootSector())).To(Succeed())
	})

	ginkgo.It("accepts FAT12, which the firmware tells from FAT16 by the count", func() {
		Expect(checkFATBootSector(BPB{
			BytesPerSector: 512, SectorsPerCluster: 4, ReservedSectors: 1, NumFATs: 2,
			RootEntries: 512, TotalSectors16: 8192, FATSize16: 6,
		}.bootSector())).To(Succeed())
	})

	ginkgo.It("rejects FAT12/16 with 65525 clusters or more", func() {
		// 512 root entries take 32 sectors
		err := checkFATBootSector(BPB{
			BytesPerSector: 512, SectorsPerCluster: 1, ReservedSectors: 1, NumFATs: 2,
			RootEntries: 512, TotalSectors32: 1 + 2*256 + 32 + 65525, FATSize16: 256,
		}.bootSector())
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("FAT16 with 65525 clusters"))
	})

	ginkgo.It("accepts FAT16 with 65524 clusters", func() {
		Expect(checkFATBootSector(BPB{
			BytesPerSector: 512, SectorsPerCluster: 1, ReservedSectors: 1, NumFATs: 2,
			RootEntries: 512, TotalSectors32: 1 + 2*256 + 32 + 65524, FATSize16: 256,
		}.bootSector())).To(Succeed())
	})

	ginkgo.It("uses the 16-bit sector count when it is set", func() {
		// TotalSectors32 alone would give 65525+ clusters
		Expect(checkFATBootSector(BPB{
			BytesPerSector: 512, SectorsPerCluster: 1, ReservedSectors: 1, NumFATs: 2,
			RootEntries: 512, TotalSectors16: 40000, TotalSectors32: 200000, FATSize16: 256,
		}.bootSector())).To(Succeed())
	})

	ginkgo.It("rejects a sector that is not a FAT boot sector", func() {
		for _, bs := range [][]byte{
			make([]byte, 512),
			BPB{BytesPerSector: 4096, SectorsPerCluster: 1, ReservedSectors: 6, NumFATs: 2, TotalSectors32: 100}.bootSector(),
			BPB{BytesPerSector: 3000, SectorsPerCluster: 1, ReservedSectors: 6, NumFATs: 2, TotalSectors32: 100000, FATSize32: 100}.bootSector(),
			BPB{BytesPerSector: 4096, SectorsPerCluster: 3, ReservedSectors: 6, NumFATs: 2, TotalSectors32: 100000, FATSize32: 100}.bootSector(),
			BPB{BytesPerSector: 4096, SectorsPerCluster: 1, ReservedSectors: 0, NumFATs: 2, TotalSectors32: 100000, FATSize32: 100}.bootSector(),
			BPB{BytesPerSector: 4096, SectorsPerCluster: 1, ReservedSectors: 6, NumFATs: 2, TotalSectors32: 38, FATSize32: 16}.bootSector(),
			make([]byte, 20),
		} {
			err := checkFATBootSector(bs)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not a FAT boot sector"))
		}
	})
})

var _ = ginkgo.Describe("CheckESP", ginkgo.Label("esp"), func() {
	var memLog *bytes.Buffer
	var log logger.KairosLogger

	ginkgo.BeforeEach(func() {
		memLog = &bytes.Buffer{}
		log = logger.NewBufferLogger(memLog)
	})

	newFS := func(bootSector []byte) *vfst.TestFS {
		fs, cleanup, err := vfst.NewTestFS(map[string]interface{}{"/dev/esp": string(bootSector)})
		Expect(err).ToNot(HaveOccurred())
		ginkgo.DeferCleanup(cleanup)
		return fs
	}

	ginkgo.It("fails on an ESP the firmware will not mount", func() {
		fs := newFS(BPB{
			BytesPerSector: 4096, SectorsPerCluster: 1, ReservedSectors: 6, NumFATs: 2,
			TotalSectors16: 16384, TotalSectors32: 16384, FATSize32: 16,
		}.bootSector())
		err := CheckESP(log, fs, "/dev/esp")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("/dev/esp"))
		Expect(err.Error()).To(ContainSubstring("FAT32 with 16346 clusters"))
	})

	ginkgo.It("passes an ESP the firmware will mount", func() {
		fs := newFS(BPB{
			BytesPerSector: 4096, SectorsPerCluster: 4, ReservedSectors: 4, NumFATs: 2,
			RootEntries: 512, TotalSectors16: 16384, FATSize16: 4,
		}.bootSector())
		Expect(CheckESP(log, fs, "/dev/esp")).To(Succeed())
	})

	ginkgo.It("warns and passes when the device cannot be read", func() {
		fs := newFS(nil)
		Expect(CheckESP(log, fs, "/dev/missing")).To(Succeed())
		Expect(memLog.String()).To(ContainSubstring("/dev/missing"))
	})

	ginkgo.It("warns and passes when the device is shorter than a boot sector", func() {
		fs := newFS([]byte("short"))
		Expect(CheckESP(log, fs, "/dev/esp")).To(Succeed())
		Expect(memLog.String()).To(ContainSubstring("/dev/esp"))
	})
})
