package action

// A UEFI boot entry for the disk an install just wrote.
//
// The GRUB install puts shim and GRUB on the ESP only at the removable-media
// path, \EFI\BOOT\BOOTX64.EFI (GetFallBackEfi), and writes no Boot####
// variable. A firmware boots that path only when its boot list does not stop
// it first. On a machine that had another system, the boot list still names
// that system's entries (an "ubuntu" entry on a partition the install just
// erased, a Windows Boot Manager on another disk), and many firmwares try
// those, fail or boot the other system, and never look at the new disk.
//
// WriteEFIBootEntry writes a Boot#### variable for the loader on the ESP and
// puts it first in BootOrder. It changes only variables it can prove are its
// own: the entry for this exact partition and file, and older entries with
// the same description whose partition no longer exists on any disk (the ones
// a reinstall leaves behind).

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
)

// efiGlobalVariableGUID is EFI_GLOBAL_VARIABLE, the vendor of Boot#### and
// BootOrder.
const efiGlobalVariableGUID = "8be4df61-93ca-11d2-aa0d-00e098032b8c"

const (
	// NON_VOLATILE | BOOTSERVICE_ACCESS | RUNTIME_ACCESS
	efiBootVarAttrs   = 0x7
	loadOptionActive  = 0x1
	hdNodeLen         = 42
	gptSignatureType  = 0x02
	gptPartitionTable = 0x02
)

// EFIBootEntryPaths holds every path WriteEFIBootEntry reads or writes, so a
// test can point all of them at a temporary directory.
type EFIBootEntryPaths struct {
	SysBlock   string // /sys/class/block
	EFIVars    string // the efivarfs mount
	ByPartUUID string // /dev/disk/by-partuuid; stale entries are kept when it is missing
}

// DefaultEFIBootEntryPaths are the paths on a running system.
func DefaultEFIBootEntryPaths() EFIBootEntryPaths {
	return EFIBootEntryPaths{
		SysBlock:   "/sys/class/block",
		EFIVars:    "/sys/firmware/efi/efivars",
		ByPartUUID: "/dev/disk/by-partuuid",
	}
}

// WriteEFIBootEntry writes a boot entry named description for loader (a path
// on the ESP such as \EFI\BOOT\BOOTX64.EFI) on the ESP partition device esp,
// and puts it first in BootOrder. It logs what it did to out.
func WriteEFIBootEntry(p EFIBootEntryPaths, esp, loader, description string, out io.Writer) error {
	if fi, err := os.Stat(p.EFIVars); err != nil || !fi.IsDir() {
		return fmt.Errorf("no %s: the system did not boot with UEFI", p.EFIVars)
	}
	part, err := readESPPartition(p.SysBlock, esp)
	if err != nil {
		return err
	}
	devicePath := hardDriveDevicePath(part, loader)
	option := efiLoadOption(description, devicePath)

	entries, err := readBootEntries(p.EFIVars)
	if err != nil {
		return err
	}
	num := -1
	var stale []int
	for _, e := range entries {
		switch {
		case num < 0 && bytes.Equal(e.devicePath, devicePath):
			num = e.num
		case e.description == description && e.partitionGone(p.ByPartUUID):
			stale = append(stale, e.num)
		}
	}
	if num < 0 {
		num = freeBootNumber(entries, stale)
	}
	for _, n := range stale {
		if n == num {
			continue
		}
		if err := removeEFIVar(p.EFIVars, bootVarName(n)); err != nil {
			return err
		}
		fmt.Fprintf(out, "removed the stale boot entry Boot%04X\n", n)
	}
	if err := writeEFIVar(p.EFIVars, bootVarName(num), option); err != nil {
		return err
	}
	order, err := readBootOrder(p.EFIVars)
	if err != nil {
		return err
	}
	if err := writeEFIVar(p.EFIVars, "BootOrder", encodeBootOrder(bootFirst(order, num, stale))); err != nil {
		return err
	}
	fmt.Fprintf(out, "Boot%04X %q: %s partition %d %s, first in BootOrder\n", num, description, part.disk, part.number, loader)
	return nil
}

