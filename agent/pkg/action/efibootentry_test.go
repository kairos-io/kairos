package action

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"

	cnst "github.com/kairos-io/kairos/v4/agent/pkg/constants"
	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/twpayne/go-vfs/v5"
	"github.com/twpayne/go-vfs/v5/vfst"
)

// A fake machine: a disk image with a GPT whose first partition is the ESP,
// the sysfs that describes it, a by-partuuid directory and an efivars
// directory.
type efiBootFixture struct {
	paths EFIBootEntryPaths
	esp   string
	guid  [16]byte
}

const (
	fixStartLBA = 2048
	fixSizeLBA  = 131072
	fixLoader   = `\EFI\BOOT\BOOTX64.EFI`
)

func newEFIBootFixture() efiBootFixture {
	root := GinkgoT().TempDir()
	write := func(p string, b []byte) {
		ExpectWithOffset(1, os.MkdirAll(filepath.Dir(p), 0o755)).To(Succeed())
		ExpectWithOffset(1, os.WriteFile(p, b, 0o644)).To(Succeed())
	}
	guid := [16]byte{0x82, 0x2e, 0x44, 0x81, 0xb6, 0x66, 0xd2, 0x5e, 0x96, 0x1b, 0x76, 0x65, 0xf5, 0x12, 0xbe, 0x2e}

	// Protective MBR, GPT header at LBA 1, partition entries at LBA 2.
	img := make([]byte, 64*512)
	hdr := img[512:]
	copy(hdr, "EFI PART")
	binary.LittleEndian.PutUint64(hdr[72:], 2)
	binary.LittleEndian.PutUint32(hdr[80:], 128)
	binary.LittleEndian.PutUint32(hdr[84:], 128)
	ent := img[2*512:]
	copy(ent[16:], guid[:])
	binary.LittleEndian.PutUint64(ent[32:], fixStartLBA)
	binary.LittleEndian.PutUint64(ent[40:], fixStartLBA+fixSizeLBA-1)
	write(filepath.Join(root, "dev", "vda"), img)
	write(filepath.Join(root, "dev", "vda1"), nil)

	disk := filepath.Join(root, "sys", "devices", "virtio0", "block", "vda")
	write(filepath.Join(disk, "queue", "logical_block_size"), []byte("512\n"))
	write(filepath.Join(disk, "vda1", "partition"), []byte("1\n"))
	write(filepath.Join(disk, "vda1", "start"), []byte("2048\n"))
	class := filepath.Join(root, "sys", "class", "block")
	Expect(os.MkdirAll(class, 0o755)).To(Succeed())
	Expect(os.Symlink(disk, filepath.Join(class, "vda"))).To(Succeed())
	Expect(os.Symlink(filepath.Join(disk, "vda1"), filepath.Join(class, "vda1"))).To(Succeed())

	parts := filepath.Join(root, "by-partuuid")
	write(filepath.Join(parts, gptGUIDString(guid)), nil)
	efivars := filepath.Join(root, "efivars")
	Expect(os.MkdirAll(efivars, 0o755)).To(Succeed())

	return efiBootFixture{
		paths: EFIBootEntryPaths{SysBlock: class, EFIVars: efivars, ByPartUUID: parts},
		esp:   filepath.Join(root, "dev", "vda1"),
		guid:  guid,
	}
}

func (f efiBootFixture) set(name string, data []byte) {
	ExpectWithOffset(1, writeEFIVar(f.paths.EFIVars, name, data)).To(Succeed())
}

func (f efiBootFixture) order() []uint16 {
	o, err := readBootOrder(f.paths.EFIVars)
	ExpectWithOffset(1, err).ToNot(HaveOccurred())
	return o
}

func (f efiBootFixture) run() string {
	var out bytes.Buffer
	ExpectWithOffset(1, WriteEFIBootEntry(f.paths, f.esp, fixLoader, "Kairos", &out)).To(Succeed())
	return out.String()
}

// otherOption is a load option for a partition this fixture's disk does not
// have, unless its GUID is added to by-partuuid.
func otherOption(description string, guidByte byte) []byte {
	dp := hardDriveDevicePath(espPartition{number: 1, startLBA: 2048, sizeLBA: 1000, signature: [16]byte{guidByte}}, `\EFI\ubuntu\shimx64.efi`)
	return efiLoadOption(description, dp)
}

