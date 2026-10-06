package testartifacts

import (
	"bytes"
	"crypto"
	"debug/pe"
	"encoding/binary"
	"fmt"

	"github.com/foxboron/go-uefi/authenticode"
)

// PEOptions tunes the header fields of MinimalPE that code under test reads.
type PEOptions struct {
	// MajorImageVersion is the optional header field systemd-boot uses
	// to carry its major version.
	MajorImageVersion uint16
}

const (
	peFileAlignment    = 0x200
	peSectionAlignment = 0x1000
	peHeaderOffset     = 0x40
)

// MinimalPE returns a 1 KiB PE32+ x86-64 EFI application with a single
// .text section holding one `ret` instruction. It is valid enough for PE
// parsers and Authenticode signing, and is never meant to run.
func MinimalPE(opts PEOptions) []byte {
	b := new(bytes.Buffer)

	dos := make([]byte, peHeaderOffset)
	copy(dos, "MZ")
	binary.LittleEndian.PutUint32(dos[0x3c:], peHeaderOffset)
	b.Write(dos)

	b.WriteString("PE\x00\x00")
	mustWrite(b, pe.FileHeader{
		Machine:              pe.IMAGE_FILE_MACHINE_AMD64,
		NumberOfSections:     1,
		SizeOfOptionalHeader: uint16(binary.Size(pe.OptionalHeader64{})),
		Characteristics:      pe.IMAGE_FILE_EXECUTABLE_IMAGE | pe.IMAGE_FILE_LARGE_ADDRESS_AWARE,
	})
	mustWrite(b, pe.OptionalHeader64{
		Magic:               0x20b,
		SizeOfCode:          peFileAlignment,
		AddressOfEntryPoint: peSectionAlignment,
		BaseOfCode:          peSectionAlignment,
		ImageBase:           0x10000000,
		SectionAlignment:    peSectionAlignment,
		FileAlignment:       peFileAlignment,
		MajorImageVersion:   opts.MajorImageVersion,
		SizeOfImage:         2 * peSectionAlignment,
		SizeOfHeaders:       peFileAlignment,
		Subsystem:           pe.IMAGE_SUBSYSTEM_EFI_APPLICATION,
		NumberOfRvaAndSizes: 16,
	})
	text := pe.SectionHeader32{
		VirtualSize:      1,
		VirtualAddress:   peSectionAlignment,
		SizeOfRawData:    peFileAlignment,
		PointerToRawData: peFileAlignment,
		Characteristics:  pe.IMAGE_SCN_CNT_CODE | pe.IMAGE_SCN_MEM_EXECUTE | pe.IMAGE_SCN_MEM_READ,
	}
	copy(text.Name[:], ".text")
	mustWrite(b, text)

	b.Write(make([]byte, peFileAlignment-b.Len()))
	code := make([]byte, peFileAlignment)
	code[0] = 0xc3
	b.Write(code)
	return b.Bytes()
}

// mustWrite encodes fixed-size header structs into a bytes.Buffer, which
// cannot fail, so an error here is a programming mistake.
func mustWrite(b *bytes.Buffer, v any) {
	if err := binary.Write(b, binary.LittleEndian, v); err != nil {
		panic(err)
	}
}

// SignPE returns a copy of img with an Authenticode signature made with kp
// appended. img itself is left untouched.
func SignPE(img []byte, kp *KeyPair) ([]byte, error) {
	bin, err := authenticode.Parse(bytes.NewReader(bytes.Clone(img)))
	if err != nil {
		return nil, fmt.Errorf("parsing PE: %w", err)
	}
	if _, err := bin.Sign(kp.Key, kp.Cert); err != nil {
		return nil, fmt.Errorf("signing PE: %w", err)
	}
	return bin.Bytes(), nil
}

// PEHash returns the SHA-256 Authenticode hash of img, the value a dbx
// hash entry holds to deny it. Signing does not change it.
func PEHash(img []byte) ([]byte, error) {
	bin, err := authenticode.Parse(bytes.NewReader(img))
	if err != nil {
		return nil, fmt.Errorf("parsing PE: %w", err)
	}
	return bin.Hash(crypto.SHA256), nil
}