// RemovableMediaLoader is the ESP path of the loader the GRUB install writes
// for arch, in the form a device path takes.
func RemovableMediaLoader(fallbackName string) string {
	return `\EFI\BOOT\` + strings.ToUpper(fallbackName)
}

type espPartition struct {
	disk      string
	number    uint32
	startLBA  uint64 // in the disk's logical blocks
	sizeLBA   uint64
	signature [16]byte // the GPT unique partition GUID, as stored on disk
}

// readESPPartition finds the partition behind dev in sysfs and reads its GPT
// entry from the disk.
func readESPPartition(sysBlock, dev string) (espPartition, error) {
	var p espPartition
	if real, err := filepath.EvalSymlinks(dev); err == nil {
		dev = real
	}
	sysPart, err := filepath.EvalSymlinks(filepath.Join(sysBlock, filepath.Base(dev)))
	if err != nil {
		return p, fmt.Errorf("%s has no sysfs entry: %w", dev, err)
	}
	num, err := readSysUint(filepath.Join(sysPart, "partition"))
	if err != nil {
		return p, fmt.Errorf("%s is not a partition: %w", dev, err)
	}
	diskName := filepath.Base(filepath.Dir(sysPart))
	lbs, err := readSysUint(filepath.Join(sysBlock, diskName, "queue", "logical_block_size"))
	if err != nil || lbs == 0 {
		lbs = 512
	}
	p.disk = filepath.Join(filepath.Dir(dev), diskName)
	p.number = uint32(num)

	f, err := os.Open(p.disk)
	if err != nil {
		return p, err
	}
	defer f.Close()
	first, last, unique, err := readGPTEntry(f, lbs, p.number)
	if err != nil {
		return p, fmt.Errorf("%s: %w", p.disk, err)
	}
	p.startLBA, p.sizeLBA, p.signature = first, last-first+1, unique

	// sysfs counts in 512-byte sectors whatever the block size. The GPT is
	// the source of truth; this only catches reading the wrong disk.
	if start, err := readSysUint(filepath.Join(sysPart, "start")); err == nil && start*512 != p.startLBA*lbs {
		return p, fmt.Errorf("%s: the GPT puts partition %d at LBA %d, the kernel at sector %d", p.disk, p.number, p.startLBA, start)
	}
	return p, nil
}

func readSysUint(path string) (uint64, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.ParseUint(strings.TrimSpace(string(b)), 10, 64)
}

// readGPTEntry reads partition entry num (1-based) of the primary GPT.
func readGPTEntry(r io.ReaderAt, lbs uint64, num uint32) (first, last uint64, unique [16]byte, err error) {
	hdr := make([]byte, 92)
	if _, err = r.ReadAt(hdr, int64(lbs)); err != nil {
		return
	}
	if string(hdr[:8]) != "EFI PART" {
		err = errors.New("no GPT")
		return
	}
	entriesLBA := binary.LittleEndian.Uint64(hdr[72:])
	count := binary.LittleEndian.Uint32(hdr[80:])
	size := binary.LittleEndian.Uint32(hdr[84:])
	if num == 0 || num > count || size < 128 {
		err = fmt.Errorf("no GPT entry %d", num)
		return
	}
	buf := make([]byte, 128)
	if _, err = r.ReadAt(buf, int64(entriesLBA*lbs)+int64(num-1)*int64(size)); err != nil {
		return
	}
	copy(unique[:], buf[16:32])
	first = binary.LittleEndian.Uint64(buf[32:])
	last = binary.LittleEndian.Uint64(buf[40:])
	if first == 0 || last < first {
		err = fmt.Errorf("GPT entry %d is empty", num)
	}
	return
}

// hardDriveDevicePath is HD(part,GPT,guid,start,size)/File(loader)/End, the
// device path efibootmgr writes for a GPT ESP.
func hardDriveDevicePath(p espPartition, loader string) []byte {
	var b bytes.Buffer
	b.Write([]byte{0x04, 0x01, hdNodeLen, 0}) // media device path, hard drive
	_ = binary.Write(&b, binary.LittleEndian, p.number)
	_ = binary.Write(&b, binary.LittleEndian, p.startLBA)
	_ = binary.Write(&b, binary.LittleEndian, p.sizeLBA)
	b.Write(p.signature[:])
	b.Write([]byte{gptPartitionTable, gptSignatureType})
	path := EncondeUtf16LEStringNullTerminated(loader)
	b.Write([]byte{0x04, 0x04}) // media device path, file path
	_ = binary.Write(&b, binary.LittleEndian, uint16(4+len(path)))
	b.Write(path)
	b.Write([]byte{0x7f, 0xff, 4, 0}) // end of the whole device path
	return b.Bytes()
}

// efiLoadOption is an EFI_LOAD_OPTION with no optional data: shim needs none
// to start GRUB from the same directory.
func efiLoadOption(description string, devicePath []byte) []byte {
	var b bytes.Buffer
	_ = binary.Write(&b, binary.LittleEndian, uint32(loadOptionActive))
	_ = binary.Write(&b, binary.LittleEndian, uint16(len(devicePath)))
	b.Write(EncondeUtf16LEStringNullTerminated(description))
	b.Write(devicePath)
	return b.Bytes()
}

type efiBootOption struct {
	num         int
	description string
	devicePath  []byte
}

// partitionGone reports whether the entry names a GPT partition that no disk
// has any more. Without a by-partuuid directory nothing can be proven gone.
func (e efiBootOption) partitionGone(byPartUUID string) bool {
	dp := e.devicePath
	if len(dp) < hdNodeLen || dp[0] != 0x04 || dp[1] != 0x01 || dp[41] != gptSignatureType {
		return false
	}
	if _, err := os.Stat(byPartUUID); err != nil {
		return false
	}
	var sig [16]byte
	copy(sig[:], dp[24:40])
	_, err := os.Stat(filepath.Join(byPartUUID, gptGUIDString(sig)))
	return errors.Is(err, os.ErrNotExist)
}

// gptGUIDString formats an on-disk (mixed-endian) GUID the way udev names it.
func gptGUIDString(g [16]byte) string {
	return fmt.Sprintf("%08x-%04x-%04x-%x-%x",
		binary.LittleEndian.Uint32(g[0:]), binary.LittleEndian.Uint16(g[4:]),
		binary.LittleEndian.Uint16(g[6:]), g[8:10], g[10:16])
}

func bootVarName(n int) string { return fmt.Sprintf("Boot%04X", n) }

func readBootEntries(efivars string) ([]efiBootOption, error) {
	ents, err := os.ReadDir(efivars)
	if err != nil {
		return nil, err
	}
	var out []efiBootOption
	for _, d := range ents {
		name, ok := strings.CutSuffix(d.Name(), "-"+efiGlobalVariableGUID)
		if !ok || len(name) != 8 || !strings.HasPrefix(name, "Boot") || strings.ToUpper(name[4:]) != name[4:] {
			continue
		}
		n, err := strconv.ParseUint(name[4:], 16, 16)
		if err != nil {
			continue
		}
		data, err := readEFIVar(efivars, name)
		if err != nil {
			continue
		}
		if e, ok := parseEFILoadOption(data); ok {
			e.num = int(n)
			out = append(out, e)
		}
	}
	return out, nil
}

func parseEFILoadOption(b []byte) (efiBootOption, bool) {
	var e efiBootOption
	if len(b) < 8 {
		return e, false
	}
	pathLen := int(binary.LittleEndian.Uint16(b[4:]))
	rest := b[6:]
	var desc []uint16
	i := 0
	for ; i+1 < len(rest); i += 2 {
		c := binary.LittleEndian.Uint16(rest[i:])
		if c == 0 {
			break
		}
		desc = append(desc, c)
	}
	start := i + 2
	if start+pathLen > len(rest) {
		return e, false
	}
	e.description = string(utf16.Decode(desc))
	e.devicePath = rest[start : start+pathLen]
	return e, true
}

// freeBootNumber reuses the lowest stale number, or takes the lowest unused.
func freeBootNumber(entries []efiBootOption, stale []int) int {
	if len(stale) > 0 {
		s := append([]int(nil), stale...)
		sort.Ints(s)
		return s[0]
	}
	used := map[int]bool{}
	for _, e := range entries {
		used[e.num] = true
	}
	n := 0
	for used[n] {
		n++
	}
	return n
}

func readBootOrder(efivars string) ([]uint16, error) {
	data, err := readEFIVar(efivars, "BootOrder")
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	order := make([]uint16, 0, len(data)/2)
	for i := 0; i+1 < len(data); i += 2 {
		order = append(order, binary.LittleEndian.Uint16(data[i:]))
	}
	return order, nil
}

// bootFirst puts num first and drops num's old place and every removed entry.
func bootFirst(order []uint16, num int, removed []int) []uint16 {
	drop := map[uint16]bool{uint16(num): true}
	for _, n := range removed {
		drop[uint16(n)] = true
	}
	out := []uint16{uint16(num)}
	for _, n := range order {
		if !drop[n] {
			out = append(out, n)
		}
	}
	return out
}

func encodeBootOrder(order []uint16) []byte {
	b := make([]byte, 2*len(order))
	for i, n := range order {
		binary.LittleEndian.PutUint16(b[2*i:], n)
	}
	return b
}

func efiVarPath(efivars, name string) string {
	return filepath.Join(efivars, name+"-"+efiGlobalVariableGUID)
}

// readEFIVar returns a variable's data without the four attribute bytes
// efivarfs puts in front.
func readEFIVar(efivars, name string) ([]byte, error) {
	b, err := os.ReadFile(efiVarPath(efivars, name))
	if err != nil {
		return nil, err
	}
	if len(b) < 4 {
		return nil, fmt.Errorf("%s: short variable", name)
	}
	return b[4:], nil
}

// writeEFIVar sets a variable. efivarfs takes the attributes and the data in
// one write and marks most existing variables immutable.
func writeEFIVar(efivars, name string, data []byte) error {
	p := efiVarPath(efivars, name)
	// A file system without inode flags (a test's temporary directory) has
	// nothing to clear; the write below reports any real failure.
	_ = clearImmutable(p)
	buf := make([]byte, 4+len(data))
	binary.LittleEndian.PutUint32(buf, efiBootVarAttrs)
	copy(buf[4:], data)
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		return fmt.Errorf("writing %s: %w", name, err)
	}
	if _, err := f.Write(buf); err != nil {
		f.Close()
		return fmt.Errorf("writing %s: %w", name, err)
	}
	// efivarfs replaces the variable on write and has no size to cut. A
	// plain file keeps old bytes past the end without this.
	if fi, err := f.Stat(); err == nil && fi.Size() > int64(len(buf)) {
		_ = f.Truncate(int64(len(buf)))
	}
	return f.Close()
}

func removeEFIVar(efivars, name string) error {
	p := efiVarPath(efivars, name)
	_ = clearImmutable(p)
	if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("removing %s: %w", name, err)
	}
	return nil
}
