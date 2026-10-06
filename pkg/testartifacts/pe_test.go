package testartifacts_test

import (
	"bytes"
	"debug/pe"

	"github.com/foxboron/go-uefi/authenticode"
	"github.com/foxboron/go-uefi/pkcs7"
	"github.com/kairos-io/kairos/v4/pkg/testartifacts"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	peparser "github.com/saferwall/pe"
)

var _ = Describe("PE artifacts", func() {
	var kp *testartifacts.KeyPair

	BeforeEach(func() {
		var err error
		kp, err = testartifacts.NewKeyPair("testartifacts signer")
		Expect(err).ToNot(HaveOccurred())
	})

	It("creates a key pair whose certificate carries the common name and matches the key", func() {
		Expect(kp.Cert.Subject.CommonName).To(Equal("testartifacts signer"))
		Expect(kp.Cert.PublicKey).To(Equal(kp.Key.Public()))
	})

	It("builds a PE32+ EFI application that debug/pe and saferwall parse", func() {
		img := testartifacts.MinimalPE(testartifacts.PEOptions{MajorImageVersion: 259})

		f, err := pe.NewFile(bytes.NewReader(img))
		Expect(err).ToNot(HaveOccurred())
		opt, ok := f.OptionalHeader.(*pe.OptionalHeader64)
		Expect(ok).To(BeTrue())
		Expect(opt.Subsystem).To(Equal(uint16(pe.IMAGE_SUBSYSTEM_EFI_APPLICATION)))
		Expect(opt.MajorImageVersion).To(Equal(uint16(259)))

		p, err := peparser.NewBytes(img, &peparser.Options{Fast: true})
		Expect(err).ToNot(HaveOccurred())
		Expect(p.Parse()).To(Succeed())
	})

	It("signs a PE so that the signing certificate verifies it and another does not", func() {
		img := testartifacts.MinimalPE(testartifacts.PEOptions{})
		signed, err := testartifacts.SignPE(img, kp)
		Expect(err).ToNot(HaveOccurred())

		bin, err := authenticode.Parse(bytes.NewReader(signed))
		Expect(err).ToNot(HaveOccurred())
		sigs, err := bin.Signatures()
		Expect(err).ToNot(HaveOccurred())
		Expect(sigs).To(HaveLen(1))
		p7, err := pkcs7.ParsePKCS7(sigs[0].Certificate)
		Expect(err).ToNot(HaveOccurred())

		ok, _ := p7.Verify(kp.Cert)
		Expect(ok).To(BeTrue())

		other, err := testartifacts.NewKeyPair("unrelated")
		Expect(err).ToNot(HaveOccurred())
		ok, _ = p7.Verify(other.Cert)
		Expect(ok).To(BeFalse())
	})

	It("does not modify the input when signing", func() {
		img := testartifacts.MinimalPE(testartifacts.PEOptions{})
		orig := bytes.Clone(img)
		_, err := testartifacts.SignPE(img, kp)
		Expect(err).ToNot(HaveOccurred())
		Expect(img).To(Equal(orig))
	})

	It("returns the Authenticode hash saferwall computes for the signed binary", func() {
		img := testartifacts.MinimalPE(testartifacts.PEOptions{})
		signed, err := testartifacts.SignPE(img, kp)
		Expect(err).ToNot(HaveOccurred())

		hash, err := testartifacts.PEHash(img)
		Expect(err).ToNot(HaveOccurred())
		Expect(hash).To(HaveLen(32))

		p, err := peparser.NewBytes(signed, &peparser.Options{Fast: true})
		Expect(err).ToNot(HaveOccurred())
		Expect(p.Parse()).To(Succeed())
		Expect(p.Authentihash()).To(Equal(hash))
	})
})
