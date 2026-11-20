package secureboot

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"time"

	"github.com/foxboron/go-uefi/efi/signature"
	efiutil "github.com/foxboron/go-uefi/efi/util"
	"github.com/foxboron/sbctl/certs"
	"github.com/kairos-io/kairos/v4/sdk/types/logger"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// testOwnerGUID is the owner GUID generateAuthKeys tags our own certificates
// with; any value works, the dedup must not depend on it.
const testOwnerGUID = "88a69775-5ad7-45d9-9f34-cec43e1f1989"

// writeKeyPair generates an RSA key and a self-signed certificate and writes
// them as <name>.key (PKCS8 PEM) and <name>.pem, which is the layout
// generateAuthKeys reads. Done in Go rather than by shelling out so the spec
// does not need openssl on PATH.
func writeKeyPair(dir, name string) *x509.Certificate {
	GinkgoHelper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	Expect(err).ToNot(HaveOccurred(), "generate %s key", name)

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: "auroraboot-test-" + name},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	Expect(err).ToNot(HaveOccurred(), "create %s cert", name)

	pkcs8, err := x509.MarshalPKCS8PrivateKey(key)
	Expect(err).ToNot(HaveOccurred(), "marshal %s key", name)

	writeFile(filepath.Join(dir, name+".key"), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8}))
	writeFile(filepath.Join(dir, name+".pem"), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))

	cert, err := x509.ParseCertificate(der)
	Expect(err).ToNot(HaveOccurred(), "parse %s cert", name)

	return cert
}

func writeFile(path string, data []byte) {
	GinkgoHelper()
	Expect(os.WriteFile(path, data, 0o600)).To(Succeed(), "write %s", path)
}

// oemCertDERs returns the DER bytes of the Microsoft certs sbctl bundles for a
// variable. These are the certs GenerateKeySet adds unless SkipMicrosoftCerts.
func oemCertDERs(variable string) [][]byte {
	GinkgoHelper()

	sigdb, err := certs.GetOEMCerts("microsoft", variable)
	Expect(err).ToNot(HaveOccurred(), "GetOEMCerts(%s)", variable)

	var out [][]byte
	for _, sl := range *sigdb {
		for _, s := range sl.Signatures {
			out = append(out, bytes.Clone(s.Data))
		}
	}
	Expect(out).ToNot(BeEmpty(), "sbctl bundles no microsoft certs for %s", variable)

	return out
}

// customDirWith builds the prepared-DER directory layout that
// generateAuthKeys hands to certs.GetCustomCerts: <dir>/custom/<variable>/*.
// This stands in for a user's UEFI export that has been run through
// prepareCustomDerDir.
func customDirWith(variable string, ders [][]byte) string {
	GinkgoHelper()

	dir := GinkgoT().TempDir()
	keyDir := filepath.Join(dir, "custom", variable)
	Expect(os.MkdirAll(keyDir, 0o755)).To(Succeed(), "mkdir %s", keyDir)

	for i, der := range ders {
		writeFile(filepath.Join(keyDir, variable+"-custom-"+big.NewInt(int64(i)).String()), der)
	}

	return dir
}

// countInEsl reports how many entries of the signature database in path carry
// exactly these certificate bytes, across every signature list and regardless
// of the owner GUID recorded for the entry.
func countInEsl(path string, der []byte) int {
	GinkgoHelper()

	b, err := os.ReadFile(path)
	Expect(err).ToNot(HaveOccurred(), "read %s", path)

	sigdb, err := signature.ReadSignatureDatabase(bytes.NewReader(b))
	Expect(err).ToNot(HaveOccurred(), "parse %s", path)

	n := 0
	for _, sl := range sigdb {
		if !efiutil.CmpEFIGUID(sl.SignatureType, signature.CERT_X509_GUID) {
			continue
		}
		for _, s := range sl.Signatures {
			if bytes.Equal(s.Data, der) {
				n++
			}
		}
	}

	return n
}

