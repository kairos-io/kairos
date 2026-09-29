package state

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"

	internalUtils "github.com/kairos-io/kairos/v4/immucore/internal/utils"
)

// gptSignature is the magic at the start of a GPT header. The header lives on
// LBA 1, so the offset it is found at also tells us the logical sector size.
const gptSignature = "EFI PART"

// gptHeaderSize is the part of the GPT header this code reads: up to and
// including the partition entry size at offset 84.
const gptHeaderSize = 92

// gptSectorSizes are the logical sector sizes a disk image can use. A DDI
// built on a 4K native host puts its GPT header at 4096 rather than 512.
var gptSectorSizes = []int64{512, 4096}

// maxGPTEntries bounds how much of a partition table is worth walking. The UEFI
// spec requires firmware to handle at least 128 entries and no DDI comes near
// that, so anything past this is a corrupt header rather than a real table.
const maxGPTEntries = 1024

// veritySignatureTypes holds the GPT partition type GUIDs systemd gives a
// verity signature partition, for the architectures Kairos builds. The values
// come from SD_GPT_{ROOT,USR}_{X86_64,ARM64,RISCV64}_VERITY_SIG in systemd's
// src/systemd/sd-gpt.h.
//
// A DDI that carries one of these asks the kernel to check its verity root
// hash against a certificate, instead of trusting the hash as given.
var veritySignatureTypes = map[string]bool{
	"41092b05-9fc8-4523-994f-2def0408b176": true, // root-x86-64-verity-sig
	"6db69de6-29f4-4758-a7a5-962190f00ce3": true, // root-arm64-verity-sig
	"efe0f087-ea8d-4469-821a-4c2a96a8386a": true, // root-riscv64-verity-sig
	"e7bb33fb-06cf-4e81-8273-e543b413e2e2": true, // usr-x86-64-verity-sig
	"c23ce4ff-44bd-4b00-b2d4-b41b3419e02a": true, // usr-arm64-verity-sig
	"d2f9000a-7a18-453f-b5cd-4d32f77a7b32": true, // usr-riscv64-verity-sig
}

// hasUnverifiableSignature reports whether path carries a verity signature
// that this boot has no certificate to check.
//
// The certificates live in /run/verity.d, and they are written by ExtractCerts
// (immucore/pkg/state/steps_uki.go) out of the Secure Boot EFI variables.
// Only the UKI boot graph registers that step (immucore/pkg/dag/dag_uki_boot.go),
// so on a GRUB boot the directory is never populated and the kernel has nothing
// to check a signature against. dm-verity then refuses the activation:
//
//	device-mapper: table: 252:0: verity: Root hash verification failed (-ENOKEY)
//
// The image policy does not catch this on its own, because it is an overlap
// test: an image that is verity *and* signed satisfies root=verity+absent, so
// it is enabled, and systemd-sysext then fails to set it up. A refresh is all
// or nothing, so that single image stops every other extension from merging.
// See kairos-io/kairos#5004.
//
// An image this cannot read is reported as fine. Refusing on a read error
// would disable every extension on a working node, which is the same trade
// unvalidatedExtensionCheck makes on the non-UKI path.
func hasUnverifiableSignature(isUKI bool, path string) bool {
	if isUKI {
		return false
	}

	signed, err := carriesVeritySignature(path)
	if err != nil {
		internalUtils.KLog.Logger.Debug().Err(err).Str("src", path).
			Msg("Could not read the partition table of an extension, leaving it enabled")
		return false
	}
	return signed
}

// carriesVeritySignature reports whether the disk image at path has a verity
// signature partition in its GPT.
//
// An image with no GPT at all carries no signature. Whether such a file is
// usable as an extension is the image policy's question, not this one.
func carriesVeritySignature(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()

	for _, sectorSize := range gptSectorSizes {
		header := make([]byte, gptHeaderSize)
		if _, err := f.ReadAt(header, sectorSize); err != nil {
			// Too small to hold a GPT at this sector size.
			continue
		}
		if string(header[0:8]) != gptSignature {
			continue
		}

		entryLBA := int64(binary.LittleEndian.Uint64(header[72:80]))
		entryCount := int64(binary.LittleEndian.Uint32(header[80:84]))
		entrySize := int64(binary.LittleEndian.Uint32(header[84:88]))
		if entrySize < 16 || entryCount < 1 || entryCount > maxGPTEntries {
			return false, fmt.Errorf("%s: unusable GPT partition table, %d entries of %d bytes", path, entryCount, entrySize)
		}

		entry := make([]byte, entrySize)
		for i := int64(0); i < entryCount; i++ {
			if _, err := f.ReadAt(entry, entryLBA*sectorSize+i*entrySize); err != nil {
				if err == io.EOF || err == io.ErrUnexpectedEOF {
					// The table is declared larger than the image. What is
					// there has been read, so answer from that.
					break
				}
				return false, err
			}
			if veritySignatureTypes[gptTypeGUID(entry[0:16])] {
				return true, nil
			}
		}
		return false, nil
	}

	return false, nil
}

// gptTypeGUID renders a partition type GUID as it is written down. GPT stores
// the first three fields little-endian and the last two big-endian, so this
// cannot be a straight hex dump of the 16 bytes.
func gptTypeGUID(b []byte) string {
	return fmt.Sprintf("%02x%02x%02x%02x-%02x%02x-%02x%02x-%02x%02x-%02x%02x%02x%02x%02x%02x",
		b[3], b[2], b[1], b[0],
		b[5], b[4],
		b[7], b[6],
		b[8], b[9],
		b[10], b[11], b[12], b[13], b[14], b[15])
}
