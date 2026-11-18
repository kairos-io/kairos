package secureboot

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/foxboron/go-uefi/efi/signature"
	efiutil "github.com/foxboron/go-uefi/efi/util"
)

// TestGenerateKeySet_AuthFilesVerify generates a full PK/KEK/db key set with
// Microsoft certs disabled (hermetic) and asserts that each produced .auth
// file's embedded PKCS7 signature verifies against its expected signer
// certificate.
//
// Chain of trust for Secure Boot enrollment:
//   - PK.auth is self-signed by PK (PK enrolls itself out of setup mode).
//   - KEK.auth is signed by PK (PK is the enroll-time trust anchor).
//   - db.auth is signed by KEK (KEK is the enroll-time trust anchor for db).
//
// The .auth signature validation encodes exactly what firmware does at
// SetVariable time, so if these assertions pass, the produced files are
// enrollable.
func TestGenerateKeySet_AuthFilesVerify(t *testing.T) {
	tmp := t.TempDir()
	if err := GenerateKeySet(Options{
		Name:               "auroraboot-test",
		OutputDir:          tmp,
		ExpirationInDays:   "365",
		SkipMicrosoftCerts: true,
	}); err != nil {
		t.Fatalf("GenerateKeySet: %v", err)
	}

	for _, kt := range []string{"PK", "KEK", "db"} {
		for _, ext := range []string{".key", ".pem", ".der", ".auth", ".esl"} {
			p := filepath.Join(tmp, kt+ext)
			info, err := os.Stat(p)
			if err != nil {
				t.Fatalf("missing %s: %v", p, err)
			}
			if info.Size() == 0 {
				t.Fatalf("empty %s", p)
			}
		}
	}

	// PK signs PK+KEK; KEK signs db.
	signerFor := map[string]string{"PK": "PK", "KEK": "PK", "db": "KEK"}

	for _, kt := range []string{"PK", "KEK", "db"} {
		authBytes, err := os.ReadFile(filepath.Join(tmp, kt+".auth"))
		if err != nil {
			t.Fatalf("read %s.auth: %v", kt, err)
		}
		signerPemBytes, err := os.ReadFile(filepath.Join(tmp, signerFor[kt]+".pem"))
		if err != nil {
			t.Fatalf("read %s.pem: %v", signerFor[kt], err)
		}
		signerCert, err := efiutil.ReadCert(signerPemBytes)
		if err != nil {
			t.Fatalf("parse %s.pem: %v", signerFor[kt], err)
		}

		var auth signature.EFIVariableAuthentication2
		if err := auth.Unmarshal(bytes.NewBuffer(authBytes)); err != nil {
			t.Fatalf("unmarshal %s.auth: %v", kt, err)
		}
		ok, err := auth.Verify(signerCert)
		if err != nil {
			t.Errorf("%s.auth verify against %s cert: %v", kt, signerFor[kt], err)
			continue
		}
		if !ok {
			t.Errorf("%s.auth signature did not verify against %s cert", kt, signerFor[kt])
		}
	}
}

// TestGenerateKeySet_EslContainsEnrolledCert asserts that each produced .esl
// signature database contains the DER of the certificate being enrolled for
// that variable (PK.esl has PK, etc.). .esl bytes are deterministic (no
// timestamp), so this is a stable byte-level assertion.
func TestGenerateKeySet_EslContainsEnrolledCert(t *testing.T) {
	tmp := t.TempDir()
	if err := GenerateKeySet(Options{
		Name:               "auroraboot-test",
		OutputDir:          tmp,
		ExpirationInDays:   "365",
		SkipMicrosoftCerts: true,
	}); err != nil {
		t.Fatalf("GenerateKeySet: %v", err)
	}

	for _, kt := range []string{"PK", "KEK", "db"} {
		pemBytes, err := os.ReadFile(filepath.Join(tmp, kt+".pem"))
		if err != nil {
			t.Fatalf("read %s.pem: %v", kt, err)
		}
		enrolled, err := efiutil.ReadCert(pemBytes)
		if err != nil {
			t.Fatalf("parse %s.pem: %v", kt, err)
		}

		eslBytes, err := os.ReadFile(filepath.Join(tmp, kt+".esl"))
		if err != nil {
			t.Fatalf("read %s.esl: %v", kt, err)
		}
		sigdb, err := signature.ReadSignatureDatabase(bytes.NewReader(eslBytes))
		if err != nil {
			t.Fatalf("parse %s.esl: %v", kt, err)
		}

		found := false
		for _, sl := range sigdb {
			if sl.SignatureType != signature.CERT_X509_GUID {
				continue
			}
			for _, s := range sl.Signatures {
				if bytes.Equal(s.Data, enrolled.Raw) {
					found = true
				}
			}
		}
		if !found {
			t.Errorf("%s.esl does not contain enrolled certificate", kt)
		}
	}
}