var _ = Describe("generateAuthKeys", func() {
	var dir string
	var guid efiutil.EFIGUID

	BeforeEach(func() {
		dir = GinkgoT().TempDir()
		guid = *efiutil.StringToGUID(testOwnerGUID)
	})

	// Covers kairos-io/kairos#2432.
	//
	// A user who exports KEK/db from their firmware and passes the export
	// through --custom-cert-dir, without also passing --skip-microsoft-certs,
	// hands us the Microsoft certs twice: once from sbctl's bundled copy and
	// once from their own export. The two arrive under different owner GUIDs
	// (certs.GetOEMCerts tags them "microsoft", certs.GetCustomCerts tags
	// everything "custom"), so the library's own owner+data dedup cannot see
	// it, and SignatureDatabase.AppendDatabase does no membership check at
	// all. The cert must still land in the enrolled database exactly once.
	DescribeTable("skips certificates already enrolled", func(variable string) {
		writeKeyPair(dir, "PK")
		signerFor := map[string]string{"KEK": "PK", "db": "KEK"}
		if signerFor[variable] == "KEK" {
			writeKeyPair(dir, "KEK")
		}
		own := writeKeyPair(dir, variable)

		msCerts := oemCertDERs(variable)
		customDir := customDirWith(variable, msCerts)

		Expect(generateAuthKeys(nullLogger(), guid, dir, variable, customDir, false)).To(Succeed())

		esl := filepath.Join(dir, variable+".esl")
		for i, der := range msCerts {
			Expect(countInEsl(esl, der)).To(Equal(1),
				"%s.esl: microsoft cert %d should appear exactly once", variable, i)
		}
		Expect(countInEsl(esl, own.Raw)).To(Equal(1),
			"%s.esl: our own cert should appear exactly once", variable)
	},
		Entry("KEK", "KEK"),
		Entry("db", "db"),
	)

	// The other half: a custom cert that is NOT already in the database still
	// has to be enrolled. Without this, "skip duplicates" could be satisfied
	// by dropping custom certs wholesale.
	It("keeps a custom certificate that is not already enrolled", func() {
		writeKeyPair(dir, "PK")
		own := writeKeyPair(dir, "KEK")

		vendor := writeKeyPair(GinkgoT().TempDir(), "vendor")
		customDir := customDirWith("KEK", [][]byte{vendor.Raw})

		Expect(generateAuthKeys(nullLogger(), guid, dir, "KEK", customDir, true)).To(Succeed())

		esl := filepath.Join(dir, "KEK.esl")
		Expect(countInEsl(esl, vendor.Raw)).To(Equal(1),
			"KEK.esl: the distinct custom cert should appear exactly once")
		Expect(countInEsl(esl, own.Raw)).To(Equal(1),
			"KEK.esl: our own cert should appear exactly once")
	})

	// Guards the common path: with no --custom-cert-dir there is nothing to
	// deduplicate, and every bundled Microsoft cert must still be enrolled
	// exactly once.
	It("enrolls every bundled microsoft certificate when there is no custom cert dir", func() {
		writeKeyPair(dir, "PK")
		own := writeKeyPair(dir, "KEK")

		Expect(generateAuthKeys(nullLogger(), guid, dir, "KEK", "", false)).To(Succeed())

		esl := filepath.Join(dir, "KEK.esl")
		for i, der := range oemCertDERs("KEK") {
			Expect(countInEsl(esl, der)).To(Equal(1),
				"KEK.esl: microsoft cert %d should appear exactly once", i)
		}
		Expect(countInEsl(esl, own.Raw)).To(Equal(1),
			"KEK.esl: our own cert should appear exactly once")
	})
})

var _ = Describe("appendDatabaseSkippingDuplicates", func() {
	// Pins the no-regression promise: when the merge finds nothing to skip, it
	// must serialize to exactly the bytes SignatureDatabase.AppendDatabase
	// produced before. That is what makes this safe for everyone not hitting
	// #2432, and it is also what proves the recomputed ListSize is right.
	DescribeTable("is byte-identical to a plain append when nothing is dropped", func(variable string) {
		oem, err := certs.GetOEMCerts("microsoft", variable)
		Expect(err).ToNot(HaveOccurred(), "GetOEMCerts(%s)", variable)

		want := signature.NewSignatureDatabase()
		want.AppendDatabase(oem)

		got := signature.NewSignatureDatabase()
		appendDatabaseSkippingDuplicates(got, oem, nullLogger(), variable, "Microsoft")

		Expect(got.Bytes()).To(Equal(want.Bytes()),
			"%s: the deduplicating merge changed the bytes of a merge with no duplicates", variable)
	},
		Entry("KEK", "KEK"),
		Entry("db", "db"),
	)
})

// nullLogger returns a logger that discards output, so the skip messages do
// not drown the spec report.
func nullLogger() logger.KairosLogger {
	return logger.NewNullLogger()
}
