package signatures

import (
	"bytes"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	iofs "io/fs"
	"os"

	"github.com/edsrzf/mmap-go"
	"github.com/foxboron/go-uefi/authenticode"
	"github.com/foxboron/go-uefi/efi"
	"github.com/foxboron/go-uefi/efi/signature"
	"github.com/foxboron/go-uefi/efi/util"
	"github.com/foxboron/go-uefi/pkcs7"
	"github.com/kairos-io/kairos-sdk/types/certs"
	"github.com/kairos-io/kairos-sdk/types/fs"
	sdkLogger "github.com/kairos-io/kairos-sdk/types/logger"
	peparser "github.com/saferwall/pe"
)

// GetKeyDatabase returns a single signature.SignatureDatabase for a given type
func GetKeyDatabase(sigType string) (*signature.SignatureDatabase, error) {
	var err error
	var sig *signature.SignatureDatabase

	switch sigType {
	case "PK", "pk":
		sig, err = efi.GetPK()
	case "KEK", "kek":
		sig, err = efi.GetKEK()
	case "DB", "db":
		sig, err = efi.Getdb()
	default:
		return nil, fmt.Errorf("signature type unkown (%s). Valid signature types are PK,KEK,DB", sigType)
	}

	return sig, err
}

// GetAllFullCerts returns a list of certs in the system. Full cert, including raw data of the cert
func GetAllFullCerts() (certs.CertListFull, error) {
	var certList certs.CertListFull
	pk, err := GetKeyDatabase("PK")
	if err != nil {
		return certList, err
	}
	kek, err := GetKeyDatabase("KEK")
	if err != nil {
		return certList, err
	}
	db, err := GetKeyDatabase("DB")
	if err != nil {
		return certList, err
	}

	certList.PK = ExtractCertsFromSignatureDatabase(pk)
	certList.KEK = ExtractCertsFromSignatureDatabase(kek)
	certList.DB = ExtractCertsFromSignatureDatabase(db)

	return certList, nil
}

// ExtractCertsFromSignatureDatabase returns a []*x509.Certificate from a *signature.SignatureDatabase
func ExtractCertsFromSignatureDatabase(database *signature.SignatureDatabase) []*x509.Certificate {
	var result []*x509.Certificate
	for _, k := range *database {
		if isValidSignature(k.SignatureType) {
			for _, k1 := range k.Signatures {
				// Note the S at the end of the function, we are parsing multiple certs, not just one
				certificates, err := x509.ParseCertificates(k1.Data)
				if err != nil {
					continue
				}
				result = append(result, certificates...)
			}
		}
	}
	return result
}

// GetAllCerts returns a list of certs in the system
func GetAllCerts() (certs.CertList, error) {
	var certList certs.CertList
	pk, err := GetKeyDatabase("PK")
	if err != nil {
		return certList, err
	}
	kek, err := GetKeyDatabase("KEK")
	if err != nil {
		return certList, err
	}
	db, err := GetKeyDatabase("DB")
	if err != nil {
		return certList, err
	}

	for _, k := range *pk {
		if isValidSignature(k.SignatureType) {
			for _, k1 := range k.Signatures {
				// Note the S at the end of the function, we are parsing multiple certs, not just one
				certificates, err := x509.ParseCertificates(k1.Data)
				if err != nil {
					continue
				}
				for _, cert := range certificates {
					certList.PK = append(certList.PK, certs.CertDetail{Owner: cert.Subject, Issuer: cert.Issuer})
				}
			}
		}
	}

	for _, k := range *kek {
		if isValidSignature(k.SignatureType) {
			for _, k1 := range k.Signatures {
				// Note the S at the end of the function, we are parsing multiple certs, not just one
				certificates, err := x509.ParseCertificates(k1.Data)
				if err != nil {
					continue
				}
				for _, cert := range certificates {
					certList.KEK = append(certList.KEK, certs.CertDetail{Owner: cert.Subject, Issuer: cert.Issuer})
				}
			}
		}
	}

	for _, k := range *db {
		if isValidSignature(k.SignatureType) {
			for _, k1 := range k.Signatures {
				// Note the S at the end of the function, we are parsing multiple certs, not just one
				certificates, err := x509.ParseCertificates(k1.Data)
				if err != nil {
					continue
				}
				for _, cert := range certificates {
					certList.DB = append(certList.DB, certs.CertDetail{Owner: cert.Subject, Issuer: cert.Issuer})
				}
			}
		}
	}

	return certList, nil
}

// isValidSignature identifies a signature based as a DER-encoded X.509 certificate
func isValidSignature(sign util.EFIGUID) bool {
	return sign == signature.CERT_X509_GUID
}