var _ = Describe("UEFI boot entry", Label("efibootentry"), func() {
	It("writes the entry for the ESP and puts it first in BootOrder", func() {
		f := newEFIBootFixture()
		f.set("Boot0000", efiLoadOption("UiApp", []byte{0x7f, 0xff, 4, 0}))
		f.set("Boot0001", otherOption("ubuntu", 0xaa))
		f.set("BootOrder", encodeBootOrder([]uint16{1, 0}))

		Expect(f.run()).To(ContainSubstring("Boot0002"))
		Expect(f.order()).To(Equal([]uint16{2, 1, 0}))

		data, err := readEFIVar(f.paths.EFIVars, "Boot0002")
		Expect(err).ToNot(HaveOccurred())
		Expect(binary.LittleEndian.Uint32(data)).To(Equal(uint32(loadOptionActive)))
		e, ok := parseEFILoadOption(data)
		Expect(ok).To(BeTrue())
		Expect(e.description).To(Equal("Kairos"))

		// HD(1,GPT,<guid>,0x800,0x20000)/File(\EFI\BOOT\BOOTX64.EFI)/End
		dp := e.devicePath
		Expect(dp[:4]).To(Equal([]byte{0x04, 0x01, 42, 0}))
		Expect(binary.LittleEndian.Uint32(dp[4:])).To(Equal(uint32(1)))
		Expect(binary.LittleEndian.Uint64(dp[8:])).To(Equal(uint64(fixStartLBA)))
		Expect(binary.LittleEndian.Uint64(dp[16:])).To(Equal(uint64(fixSizeLBA)))
		Expect(dp[24:40]).To(Equal(f.guid[:]))
		Expect(dp[40:42]).To(Equal([]byte{0x02, 0x02}))
		Expect(dp[42:44]).To(Equal([]byte{0x04, 0x04}))
		Expect(dp[46 : len(dp)-4]).To(Equal(EncondeUtf16LEStringNullTerminated(fixLoader)))
		Expect(dp[len(dp)-4:]).To(Equal([]byte{0x7f, 0xff, 4, 0}))

		_, err = readEFIVar(f.paths.EFIVars, "Boot0001")
		Expect(err).ToNot(HaveOccurred(), "another system's entry must stay")
	})

	It("changes nothing when it runs again", func() {
		f := newEFIBootFixture()
		f.set("Boot0000", otherOption("ubuntu", 0xaa))
		f.set("BootOrder", encodeBootOrder([]uint16{0}))
		f.run()
		f.run()
		Expect(f.order()).To(Equal([]uint16{1, 0}))
		entries, err := readBootEntries(f.paths.EFIVars)
		Expect(err).ToNot(HaveOccurred())
		Expect(entries).To(HaveLen(2))
	})

	It("replaces its own entry for a partition that is gone, and keeps one that still exists", func() {
		f := newEFIBootFixture()
		f.set("Boot0000", otherOption("Windows Boot Manager", 0xbb))
		f.set("Boot0003", otherOption("Kairos", 0xcc)) // a reinstall left this one
		f.set("Boot0004", otherOption("Kairos", 0xdd)) // another disk still has this one
		Expect(os.WriteFile(filepath.Join(f.paths.ByPartUUID, gptGUIDString([16]byte{0xdd})), nil, 0o644)).To(Succeed())
		f.set("BootOrder", encodeBootOrder([]uint16{3, 0, 4}))

		f.run()
		Expect(f.order()).To(Equal([]uint16{3, 0, 4}))
		data, err := readEFIVar(f.paths.EFIVars, "Boot0003")
		Expect(err).ToNot(HaveOccurred())
		e, _ := parseEFILoadOption(data)
		Expect(e.devicePath[24:40]).To(Equal(f.guid[:]))
		_, err = readEFIVar(f.paths.EFIVars, "Boot0004")
		Expect(err).ToNot(HaveOccurred())
	})

	It("refuses on a system that did not boot with UEFI", func() {
		f := newEFIBootFixture()
		f.paths.EFIVars = filepath.Join(f.paths.EFIVars, "missing")
		err := WriteEFIBootEntry(f.paths, f.esp, fixLoader, "Kairos", &bytes.Buffer{})
		Expect(err).To(MatchError(ContainSubstring("UEFI")))
	})

	It("writes nothing for a device that is not a GPT partition", func() {
		f := newEFIBootFixture()
		err := WriteEFIBootEntry(f.paths, filepath.Join(filepath.Dir(f.esp), "vda"), fixLoader, "Kairos", &bytes.Buffer{})
		Expect(err).To(HaveOccurred())
		ents, _ := os.ReadDir(f.paths.EFIVars)
		Expect(ents).To(BeEmpty())
	})

	It("formats a GPT GUID the way udev names it", func() {
		g := [16]byte{0x82, 0x2e, 0x44, 0x81, 0xb6, 0x66, 0xd2, 0x5e, 0x96, 0x1b, 0x76, 0x65, 0xf5, 0x12, 0xbe, 0x2e}
		Expect(gptGUIDString(g)).To(Equal("81442e82-66b6-5ed2-961b-7665f512be2e"))
	})

	It("names the loader the GRUB install writes", func() {
		Expect(RemovableMediaLoader(cnst.GetFallBackEfi(cnst.ArchAmd64))).To(Equal(`\EFI\BOOT\BOOTX64.EFI`))
		Expect(RemovableMediaLoader(cnst.GetFallBackEfi(cnst.ArchArm64))).To(Equal(`\EFI\BOOT\BOOTAA64.EFI`))
	})

	Describe("efiBootEntryName", func() {
		var fs vfs.FS
		var cleanup func()
		var cfg *sdkConfig.Config
		BeforeEach(func() {
			var err error
			fs, cleanup, err = vfst.NewTestFS(map[string]interface{}{})
			Expect(err).ToNot(HaveOccurred())
			cfg = &sdkConfig.Config{Fs: fs}
		})
		AfterEach(func() { cleanup() })

		It("uses the configured GRUB entry name first", func() {
			Expect(efiBootEntryName(cfg, "/img", "Custom")).To(Equal("Custom"))
		})
		It("reads GRUB_ENTRY_NAME from the image", func() {
			Expect(fs.Mkdir("/img", 0o755)).To(Succeed())
			Expect(fs.Mkdir("/img/etc", 0o755)).To(Succeed())
			Expect(fs.WriteFile("/img/etc/kairos-release", []byte("GRUB_ENTRY_NAME=\"Isotope\"\n"), 0o644)).To(Succeed())
			Expect(efiBootEntryName(cfg, "/img", "")).To(Equal("Isotope"))
		})
		It("falls back to the default GRUB entry name", func() {
			Expect(efiBootEntryName(cfg, "/img", "")).To(Equal(cnst.GrubDefEntry))
		})
	})
})
