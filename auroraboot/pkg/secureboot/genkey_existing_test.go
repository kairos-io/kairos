package secureboot

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/foxboron/go-uefi/efi/signature"
	efiutil "github.com/foxboron/go-uefi/efi/util"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("GenerateKeySet with ExistingKeysDir", func() {
	var src, out string
	var srcCerts map[string]*x509.Certificate

	BeforeEach(func() {
		src = GinkgoT().TempDir()
		out = filepath.Join(GinkgoT().TempDir(), "keys")
		srcCerts = map[string]*x509.Certificate{}
		for _, kt := range []string{"PK", "KEK", "db"} {
			srcCerts[kt] = writeKeyPair(src, kt)
		}
	})

	run := func(o Options) error {
		l := nullLogger()
		o.Logger = &l
		o.SkipMicrosoftCerts = true
		return GenerateKeySet(o)
	}

	It("builds the set from the given key pairs without a name", func() {
		Expect(run(Options{OutputDir: out, ExistingKeysDir: src})).To(Succeed())

		for _, kt := range []string{"PK", "KEK", "db"} {
			for _, ext := range []string{".key", ".pem", ".der", ".auth", ".esl"} {
				info, err := os.Stat(filepath.Join(out, kt+ext))
				Expect(err).ToNot(HaveOccurred())
				Expect(info.Size()).ToNot(BeZero(), "%s%s is empty", kt, ext)
			}
			for _, ext := range []string{".key", ".pem"} {
				want, err := os.ReadFile(filepath.Join(src, kt+ext))
				Expect(err).ToNot(HaveOccurred())
				got, err := os.ReadFile(filepath.Join(out, kt+ext))
				Expect(err).ToNot(HaveOccurred())
				Expect(got).To(Equal(want), "%s%s was not copied as is", kt, ext)
			}
			info, err := os.Stat(filepath.Join(out, kt+".key"))
			Expect(err).ToNot(HaveOccurred())
			Expect(info.Mode().Perm()).To(Equal(os.FileMode(0o600)), "%s.key mode", kt)

			der, err := os.ReadFile(filepath.Join(out, kt+".der"))
			Expect(err).ToNot(HaveOccurred())
			Expect(der).To(Equal(srcCerts[kt].Raw), "%s.der is not the given certificate", kt)
		}
		Expect(filepath.Join(out, "tpm2-pcr-private.pem")).To(BeAnExistingFile())

		signerFor := map[string]string{"PK": "PK", "KEK": "PK", "db": "KEK"}
		for kt, signer := range signerFor {
			b, err := os.ReadFile(filepath.Join(out, kt+".auth"))
			Expect(err).ToNot(HaveOccurred())
			var auth signature.EFIVariableAuthentication2
			Expect(auth.Unmarshal(bytes.NewBuffer(b))).To(Succeed(), "unmarshal %s.auth", kt)
			ok, err := auth.Verify(srcCerts[signer])
			Expect(err).ToNot(HaveOccurred(), "verify %s.auth", kt)
			Expect(ok).To(BeTrue(), "%s.auth is not signed by the given %s", kt, signer)
		}
	})

	It("still requires a name when generating keys", func() {
		Expect(run(Options{OutputDir: out})).To(MatchError(ContainSubstring("name is required")))
	})

	It("names the missing file and writes nothing", func() {
		Expect(os.Remove(filepath.Join(src, "db.pem"))).To(Succeed())

		err := run(Options{OutputDir: out, ExistingKeysDir: src})
		Expect(err).To(MatchError(ContainSubstring("missing db.pem")))
		Expect(filepath.Join(out, "PK.key")).ToNot(BeAnExistingFile())
	})

	It("rejects a key that does not match its certificate", func() {
		other := GinkgoT().TempDir()
		writeKeyPair(other, "KEK")
		b, err := os.ReadFile(filepath.Join(other, "KEK.pem"))
		Expect(err).ToNot(HaveOccurred())
		writeFile(filepath.Join(src, "KEK.pem"), b)

		err = run(Options{OutputDir: out, ExistingKeysDir: src})
		Expect(err).To(MatchError(ContainSubstring("KEK.key does not match the public key in KEK.pem")))
	})

	It("rejects a PKCS#8 encrypted key", func() {
		key := filepath.Join(src, "PK.key")
		enc := filepath.Join(src, "PK.key.enc")
		cmd := exec.Command("openssl", "pkcs8", "-topk8", "-v2", "aes-256-cbc",
			"-passout", "pass:test", "-in", key, "-out", enc)
		o, err := cmd.CombinedOutput()
		Expect(err).ToNot(HaveOccurred(), string(o))
		Expect(os.Rename(enc, key)).To(Succeed())

		err = run(Options{OutputDir: out, ExistingKeysDir: src})
		Expect(err).To(MatchError(ContainSubstring("PK.key is encrypted")))
	})

	It("rejects a legacy encrypted key", func() {
		writeFile(filepath.Join(src, "db.key"), pem.EncodeToMemory(&pem.Block{
			Type: "RSA PRIVATE KEY",
			Headers: map[string]string{
				"Proc-Type": "4,ENCRYPTED",
				"DEK-Info":  "AES-256-CBC,00000000000000000000000000000000",
			},
			Bytes: []byte("not really a key"),
		}))

		err := run(Options{OutputDir: out, ExistingKeysDir: src})
		Expect(err).To(MatchError(ContainSubstring("db.key is encrypted")))
	})

	It("rejects a key that is not PEM", func() {
		writeFile(filepath.Join(src, "KEK.key"), []byte("garbage"))

		err := run(Options{OutputDir: out, ExistingKeysDir: src})
		Expect(err).To(MatchError(ContainSubstring("KEK.key is not a PEM encoded private key")))
	})

	It("refuses to write over its own source", func() {
		before, err := os.ReadFile(filepath.Join(src, "PK.key"))
		Expect(err).ToNot(HaveOccurred())

		err = run(Options{OutputDir: src, ExistingKeysDir: src})
		Expect(err).To(MatchError(ContainSubstring("use a different output directory")))

		after, err := os.ReadFile(filepath.Join(src, "PK.key"))
		Expect(err).ToNot(HaveOccurred())
		Expect(after).To(Equal(before))
		_, err = efiutil.ReadKey(after)
		Expect(err).ToNot(HaveOccurred())
	})

	Context("tpm2-pcr-private.pem", func() {
		pcrKey := func() []byte {
			key, err := rsa.GenerateKey(rand.Reader, 2048)
			Expect(err).ToNot(HaveOccurred())
			der, err := x509.MarshalPKCS8PrivateKey(key)
			Expect(err).ToNot(HaveOccurred())
			return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
		}

		It("copies the PCR key from the existing dir as is", func() {
			want := pcrKey()
			writeFile(filepath.Join(src, "tpm2-pcr-private.pem"), want)

			Expect(run(Options{OutputDir: out, ExistingKeysDir: src})).To(Succeed())

			got, err := os.ReadFile(filepath.Join(out, "tpm2-pcr-private.pem"))
			Expect(err).ToNot(HaveOccurred())
			Expect(got).To(Equal(want))
			info, err := os.Stat(filepath.Join(out, "tpm2-pcr-private.pem"))
			Expect(err).ToNot(HaveOccurred())
			Expect(info.Mode().Perm()).To(Equal(os.FileMode(0o600)))
		})

		It("accepts a PKCS#1 RSA key", func() {
			key, err := rsa.GenerateKey(rand.Reader, 2048)
			Expect(err).ToNot(HaveOccurred())
			want := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
			writeFile(filepath.Join(src, "tpm2-pcr-private.pem"), want)

			Expect(run(Options{OutputDir: out, ExistingKeysDir: src})).To(Succeed())

			got, err := os.ReadFile(filepath.Join(out, "tpm2-pcr-private.pem"))
			Expect(err).ToNot(HaveOccurred())
			Expect(got).To(Equal(want))
		})

		It("generates a PCR key when the existing dir has none", func() {
			Expect(run(Options{OutputDir: out, ExistingKeysDir: src})).To(Succeed())

			got, err := os.ReadFile(filepath.Join(out, "tpm2-pcr-private.pem"))
			Expect(err).ToNot(HaveOccurred())
			Expect(checkPCRKey(got)).To(Succeed())
		})

		It("rejects a PCR key that is not PEM and writes nothing", func() {
			writeFile(filepath.Join(src, "tpm2-pcr-private.pem"), []byte("garbage"))

			err := run(Options{OutputDir: out, ExistingKeysDir: src})
			Expect(err).To(MatchError(ContainSubstring("tpm2-pcr-private.pem: is not a PEM encoded private key")))
			Expect(filepath.Join(out, "PK.key")).ToNot(BeAnExistingFile())
		})

		It("rejects an encrypted PCR key", func() {
			writeFile(filepath.Join(src, "tpm2-pcr-private.pem"), pem.EncodeToMemory(&pem.Block{Type: "ENCRYPTED PRIVATE KEY", Bytes: []byte("x")}))

			err := run(Options{OutputDir: out, ExistingKeysDir: src})
			Expect(err).To(MatchError(ContainSubstring("tpm2-pcr-private.pem: is encrypted")))
		})

		It("rejects a non-RSA PCR key", func() {
			key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
			Expect(err).ToNot(HaveOccurred())
			der, err := x509.MarshalPKCS8PrivateKey(key)
			Expect(err).ToNot(HaveOccurred())
			writeFile(filepath.Join(src, "tpm2-pcr-private.pem"), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))

			err = run(Options{OutputDir: out, ExistingKeysDir: src})
			Expect(err).To(MatchError(ContainSubstring("must be an RSA private key")))
		})
	})
})
