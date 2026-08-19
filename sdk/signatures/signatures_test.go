package signatures

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/foxboron/go-uefi/efi/attributes"
	sdkTypes "github.com/kairos-io/kairos-sdk/types/fs"
	sdkLogger "github.com/kairos-io/kairos-sdk/types/logger"
	fsUtils "github.com/kairos-io/kairos-sdk/utils/fs"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/twpayne/go-vfs/v4/vfst"
)

func TestSuite(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Signatures Test Suite")
}

// This tests require prepared files to work unless we prepare them here in the test which is a bit costly
// 2 efi files, one signed and one unsigned
// fbx64.efi -> unsigned
// fbx64.signed.efi -> signed
// 2 db files extracted from a real db, one with the proper certificate that signed the efi file one without it
// db-wrong -> extracted db, contains signatures but they don't have the signature that signed the efi file
// db -> extracted db, contains signatures, including the one that signed the efi file
// 2 dbx files extracted from a real db, one that has nothing on it and one that has the efi file blacklisted
// TODO: have just 1 efi file and generate all of this on the fly:
// sign it when needed
// create the db/dbx efivars on the fly with the proper signatures
// Use efi.EfivarFs for this

var _ = Describe("Uki utils", Label("uki", "utils"), func() {
	var fs sdkTypes.KairosFS
	var logger sdkLogger.KairosLogger
	var memLog *bytes.Buffer
	var cleanup func()

	BeforeEach(func() {
		var err error
		fs, cleanup, err = vfst.NewTestFS(map[string]interface{}{})
		Expect(err).Should(BeNil())
		// create fs with proper setup
		err = fsUtils.MkdirAll(fs, "/sys/firmware/efi/efivars", os.ModeDir|os.ModePerm)
		Expect(err).ToNot(HaveOccurred())
		file, err := os.ReadFile("tests/fbx64.efi")
		Expect(err).ToNot(HaveOccurred())
		err = fs.WriteFile("/efitest.efi", file, os.ModePerm)
		Expect(err).ToNot(HaveOccurred())
		file, err = os.ReadFile("tests/fbx64.signed.efi")
		Expect(err).ToNot(HaveOccurred())
		err = fs.WriteFile("/efitest.signed.efi", file, os.ModePerm)
		Expect(err).ToNot(HaveOccurred())
		memLog = &bytes.Buffer{}
		logger = sdkLogger.NewBufferLogger(memLog)
		// Override the Efivars location to point to our fake ones
		// so the go-uefi lib looks in there
		fakeEfivars, err := fs.RawPath("/sys/firmware/efi/efivars")
		Expect(err).ToNot(HaveOccurred())
		attributes.Efivars = fakeEfivars
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
		dbFile := fmt.Sprintf("db-%s", attributes.EFI_IMAGE_SECURITY_DATABASE_GUID.Format())
		file, err := os.ReadFile("tests/db")
		Expect(err).ToNot(HaveOccurred())
		err = fs.WriteFile(filepath.Join("/sys/firmware/efi/efivars", dbFile), file, os.ModePerm)
		Expect(err).ToNot(HaveOccurred())
		err = CheckArtifactSignatureIsValid(fs, "/efitest.efi", logger)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("no signatures in the file"))
	})

	It("fails when signature doesn't match the db", func() {
		dbFile := fmt.Sprintf("db-%s", attributes.EFI_IMAGE_SECURITY_DATABASE_GUID.Format())
		file, err := os.ReadFile("tests/db-wrong")
		Expect(err).ToNot(HaveOccurred())
		err = fs.WriteFile(filepath.Join("/sys/firmware/efi/efivars", dbFile), file, os.ModePerm)
		Expect(err).ToNot(HaveOccurred())
		err = CheckArtifactSignatureIsValid(fs, "/efitest.signed.efi", logger)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("could not find a signature in EFIVars DB that matches the artifact"))
	})

	It("matches the DB", func() {
		dbFile := fmt.Sprintf("db-%s", attributes.EFI_IMAGE_SECURITY_DATABASE_GUID.Format())
		file, err := os.ReadFile("tests/db")
		Expect(err).ToNot(HaveOccurred())
		err = fs.WriteFile(filepath.Join("/sys/firmware/efi/efivars", dbFile), file, os.ModePerm)
		Expect(err).ToNot(HaveOccurred())
		err = CheckArtifactSignatureIsValid(fs, "/efitest.signed.efi", logger)
		Expect(err).ToNot(HaveOccurred())
	})

	It("doesn't fail when it matches the DB and not DBX", func() {
		dbFile := fmt.Sprintf("db-%s", attributes.EFI_IMAGE_SECURITY_DATABASE_GUID.Format())
		dbxFile := fmt.Sprintf("dbx-%s", attributes.EFI_IMAGE_SECURITY_DATABASE_GUID.Format())
		file, err := os.ReadFile("tests/db")
		Expect(err).ToNot(HaveOccurred())
		err = fs.WriteFile(filepath.Join("/sys/firmware/efi/efivars", dbFile), file, os.ModePerm)
		Expect(err).ToNot(HaveOccurred())
		file, err = os.ReadFile("tests/dbx-wrong")
		Expect(err).ToNot(HaveOccurred())
		err = fs.WriteFile(filepath.Join("/sys/firmware/efi/efivars", dbxFile), file, os.ModePerm)
		Expect(err).ToNot(HaveOccurred())
		err = CheckArtifactSignatureIsValid(fs, "/efitest.signed.efi", logger)
		Expect(err).ToNot(HaveOccurred())
	})

	It("Fails if signature is in DBX, even if its also on DB", func() {
		dbFile := fmt.Sprintf("db-%s", attributes.EFI_IMAGE_SECURITY_DATABASE_GUID.Format())
		dbxFile := fmt.Sprintf("dbx-%s", attributes.EFI_IMAGE_SECURITY_DATABASE_GUID.Format())
		file, err := os.ReadFile("tests/db")
		Expect(err).ToNot(HaveOccurred())
		err = fs.WriteFile(filepath.Join("/sys/firmware/efi/efivars", dbFile), file, os.ModePerm)
		Expect(err).ToNot(HaveOccurred())
		file, err = os.ReadFile("tests/dbx")
		Expect(err).ToNot(HaveOccurred())
		err = fs.WriteFile(filepath.Join("/sys/firmware/efi/efivars", dbxFile), file, os.ModePerm)
		Expect(err).ToNot(HaveOccurred())
		err = CheckArtifactSignatureIsValid(fs, "/efitest.signed.efi", logger)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("hash appears on DBX"))
	})

})
