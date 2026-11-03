package secureboot

import (
	"bytes"
	"crypto/sha256"
	"os"
	"path/filepath"

	"github.com/foxboron/go-uefi/efi/signature"
	efiutil "github.com/foxboron/go-uefi/efi/util"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// firmwareExportWith writes db and KEK signature databases the way they are
// exported from UEFI firmware, which is the layout prepareCustomDerDir reads.
func firmwareExportWith(db, kek *signature.SignatureDatabase) string {
	GinkgoHelper()

	dir := GinkgoT().TempDir()
	writeFile(filepath.Join(dir, "db"), db.Bytes())
	writeFile(filepath.Join(dir, "KEK"), kek.Bytes())

	return dir
}

// countTypeInEsl reports how many entries of this signature type in the
// signature database at path carry exactly these bytes.
func countTypeInEsl(path string, certType efiutil.EFIGUID, data []byte) int {
	GinkgoHelper()

	b, err := os.ReadFile(path)
	Expect(err).ToNot(HaveOccurred(), "read %s", path)

	sigdb, err := signature.ReadSignatureDatabase(bytes.NewReader(b))
	Expect(err).ToNot(HaveOccurred(), "parse %s", path)

	n := 0
	for _, sl := range sigdb {
		if !efiutil.CmpEFIGUID(sl.SignatureType, certType) {
			continue
		}
		for _, s := range sl.Signatures {
			if bytes.Equal(s.Data, data) {
				n++
			}
		}
	}

	return n
}

var _ = Describe("custom cert dir with SHA256 entries", func() {
	It("carries a firmware SHA256 digest into the generated db", func() {
		dir := GinkgoT().TempDir()
		writeKeyPair(dir, "PK")
		writeKeyPair(dir, "KEK")
		own := writeKeyPair(dir, "db")

		vendorOwner := *efiutil.StringToGUID("11111111-2222-3333-4444-555555555555")
		vendor := writeKeyPair(GinkgoT().TempDir(), "vendor")
		digest := sha256.Sum256([]byte("vendor option rom"))

		db := signature.NewSignatureDatabase()
		Expect(db.Append(signature.CERT_X509_GUID, vendorOwner, vendor.Raw)).To(Succeed())
		Expect(db.Append(signature.CERT_SHA256_GUID, vendorOwner, digest[:])).To(Succeed())
		kek := signature.NewSignatureDatabase()
		Expect(kek.Append(signature.CERT_X509_GUID, vendorOwner, vendor.Raw)).To(Succeed())

		customDir, err := prepareCustomDerDir(nullLogger(), firmwareExportWith(db, kek))
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(os.RemoveAll, customDir)

		guid := *efiutil.StringToGUID(testOwnerGUID)
		Expect(generateAuthKeys(nullLogger(), guid, dir, "db", customDir, true)).To(Succeed())

		esl := filepath.Join(dir, "db.esl")
		Expect(countTypeInEsl(esl, signature.CERT_X509_GUID, own.Raw)).To(Equal(1),
			"db.esl: our own cert should appear exactly once")
		Expect(countTypeInEsl(esl, signature.CERT_X509_GUID, vendor.Raw)).To(Equal(1),
			"db.esl: the vendor cert should appear exactly once")
		Expect(countTypeInEsl(esl, signature.CERT_SHA256_GUID, digest[:])).To(Equal(1),
			"db.esl: the vendor SHA256 digest should appear exactly once")
	})
})
