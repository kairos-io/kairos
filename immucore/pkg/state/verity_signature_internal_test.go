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

// assets are the real extension images the test suite ships and the CI job
// bakes onto the live media. Reading them rather than a synthetic image is the
// point: work.sysext.raw is what reddened test-core/bundles.
const assets = "../../../tests/assets/sysext"

// writeGPT builds a disk image whose partition table holds the given type
// GUIDs, so the arch and sector-size cases can be covered without carrying six
// more multi-megabyte fixtures in the repo.
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

var _ = Describe("detecting a verity signature partition", func() {
	It("finds one in the signed extension the suite ships", func() {
		// work.sysext.raw is verity and signed with tests/assets/keys/db.key,
		// so it has a root-x86-64-verity-sig partition.
		Expect(carriesVeritySignature(filepath.Join(assets, "work.sysext.raw"))).To(BeTrue())
	})

	It("finds none in the unsigned extension the suite ships", func() {
		// hello-broke.sysext.raw was built without verity or signing, and has
		// no partition table at all.
		Expect(carriesVeritySignature(filepath.Join(assets, "hello-broke.sysext.raw"))).To(BeFalse())
	})

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
	signed := filepath.Join(assets, "work.sysext.raw")

	// The regression this guards: the image satisfies root=verity+absent, so
	// the policy check keeps it, the boot then fails to set it up with ENOKEY,
	// and a refresh being all or nothing costs the node every other extension.
	// kairos-io/kairos#5004.
	It("skips a signed extension on a GRUB boot, which has no certificates", func() {
		Expect(hasUnverifiableSignature(false, signed)).To(BeTrue())
	})

	It("keeps it on a trusted boot, where ExtractCerts populates /run/verity.d", func() {
		Expect(hasUnverifiableSignature(true, signed)).To(BeFalse())
	})

	It("keeps an unsigned extension on a GRUB boot", func() {
		Expect(hasUnverifiableSignature(false, filepath.Join(assets, "hello-broke.sysext.raw"))).To(BeFalse())
	})

	It("keeps an image it cannot read, rather than disabling a working node", func() {
		Expect(hasUnverifiableSignature(false, filepath.Join(GinkgoT().TempDir(), "absent.raw"))).To(BeFalse())
	})
})

var _ = Describe("the check the extension sweep actually runs", func() {
	// passes stands in for systemd-dissect accepting the image. That is the
	// real answer for work.sysext.raw on a GRUB boot: it is verity and signed,
	// and root=verity+absent is an overlap test, so the policy is satisfied.
	passes := func(bool, string) bool { return true }
	rejects := func(bool, string) bool { return false }

	signed := filepath.Join(assets, "work.sysext.raw")
	unsigned := filepath.Join(assets, "hello-broke.sysext.raw")

	It("skips a signed image a GRUB boot cannot activate, even though it passes the policy", func() {
		Expect(activatableExtensionCheck(false, passes)(signed)).To(BeFalse())
	})

	It("keeps that same image on a trusted boot", func() {
		Expect(activatableExtensionCheck(true, passes)(signed)).To(BeTrue())
	})

	It("keeps an unsigned image that passes the policy", func() {
		Expect(activatableExtensionCheck(false, passes)(unsigned)).To(BeTrue())
	})

	It("still skips an image the policy rejects", func() {
		Expect(activatableExtensionCheck(false, rejects)(unsigned)).To(BeFalse())
	})

	It("does not ask about the signature once the policy has rejected the image", func() {
		asked := false
		Expect(activatableExtensionCheck(false, func(bool, string) bool {
			asked = true
			return false
		})(signed)).To(BeFalse())
		Expect(asked).To(BeTrue())
	})
})
