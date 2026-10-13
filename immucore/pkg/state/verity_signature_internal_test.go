package state

import (
	"encoding/binary"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Type GUIDs of the partitions AuroraBoot puts in an x86-64 sysext image.
const (
	rootX86          = "4f68bce3-e8cd-4db1-96e7-fbcaf984b709"
	rootX86Verity    = "2c7357ed-ebd2-46d9-aec1-23d437ec2bf5"
	rootX86VeritySig = "41092b05-9fc8-4523-994f-2def0408b176"
)

// The images reproduce the partition layout AuroraBoot gives a signed
// extension (root, verity, verity signature), a verity-only one, and a plain
// squashfs. The specs only read partition types, and the end-to-end boot
// tests exercise images AuroraBoot really builds.
var (
	// signedWork is verity and signed, as on a UKI live media.
	signedWork string
	// verityOnlyWork has no signature partition, as on a GRUB live media,
	// where nothing enrolls the signing key.
	verityOnlyWork string
	// helloBroke is a plain squashfs with neither verity nor signature.
	helloBroke string
)

// buildExtensionImages writes the three images into a per-spec temp dir.
func buildExtensionImages() {
	dir := GinkgoT().TempDir()

	signedWork = filepath.Join(dir, "work-signed.sysext.raw")
	writeGPT(signedWork, 512, rootX86, rootX86Verity, rootX86VeritySig)

	verityOnlyWork = filepath.Join(dir, "work-verity.sysext.raw")
	writeGPT(verityOnlyWork, 512, rootX86, rootX86Verity)

	helloBroke = filepath.Join(dir, "hello-broke.sysext.raw")
	squashfs := make([]byte, 4096)
	copy(squashfs, "hsqs")
	ExpectWithOffset(1, os.WriteFile(helloBroke, squashfs, 0644)).To(Succeed())
}

// writeGPT builds a disk image whose partition table holds the given type
// GUIDs, so every layout can be covered without committing binary fixtures.
func writeGPT(path string, sectorSize int64, typeGUIDs ...string) {
	const entrySize = 128
	entryLBA := int64(2)

	size := entryLBA*sectorSize + entrySize*int64(len(typeGUIDs))
	image := make([]byte, size)

	copy(image[sectorSize:], []byte(gptSignature))
	binary.LittleEndian.PutUint64(image[sectorSize+72:], uint64(entryLBA))
	binary.LittleEndian.PutUint32(image[sectorSize+80:], uint32(len(typeGUIDs)))
	binary.LittleEndian.PutUint32(image[sectorSize+84:], entrySize)

	for i, guid := range typeGUIDs {
		at := entryLBA*sectorSize + int64(i)*entrySize
		copy(image[at:], encodeGPTTypeGUID(guid))
	}

	ExpectWithOffset(1, os.WriteFile(path, image, 0644)).To(Succeed())
}

// encodeGPTTypeGUID is the inverse of gptTypeGUID, so a round trip through the
// two proves the mixed-endian layout is read the way it is written.
func encodeGPTTypeGUID(guid string) []byte {
	raw, err := hex.DecodeString(strings.ReplaceAll(guid, "-", ""))
	ExpectWithOffset(2, err).ToNot(HaveOccurred())
	ExpectWithOffset(2, raw).To(HaveLen(16))

	b := make([]byte, 16)
	// The first three fields are stored little-endian, the last two as written.
	b[0], b[1], b[2], b[3] = raw[3], raw[2], raw[1], raw[0]
	b[4], b[5] = raw[5], raw[4]
	b[6], b[7] = raw[7], raw[6]
	copy(b[8:], raw[8:])
	return b
}

// These specs build their images with writeGPT and need no Docker.
var _ = Describe("detecting a verity signature partition", func() {
	DescribeTable("recognises the signature partition of every architecture Kairos builds",
		func(guid string) {
			path := filepath.Join(GinkgoT().TempDir(), "ext.sysext.raw")
			writeGPT(path, 512, guid)

			Expect(carriesVeritySignature(path)).To(BeTrue())
		},
		Entry("root-x86-64-verity-sig", "41092b05-9fc8-4523-994f-2def0408b176"),
		Entry("root-arm64-verity-sig", "6db69de6-29f4-4758-a7a5-962190f00ce3"),
		Entry("root-riscv64-verity-sig", "efe0f087-ea8d-4469-821a-4c2a96a8386a"),
		Entry("usr-x86-64-verity-sig", "e7bb33fb-06cf-4e81-8273-e543b413e2e2"),
		Entry("usr-arm64-verity-sig", "c23ce4ff-44bd-4b00-b2d4-b41b3419e02a"),
		Entry("usr-riscv64-verity-sig", "d2f9000a-7a18-453f-b5cd-4d32f77a7b32"),
	)

	It("does not mistake a plain verity partition for a signed one", func() {
		// A verity image with no signature activates by root hash alone, which
		// needs no certificate, so it stays enabled on a GRUB boot.
		path := filepath.Join(GinkgoT().TempDir(), "ext.sysext.raw")
		writeGPT(path, 512,
			"4f68bce3-e8cd-4db1-96e7-fbcaf984b709", // root-x86-64
			"2c7357ed-ebd2-46d9-aec1-23d437ec2bf5", // root-x86-64-verity
		)

		Expect(carriesVeritySignature(path)).To(BeFalse())
	})

	It("reads an image built with a 4096 byte sector", func() {
		path := filepath.Join(GinkgoT().TempDir(), "ext.sysext.raw")
		writeGPT(path, 4096, "41092b05-9fc8-4523-994f-2def0408b176")

		Expect(carriesVeritySignature(path)).To(BeTrue())
	})

	It("reports a file that is not an image as carrying no signature", func() {
		path := filepath.Join(GinkgoT().TempDir(), "notes.txt")
		Expect(os.WriteFile(path, []byte("not an image"), 0644)).To(Succeed())

		Expect(carriesVeritySignature(path)).To(BeFalse())
	})

	It("is an error when the file does not exist", func() {
		_, err := carriesVeritySignature(filepath.Join(GinkgoT().TempDir(), "absent.raw"))
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("skipping an extension this boot cannot verify", func() {
	It("keeps an image it cannot read, rather than disabling a working node", func() {
		Expect(hasUnverifiableSignature(false, filepath.Join(GinkgoT().TempDir(), "absent.raw"))).To(BeFalse())
	})
})

var _ = Describe("detecting a verity signature partition", func() {
	BeforeEach(buildExtensionImages)

	It("finds one in the signed extension the suite ships", func() {
		// signedWork has a root-x86-64-verity-sig partition.
		Expect(carriesVeritySignature(signedWork)).To(BeTrue())
	})

	It("finds none in the unsigned extension the suite ships", func() {
		// helloBroke has no partition table at all.
		Expect(carriesVeritySignature(helloBroke)).To(BeFalse())
	})

	It("finds none in the verity-only extension the GRUB media ships", func() {
		// verityOnlyWork has the same root and verity partitions as signedWork
		// and no root-verity-sig partition, as a GRUB live media carries it.
		Expect(carriesVeritySignature(verityOnlyWork)).To(BeFalse())
	})
})

var _ = Describe("skipping an extension this boot cannot verify", func() {
	BeforeEach(buildExtensionImages)

	// The regression this guards: the image satisfies root=verity+absent, so
	// the policy check keeps it, the boot then fails to set it up with ENOKEY,
	// and a refresh being all or nothing costs the node every other extension.
	// kairos-io/kairos#5004.
	It("skips a signed extension on a GRUB boot, which has no certificates", func() {
		Expect(hasUnverifiableSignature(false, signedWork)).To(BeTrue())
	})

	It("keeps it on a trusted boot, where ExtractCerts populates /run/verity.d", func() {
		Expect(hasUnverifiableSignature(true, signedWork)).To(BeFalse())
	})

	It("keeps an unsigned extension on a GRUB boot", func() {
		Expect(hasUnverifiableSignature(false, helloBroke)).To(BeFalse())
	})

	// Why a verity-only image exists at all: the GRUB live media has to
	// carry an extension this check keeps, or the sweep it covers has nothing
	// left to merge.
	It("keeps the verity-only extension the GRUB media ships", func() {
		Expect(hasUnverifiableSignature(false, verityOnlyWork)).To(BeFalse())
	})
})

var _ = Describe("the check the extension sweep actually runs", func() {
	BeforeEach(buildExtensionImages)

	// passes stands in for systemd-dissect accepting the image. That is the
	// real answer for work.sysext.raw on a GRUB boot: it is verity and signed,
	// and root=verity+absent is an overlap test, so the policy is satisfied.
	passes := func(bool, string) bool { return true }
	rejects := func(bool, string) bool { return false }

	It("skips a signed image a GRUB boot cannot activate, even though it passes the policy", func() {
		Expect(activatableExtensionCheck(false, passes)(signedWork)).To(BeFalse())
	})

	It("keeps that same image on a trusted boot", func() {
		Expect(activatableExtensionCheck(true, passes)(signedWork)).To(BeTrue())
	})

	It("keeps an unsigned image that passes the policy", func() {
		Expect(activatableExtensionCheck(false, passes)(helloBroke)).To(BeTrue())
	})

	It("still skips an image the policy rejects", func() {
		Expect(activatableExtensionCheck(false, rejects)(helloBroke)).To(BeFalse())
	})

	It("does not ask about the signature once the policy has rejected the image", func() {
		asked := false
		Expect(activatableExtensionCheck(false, func(bool, string) bool {
			asked = true
			return false
		})(signedWork)).To(BeFalse())
		Expect(asked).To(BeTrue())
	})
})
