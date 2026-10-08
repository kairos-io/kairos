package testartifacts

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/binary"
	"fmt"

	"github.com/foxboron/go-uefi/efi/attributes"
	"github.com/foxboron/go-uefi/efi/signature"
	"github.com/foxboron/go-uefi/efi/util"
)

// signatureOwner is the owner GUID recorded on every entry these helpers
// write. Firmware ignores it when checking signatures.
var signatureOwner = *util.StringToGUID("8f2bd58e-6c5b-4c4e-9b3a-2d0b6a1d7e41")

// dbVarAttributes are the attributes a real db or dbx carries: non
// volatile, boot and runtime access, time based authenticated writes.
const dbVarAttributes = attributes.EFI_VARIABLE_NON_VOLATILE |
	attributes.EFI_VARIABLE_BOOTSERVICE_ACCESS |
	attributes.EFI_VARIABLE_RUNTIME_ACCESS |
	attributes.EFI_VARIABLE_TIME_BASED_AUTHENTICATED_WRITE_ACCESS

// SignatureDBVarName returns the efivarfs file name of the image security
// database variable called name, such as "db" or "dbx".
func SignatureDBVarName(name string) string {
	return name + "-" + attributes.EFI_IMAGE_SECURITY_DATABASE_GUID.Format()
}

// CertDBVar returns efivarfs file contents for a signature database that
// holds the given X.509 certificates.
func CertDBVar(certs ...*x509.Certificate) ([]byte, error) {
	sd := signature.NewSignatureDatabase()
	for _, c := range certs {
		if err := sd.Append(signature.CERT_X509_GUID, signatureOwner, c.Raw); err != nil {
			return nil, fmt.Errorf("adding certificate %q: %w", c.Subject.CommonName, err)
		}
	}
	return efivarBytes(sd), nil
}

// HashDBVar returns efivarfs file contents for a signature database that
// holds the given SHA-256 hashes.
func HashDBVar(hashes ...[]byte) ([]byte, error) {
	sd := signature.NewSignatureDatabase()
	for _, h := range hashes {
		if len(h) != sha256.Size {
			return nil, fmt.Errorf("hash is %d bytes, want %d", len(h), sha256.Size)
		}
		if err := sd.Append(signature.CERT_SHA256_GUID, signatureOwner, h); err != nil {
			return nil, fmt.Errorf("adding hash: %w", err)
		}
	}
	return efivarBytes(sd), nil
}

// efivarBytes prefixes the database with the 4-byte attribute header
// efivarfs puts in front of every variable.
func efivarBytes(sd *signature.SignatureDatabase) []byte {
	out := binary.LittleEndian.AppendUint32(nil, uint32(dbVarAttributes))
	return append(out, sd.Bytes()...)
}
