package state

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kairos-io/kairos/v4/pkg/testartifacts"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Extension images built by AuroraBoot in BeforeAll, the same way CI builds
// the ones it bakes onto test ISOs. Reading real images rather than a
// synthetic partition table is the point: a signed work.sysext.raw on a
// GRUB live media is what once broke test-core/bundles.
var (
	// signedWork is verity and signed, as on a UKI live media.
	signedWork string
	// verityOnlyWork has no signature partition, as on a GRUB live media,
	// where nothing enrolls the signing key.
	verityOnlyWork string
	// helloBroke is a plain squashfs with neither verity nor signature.
	helloBroke string
)

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

var _ = Describe("extension images", Ordered, Label("docker"), func() {
	BeforeAll(func() {
		if !testartifacts.DockerAvailable() {
			Fail("these specs build extension images with Docker, and no Docker daemon is reachable")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		dir, err := os.MkdirTemp("", "immucore-sysext-")
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(os.RemoveAll, dir)

		keys := filepath.Join(dir, "keys")
		Expect(testartifacts.GenerateKeySet(ctx, keys)).To(Succeed())
		signedWork, err = testartifacts.BuildSysext(ctx, testartifacts.SysextOptions{
			Dir: filepath.Join(dir, "uki"), Name: "work", Arch: "amd64",
			KeyFile: filepath.Join(keys, "db.key"), CertFile: filepath.Join(keys, "db.pem"),
		})
		Expect(err).ToNot(HaveOccurred())
		verityOnlyWork, err = testartifacts.BuildSysext(ctx, testartifacts.SysextOptions{
			Dir: filepath.Join(dir, "grub"), Name: "work", Arch: "amd64",
		})
		Expect(err).ToNot(HaveOccurred())
		helloBroke, err = testartifacts.BuildPlainSquashfsSysext(ctx, filepath.Join(dir, "uki"), "hello-broke")
		Expect(err).ToNot(HaveOccurred())
	})

	var _ = Describe("detecting a verity signature partition", func() {
		It("finds one in the signed extension the suite ships", func() {
			// work.sysext.raw is verity and signed with a generated key,
			// so it has a root-x86-64-verity-sig partition.
			Expect(carriesVeritySignature(signedWork)).To(BeTrue())
		})

		It("finds none in the unsigned extension the suite ships", func() {
			// hello-broke.sysext.raw is built without verity or signing, and has
			// no partition table at all.
			Expect(carriesVeritySignature(helloBroke)).To(BeFalse())
		})

		It("finds none in the verity-only extension the GRUB media ships", func() {
			// The GRUB image has the same payload and the same verity hash
			// partition as the UKI one, and no root-verity-sig partition. It is
			// the case the synthetic writeGPT images below stand in for, built the way a GRUB
			// live media carries it.
			Expect(carriesVeritySignature(verityOnlyWork)).To(BeFalse())
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
})
