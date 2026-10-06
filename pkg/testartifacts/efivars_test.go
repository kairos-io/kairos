package testartifacts_test

import (
	"crypto/x509"
	"encoding/hex"
	"os"
	"path/filepath"

	"github.com/foxboron/go-uefi/efi"
	"github.com/foxboron/go-uefi/efi/attributes"
	"github.com/foxboron/go-uefi/efi/signature"
	"github.com/kairos-io/kairos/v4/pkg/testartifacts"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("EFI signature database variables", func() {
	var dir string

	BeforeEach(func() {
		dir = GinkgoT().TempDir()
		orig := attributes.Efivars
		attributes.Efivars = dir
		DeferCleanup(func() { attributes.Efivars = orig })
	})

	It("names db and dbx after the image security database GUID", func() {
		Expect(testartifacts.SignatureDBVarName("db")).To(Equal("db-" + attributes.EFI_IMAGE_SECURITY_DATABASE_GUID.Format()))
		Expect(testartifacts.SignatureDBVarName("dbx")).To(Equal("dbx-" + attributes.EFI_IMAGE_SECURITY_DATABASE_GUID.Format()))
	})

	It("writes a db that efi.Getdb reads back with the given certificates", func() {
		a, err := testartifacts.NewKeyPair("a")
		Expect(err).ToNot(HaveOccurred())
		b, err := testartifacts.NewKeyPair("b")
		Expect(err).ToNot(HaveOccurred())

		data, err := testartifacts.CertDBVar(a.Cert, b.Cert)
		Expect(err).ToNot(HaveOccurred())
		Expect(os.WriteFile(filepath.Join(dir, testartifacts.SignatureDBVarName("db")), data, 0o644)).To(Succeed())

		db, err := efi.Getdb()
		Expect(err).ToNot(HaveOccurred())
		var got []string
		for _, list := range *db {
			Expect(list.SignatureType).To(Equal(signature.CERT_X509_GUID))
			for _, s := range list.Signatures {
				c, err := x509.ParseCertificate(s.Data)
				Expect(err).ToNot(HaveOccurred())
				got = append(got, c.Subject.CommonName)
			}
		}
		Expect(got).To(ConsistOf("a", "b"))
	})

	It("writes a dbx that efi.Getdbx reads back with the given hashes", func() {
		hash, err := testartifacts.PEHash(testartifacts.MinimalPE(testartifacts.PEOptions{}))
		Expect(err).ToNot(HaveOccurred())

		data, err := testartifacts.HashDBVar(hash)
		Expect(err).ToNot(HaveOccurred())
		Expect(os.WriteFile(filepath.Join(dir, testartifacts.SignatureDBVarName("dbx")), data, 0o644)).To(Succeed())

		dbx, err := efi.Getdbx()
		Expect(err).ToNot(HaveOccurred())
		Expect(*dbx).To(HaveLen(1))
		Expect((*dbx)[0].SignatureType).To(Equal(signature.CERT_SHA256_GUID))
		Expect((*dbx)[0].Signatures).To(HaveLen(1))
		Expect(hex.EncodeToString((*dbx)[0].Signatures[0].Data)).To(Equal(hex.EncodeToString(hash)))
	})

	It("rejects a hash that is not SHA-256 sized", func() {
		_, err := testartifacts.HashDBVar([]byte{1, 2, 3})
		Expect(err).To(HaveOccurred())
	})
})