// CheckArtifactSignatureIsValid checks that a given efi artifact is signed properly with a signature that would allow it to
// boot correctly in the current node if secureboot is enabled
func CheckArtifactSignatureIsValid(fs fs.KairosFS, artifact string, logger sdkLogger.KairosLogger) error {
	var err error
	logger.Logger.Info().Str("what", artifact).Msg("Checking artifact for valid signature")
	info, err := fs.Stat(artifact)
	if errors.Is(err, os.ErrNotExist) {
		logger.Warnf("%s does not exist", artifact)
		return fmt.Errorf("%s does not exist", artifact)
	} else if errors.Is(err, os.ErrPermission) {
		logger.Warnf("%s permission denied. Can't read file", artifact)
		return fmt.Errorf("%s permission denied. Can't read file", artifact)
	} else if err != nil {
		return err
	}
	if info.Size() == 0 {
		logger.Warnf("%s file is empty denied", artifact)
		return fmt.Errorf("%s file has zero size", artifact)
	}
	logger.Logger.Debug().Str("what", artifact).Msg("Reading artifact")

	// MMAP the file, seems to save memory rather than reading the full file
	// Unfortunately we have to do some type conversion to keep using the v1.Fs
	f, err := fs.Open(artifact)
	defer func(f iofs.File) {
		_ = f.Close()
	}(f)
	if err != nil {
		return err
	}
	// type conversion, ugh
	fOS := f.(*os.File)
	data, err := mmap.Map(fOS, mmap.RDONLY, 0)
	defer func(data *mmap.MMap) {
		_ = data.Unmap()
	}(&data)
	if err != nil {
		return err
	}

	// Get sha256 of the artifact
	// Note that this is a PEFile, so it's a bit different from a normal file as there are some sections that need to be
	// excluded when calculating the sha
	logger.Logger.Debug().Str("what", artifact).Msg("Parsing PE artifact")
	file, _ := peparser.NewBytes(data, &peparser.Options{Fast: true})
	err = file.Parse()
	if err != nil {
		logger.Logger.Error().Err(err).Msg("parsing PE file for hash")
		return err
	}

	logger.Logger.Debug().Str("what", artifact).Msg("Checking if its an EFI file")
	// Check for proper header in the efi file
	if file.DOSHeader.Magic != peparser.ImageDOSZMSignature && file.DOSHeader.Magic != peparser.ImageDOSSignature {
		logger.Error(fmt.Errorf("no pe file header: %d", file.DOSHeader.Magic))
		return fmt.Errorf("no pe file header: %d", file.DOSHeader.Magic)
	}

	// Get hash to compare in dbx if we have hashes
	hashArtifact := hex.EncodeToString(file.Authentihash())

	logger.Logger.Debug().Str("what", artifact).Msg("Getting DB certs")
	// We need to read the current db database to have the proper certs to check against
	db, err := efi.Getdb()
	if err != nil {
		logger.Logger.Error().Err(err).Msg("Getting DB certs")
		return err
	}

	dbCerts := ExtractCertsFromSignatureDatabase(db)

	logger.Logger.Debug().Str("what", artifact).Msg("Getting signatures from artifact")
	// Get signatures from the artifact
	binary, err := authenticode.Parse(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("%s: %w", artifact, err)
	}
	if binary.Datadir.Size == 0 {
		return fmt.Errorf("no signatures in the file %s", artifact)
	}

	sigs, err := binary.Signatures()
	if err != nil {
		return fmt.Errorf("%s: %w", artifact, err)
	}

	logger.Logger.Debug().Str("what", artifact).Msg("Getting DBX certs")
	dbx, err := efi.Getdbx()
	if err != nil {
		logger.Logger.Error().Err(err).Msg("getting DBX certs")
		return err
	}

	// First check the dbx database as it has precedence, on match, return immediately
	for _, k := range *dbx {
		switch k.SignatureType {
		case signature.CERT_SHA256_GUID: // SHA256 hash
			// Compare it against the dbx
			for _, k1 := range k.Signatures {
				shaSign := hex.EncodeToString(k1.Data)
				logger.Logger.Debug().Str("artifact", string(hashArtifact)).Str("signature", shaSign).Msg("Comparing hashes")
				if hashArtifact == shaSign {
					return fmt.Errorf("hash appears on DBX: %s", hashArtifact)
				}

			}
		case signature.CERT_X509_GUID: // Certificate
			var result []*x509.Certificate
			for _, k1 := range k.Signatures {
				certificates, err := x509.ParseCertificates(k1.Data)
				if err != nil {
					continue
				}
				result = append(result, certificates...)
			}
			for _, sig := range sigs {
				for _, cert := range result {
					logger.Logger.Debug().Str("what", artifact).Str("subject", cert.Subject.CommonName).Msg("checking signature")
					p, err := pkcs7.ParsePKCS7(sig.Certificate)
					if err != nil {
						logger.Logger.Info().Str("error", err.Error()).Msg("parsing signature")
						return err
					}
					ok, _ := p.Verify(cert)
					// If cert matches then it means its blacklisted so return error
					if ok {
						return fmt.Errorf("artifact is signed with a blacklisted cert")
					}

				}
			}
		default:
			logger.Logger.Debug().Str("what", artifact).Str("cert type", string(signature.ValidEFISignatureSchemes[k.SignatureType])).Msg("not supported type of cert")
		}
	}

	// Now check against the DB to see if its allowed
	for _, sig := range sigs {
		for _, cert := range dbCerts {
			logger.Logger.Debug().Str("what", artifact).Str("subject", cert.Subject.CommonName).Msg("checking signature")
			p, err := pkcs7.ParsePKCS7(sig.Certificate)
			if err != nil {
				logger.Logger.Info().Str("error", err.Error()).Msg("parsing signature")
				return err
			}
			ok, _ := p.Verify(cert)
			if ok {
				logger.Logger.Info().Str("what", artifact).Str("subject", cert.Subject.CommonName).Msg("verified")
				return nil
			}
		}
	}
	// If we reach this point, we need to fail as we haven't matched anything, so default is to fail
	return fmt.Errorf("could not find a signature in EFIVars DB that matches the artifact")
}
