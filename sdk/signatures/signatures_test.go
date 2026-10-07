package signatures

import (
	"bytes"
	"crypto/sha256"
	"os"
	"path/filepath"
	"testing"

	"github.com/foxboron/go-uefi/efi/attributes"
	"github.com/kairos-io/kairos/v4/internal/testartifacts"
	sdkTypes "github.com/kairos-io/kairos/v4/sdk/types/fs"
	sdkLogger "github.com/kairos-io/kairos/v4/sdk/types/logger"
	fsUtils "github.com/kairos-io/kairos/v4/sdk/utils/fs"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/twpayne/go-vfs/v4/vfst"
)

func TestSuite(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Signatures Test Suite")
}

// The fixtures are generated per spec with testartifacts: one unsigned PE, the
// same PE signed by a generated key, db variables with and without that key's
// certificate, and dbx variables with and without the PE's hash.

var _ = Describe("Uki utils", Label("uki", "utils"), func() {
	var fs sdkTypes.KairosFS
	var logger sdkLogger.KairosLogger
	var memLog *bytes.Buffer
	var cleanup func()
	var signer, other *testartifacts.KeyPair
	var dbVar, dbWrongVar, dbxVar, dbxWrongVar []byte

	writeVar := func(name string, data []byte) {
		Expect(fs.WriteFile(filepath.Join("/sys/firmware/efi/efivars", testartifacts.SignatureDBVarName(name)), data, os.ModePerm)).To(Succeed())
	}

	BeforeEach(func() {
		var err error
		fs, cleanup, err = vfst.NewTestFS(map[string]interface{}{})
		Expect(err).Should(BeNil())
		// create fs with proper setup
		err = fsUtils.MkdirAll(fs, "/sys/firmware/efi/efivars", os.ModeDir|os.ModePerm)
		Expect(err).ToNot(HaveOccurred())
		memLog = &bytes.Buffer{}
		logger = sdkLogger.NewBufferLogger(memLog)
		// Override the Efivars location to point to our fake ones
		// so the go-uefi lib looks in there
		fakeEfivars, err := fs.RawPath("/sys/firmware/efi/efivars")
		Expect(err).ToNot(HaveOccurred())
		attributes.Efivars = fakeEfivars

		signer, err = testartifacts.NewKeyPair("signatures test signer")
		Expect(err).ToNot(HaveOccurred())
		other, err = testartifacts.NewKeyPair("signatures test unrelated")
		Expect(err).ToNot(HaveOccurred())

		unsigned := testartifacts.MinimalPE(testartifacts.PEOptions{})
		signed, err := testartifacts.SignPE(unsigned, signer)
		Expect(err).ToNot(HaveOccurred())
		Expect(fs.WriteFile("/efitest.efi", unsigned, os.ModePerm)).To(Succeed())
		Expect(fs.WriteFile("/efitest.signed.efi", signed, os.ModePerm)).To(Succeed())

		dbVar, err = testartifacts.CertDBVar(signer.Cert, other.Cert)
		Expect(err).ToNot(HaveOccurred())
		dbWrongVar, err = testartifacts.CertDBVar(other.Cert)
		Expect(err).ToNot(HaveOccurred())
		hash, err := testartifacts.PEHash(unsigned)
		Expect(err).ToNot(HaveOccurred())
		dbxVar, err = testartifacts.HashDBVar(hash)
		Expect(err).ToNot(HaveOccurred())
		unrelated := sha256.Sum256([]byte("unrelated"))
		dbxWrongVar, err = testartifacts.HashDBVar(unrelated[:])
		Expect(err).ToNot(HaveOccurred())
	})
	AfterEach(func() {
		cleanup()
	})
	It("Fails if it cant find the file to check", func() {
		err := CheckArtifactSignatureIsValid(fs, "/notexists.efi", logger)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("does not exist"))
	})

	It("Fails if the file is empty", func() {
		// File needs to not be empty for the parser to try to parse it
		err := fs.WriteFile("/nonefi.file", []byte(""), os.ModePerm)
		Expect(err).ToNot(HaveOccurred())
		err = CheckArtifactSignatureIsValid(fs, "/nonefi.file", logger)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("has zero size"))
	})

	It("Fails if the file is not a valid efi file", func() {
		// File needs to not be empty for the parser to try to parse it
		err := fs.WriteFile("/nonefi.file", []byte("asdkljhfjklahsdfjk,hbasdfjkhas"), os.ModePerm)
		Expect(err).ToNot(HaveOccurred())
		err = CheckArtifactSignatureIsValid(fs, "/nonefi.file", logger)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("not a PE file"))
	})

	It("Fails if the file to check has no signatures", func() {
		writeVar("db", dbVar)
		err := CheckArtifactSignatureIsValid(fs, "/efitest.efi", logger)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("no signatures in the file"))
	})

	It("fails when signature doesn't match the db", func() {
		writeVar("db", dbWrongVar)
		err := CheckArtifactSignatureIsValid(fs, "/efitest.signed.efi", logger)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("could not find a signature in EFIVars DB that matches the artifact"))
	})

	It("matches the DB", func() {
		writeVar("db", dbVar)
		err := CheckArtifactSignatureIsValid(fs, "/efitest.signed.efi", logger)
		Expect(err).ToNot(HaveOccurred())
	})

	It("doesn't fail when it matches the DB and not DBX", func() {
		writeVar("db", dbVar)
		writeVar("dbx", dbxWrongVar)
		err := CheckArtifactSignatureIsValid(fs, "/efitest.signed.efi", logger)
		Expect(err).ToNot(HaveOccurred())
	})

	It("Fails if signature is in DBX, even if its also on DB", func() {
		writeVar("db", dbVar)
		writeVar("dbx", dbxVar)
		err := CheckArtifactSignatureIsValid(fs, "/efitest.signed.efi", logger)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("hash appears on DBX"))
	})

})
