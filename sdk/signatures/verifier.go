package signatures

import (
	"bytes"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	iofs "io/fs"
	"os"

	"github.com/edsrzf/mmap-go"
	"github.com/foxboron/go-uefi/authenticode"
	"github.com/foxboron/go-uefi/efi"
	"github.com/foxboron/go-uefi/pkcs7"
	"github.com/kairos-io/kairos/v4/sdk/types/fs"
	peparser "github.com/saferwall/pe"
)

// VerifyingDBCert returns the first cert from the machine's EFI DB whose
// public key successfully verifies the Authenticode signature on artifact.
//
// Where CheckArtifactSignatureIsValid answers "is this artifact signed by
// something in DB (and not on DBX)," VerifyingDBCert answers the sharper
// question "WHICH cert in DB signed it." That distinction matters for the
// upgrade path: DB commonly holds several unrelated CAs (Microsoft +
// vendor + Kairos test keys); accepting any of them for an upgrade lets a
// .efi signed by an unrelated-but-DB-trusted vendor swap the Kairos
// system out. Callers compare the returned cert's fingerprint to the one
// on the currently-booted .efi to enforce "same signer as before,"
// which is the actual security bar.
//
// Uses the same DB traversal shape as CheckArtifactSignatureIsValid so
// its trust decisions stay in lockstep with the existing check; anything
// the existing function accepts, this one can identify. Returns an error
// when no DB cert verifies the artifact.
func VerifyingDBCert(fs fs.KairosFS, artifact string) (*x509.Certificate, error) {
	info, err := fs.Stat(artifact)
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", artifact, err)
	}
	if info.Size() == 0 {
		return nil, fmt.Errorf("%s: zero-size file", artifact)
	}
	f, err := fs.Open(artifact)
	if err != nil {
		return nil, err
	}
	defer func(f iofs.File) { _ = f.Close() }(f)
	fOS, ok := f.(*os.File)
	if !ok {
		return nil, fmt.Errorf("%s: cannot mmap non-os file", artifact)
	}
	data, err := mmap.Map(fOS, mmap.RDONLY, 0)
	if err != nil {
		return nil, err
	}
	defer func() { _ = data.Unmap() }()

	peFile, err := peparser.NewBytes(data, &peparser.Options{Fast: true})
	if err != nil {
		return nil, fmt.Errorf("wrapping %s bytes as PE: %w", artifact, err)
	}
	if err := peFile.Parse(); err != nil {
		return nil, fmt.Errorf("parsing %s as PE: %w", artifact, err)
	}
	if peFile.DOSHeader.Magic != peparser.ImageDOSZMSignature && peFile.DOSHeader.Magic != peparser.ImageDOSSignature {
		return nil, fmt.Errorf("%s: not a PE file", artifact)
	}

	db, err := efi.Getdb()
	if err != nil {
		return nil, fmt.Errorf("reading EFI DB: %w", err)
	}
	dbCerts := ExtractCertsFromSignatureDatabase(db)

	binary, err := authenticode.Parse(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", artifact, err)
	}
	if binary.Datadir.Size == 0 {
		return nil, fmt.Errorf("no signatures in %s", artifact)
	}
	sigs, err := binary.Signatures()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", artifact, err)
	}

	for _, sig := range sigs {
		for _, cert := range dbCerts {
			p, err := pkcs7.ParsePKCS7(sig.Certificate)
			if err != nil {
				return nil, fmt.Errorf("parse PKCS7 for %s: %w", artifact, err)
			}
			ok, _ := p.Verify(cert)
			if ok {
				return cert, nil
			}
		}
	}
	return nil, fmt.Errorf("no DB cert verifies %s", artifact)
}

// SameSigner reports whether two x509 certs share the same DER encoding
// (SHA-256 fingerprint match). Used to enforce that an upgrade .efi was
// signed by the exact cert that signed the currently-booted .efi, rather
// than by any-cert-in-DB.
func SameSigner(a, b *x509.Certificate) bool {
	if a == nil || b == nil {
		return false
	}
	ha := sha256.Sum256(a.Raw)
	hb := sha256.Sum256(b.Raw)
	return hex.EncodeToString(ha[:]) == hex.EncodeToString(hb[:])
}
