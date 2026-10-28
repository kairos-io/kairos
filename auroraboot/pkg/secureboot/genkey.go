// Package secureboot exposes Secure Boot key generation as a reusable Go API.
//
// This package mirrors the behavior of the `auroraboot genkey` CLI command but
// is callable as a library so external tools (for example, auroraboot) don't need
// to shell out to the auroraboot binary just to generate a key set.
//
// The generation pipeline still relies on `openssl` being available on PATH for
// X.509 certificate creation; the .auth/.esl files are produced via the
// foxboron/sbctl + foxboron/go-uefi libraries (no external commands required
// for that step).
package secureboot

import (
	"bytes"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/foxboron/go-uefi/efi/signature"
	efiutil "github.com/foxboron/go-uefi/efi/util"
	"github.com/foxboron/go-uefi/efivar"
	"github.com/foxboron/sbctl"
	"github.com/foxboron/sbctl/certs"
	"github.com/kairos-io/kairos/v4/sdk/types/logger"
)

// Options configures a Secure Boot key set generation.
type Options struct {
	// Name is the common-name prefix used for the X.509 certificate subjects
	// (e.g. "<name>-PK", "<name>-KEK", "<name>-db"). Required unless
	// ExistingKeysDir is set.
	Name string

	// OutputDir is the directory where all generated key files will be written.
	// Created with mode 0700 if it doesn't exist. Required.
	OutputDir string

	// ExpirationInDays controls the X.509 certificate validity. Defaults to "365"
	// when empty. Ignored when ExistingKeysDir is set.
	ExpirationInDays string

	// ExistingKeysDir is an optional directory holding PK.key, PK.pem,
	// KEK.key, KEK.pem, db.key and db.pem. When set, those key pairs are
	// copied into OutputDir instead of generating new ones, and the rest of
	// the set (.der, .auth, .esl) is built from them. Keys must be
	// unencrypted PKCS#8 RSA PEM and match their certificate.
	//
	// The directory may also hold tpm2-pcr-private.pem, an unencrypted RSA
	// private key. When present it is copied as is, so UKIs keep the same PCR
	// public key and already encrypted nodes can still unlock. When absent a
	// new one is generated; it is not derived from the other keys.
	ExistingKeysDir string

	// SkipMicrosoftCerts disables bundling Microsoft OEM certificates into the
	// KEK and db .auth files. THIS COULD BRICK FIRMWARE — only set to true if
	// you are sure your hardware doesn't need the Microsoft certificates.
	SkipMicrosoftCerts bool

	// CustomCertDir is an optional directory containing custom certificates to
	// enroll alongside the generated keys. The directory is expected to follow
	// the format produced by exporting authenticated EFI variables (see the
	// genkey CLI documentation for details). Empty disables custom cert handling.
	CustomCertDir string

	// Overwrite allows generating new keys into an OutputDir that already
	// holds PK.key, KEK.key, db.key or tpm2-pcr-private.pem. Without it
	// GenerateKeySet refuses, because replacing those files makes every
	// machine that enrolled the old keys reject what is signed afterwards.
	// Ignored when ExistingKeysDir is set.
	Overwrite bool

	// Logger is the kairos-sdk logger used for progress messages. If nil, a
	// default debug-level logger is used.
	Logger *logger.KairosLogger
}

// GenerateKeySet generates a complete Secure Boot key set under opts.OutputDir.
// On success the directory will contain:
//
//	PK.key, PK.pem, PK.der, PK.auth, PK.esl
//	KEK.key, KEK.pem, KEK.der, KEK.auth, KEK.esl
//	db.key, db.pem, db.der, db.auth, db.esl
//	tpm2-pcr-private.pem
//
// The .auth files are signed for SecureBoot auto-enrollment (PK signs PK and
// KEK; KEK signs db). When SkipMicrosoftCerts is false (the default), the KEK
// and db databases also include Microsoft's OEM certificates.
func GenerateKeySet(opts Options) error {
	if opts.Name == "" && opts.ExistingKeysDir == "" {
		return errors.New("name is required")
	}
	if opts.OutputDir == "" {
		return errors.New("output directory is required")
	}

	log := opts.Logger
	if log == nil {
		l := logger.NewKairosLogger("auroraboot", "debug", false)
		log = &l
	}

	uuid := sbctl.CreateUUID()
	guid := efiutil.StringToGUID(string(uuid))

	if opts.ExistingKeysDir == "" && !opts.Overwrite {
		for _, name := range []string{"PK.key", "KEK.key", "db.key", "tpm2-pcr-private.pem"} {
			if _, err := os.Stat(filepath.Join(opts.OutputDir, name)); err == nil {
				return fmt.Errorf("output directory %s already holds %s; refusing to overwrite existing keys, use a different directory or allow overwriting", opts.OutputDir, name)
			} else if !os.IsNotExist(err) {
				return fmt.Errorf("checking %s: %w", name, err)
			}
		}
	}

	if err := os.MkdirAll(opts.OutputDir, 0700); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}

	customDerDir := ""
	if opts.CustomCertDir != "" {
		var err error
		customDerDir, err = prepareCustomDerDir(*log, opts.CustomCertDir)
		if err != nil {
			return fmt.Errorf("preparing custom certs directory: %w", err)
		}
		defer os.RemoveAll(customDerDir)
	}

	copiedPCR := false
	if opts.ExistingKeysDir != "" {
		var err error
		copiedPCR, err = copyExistingKeys(*log, opts.ExistingKeysDir, opts.OutputDir)
		if err != nil {
			return err
		}
	}

	for _, keyType := range []string{"PK", "KEK", "db"} {
		key := filepath.Join(opts.OutputDir, fmt.Sprintf("%s.key", keyType))
		pem := filepath.Join(opts.OutputDir, fmt.Sprintf("%s.pem", keyType))
		der := filepath.Join(opts.OutputDir, fmt.Sprintf("%s.der", keyType))

		if opts.ExistingKeysDir == "" {
			log.Infof("Generating %s", keyType)
			args := []string{
				"req", "-nodes", "-x509", "-subj", fmt.Sprintf("/CN=%s-%s/", opts.Name, keyType),
				"-keyout", key,
				"-out", pem,
			}
			expirationDays := opts.ExpirationInDays
			if expirationDays == "" {
				expirationDays = "365"
			}
			args = append(args, "-days", expirationDays)

			cmd := exec.Command("openssl", args...)
			if out, err := cmd.CombinedOutput(); err != nil {
				return fmt.Errorf("generating %s key+cert: %w / %s", keyType, err, string(out))
			}
			log.Infof("%s generated at %s and %s", keyType, key, pem)
		}

		log.Infof("Converting %s.pem to DER", keyType)
		cmd := exec.Command("openssl", "x509", "-outform", "DER", "-in", pem, "-out", der)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("converting %s to DER: %w / %s", keyType, err, string(out))
		}
		log.Infof("%s generated at %s", keyType, der)

		if err := generateAuthKeys(*log, *guid, opts.OutputDir, keyType, customDerDir, opts.SkipMicrosoftCerts); err != nil {
			return fmt.Errorf("generating auth keys for %s: %w", keyType, err)
		}

		if customDerDir != "" && keyType != "PK" {
			if err := appendCustomDerCerts(keyType, customDerDir, opts.OutputDir); err != nil {
				return fmt.Errorf("appending custom der certs for %s: %w", keyType, err)
			}
		}
	}

	// Generate the TPM PCR policy encryption key (used for UKI builds), unless
	// one was copied from the existing keys directory.
	tpmPrivate := filepath.Join(opts.OutputDir, pcrKeyName)
	if copiedPCR {
		return nil
	}
	log.Infof("Generating policy encryption key")
	cmd := exec.Command("openssl", "genrsa", "-out", tpmPrivate, "2048")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("generating tpm2-pcr-private.pem: %w / %s", err, string(out))
	}

	return nil
}

// pcrKeyName is the TPM PCR policy key that UKI builds sign their measurements with.
const pcrKeyName = "tpm2-pcr-private.pem"

// copyExistingKeys checks the PK, KEK and db key pairs in srcDir and copies
// them into dstDir, together with tpm2-pcr-private.pem when srcDir has one
// (the returned bool says whether it did). Nothing is written until every file
// has been read and each key has been checked.
func copyExistingKeys(log logger.KairosLogger, srcDir, dstDir string) (bool, error) {
	type keyFile struct {
		name string
		mode os.FileMode
		data []byte
	}
	var files []keyFile

	for _, keyType := range []string{"PK", "KEK", "db"} {
		keyName, pemName := keyType+".key", keyType+".pem"
		for _, name := range []string{keyName, pemName} {
			src := filepath.Join(srcDir, name)
			srcInfo, err := os.Stat(src)
			if err != nil {
				if os.IsNotExist(err) {
					return false, fmt.Errorf("existing keys directory %s is missing %s", srcDir, name)
				}
				return false, fmt.Errorf("reading %s: %w", src, err)
			}
			if dstInfo, err := os.Stat(filepath.Join(dstDir, name)); err == nil && os.SameFile(srcInfo, dstInfo) {
				return false, fmt.Errorf("output directory %s already holds %s from the existing keys directory; use a different output directory", dstDir, name)
			}
		}

		keyBytes, err := os.ReadFile(filepath.Join(srcDir, keyName))
		if err != nil {
			return false, fmt.Errorf("reading %s: %w", keyName, err)
		}
		pemBytes, err := os.ReadFile(filepath.Join(srcDir, pemName))
		if err != nil {
			return false, fmt.Errorf("reading %s: %w", pemName, err)
		}

		block, _ := pem.Decode(keyBytes)
		if block == nil {
			return false, fmt.Errorf("%s is not a PEM encoded private key", keyName)
		}
		if block.Type == "ENCRYPTED PRIVATE KEY" || strings.Contains(block.Headers["Proc-Type"], "ENCRYPTED") {
			return false, fmt.Errorf("%s is encrypted; provide an unencrypted key", keyName)
		}
		key, err := efiutil.ReadKey(keyBytes)
		if err != nil {
			return false, fmt.Errorf("%s must be an unencrypted PKCS#8 RSA private key: %w", keyName, err)
		}
		cert, err := efiutil.ReadCert(pemBytes)
		if err != nil {
			return false, fmt.Errorf("%s is not a PEM encoded X.509 certificate: %w", pemName, err)
		}
		if !key.PublicKey.Equal(cert.PublicKey) {
			return false, fmt.Errorf("%s does not match the public key in %s", keyName, pemName)
		}

		files = append(files, keyFile{keyName, 0o600, keyBytes}, keyFile{pemName, 0o644, pemBytes})
	}

	havePCR := false
	pcrSrc := filepath.Join(srcDir, pcrKeyName)
	if srcInfo, err := os.Stat(pcrSrc); err == nil {
		if dstInfo, err := os.Stat(filepath.Join(dstDir, pcrKeyName)); err == nil && os.SameFile(srcInfo, dstInfo) {
			return false, fmt.Errorf("output directory %s already holds %s from the existing keys directory; use a different output directory", dstDir, pcrKeyName)
		}
		pcrBytes, err := os.ReadFile(pcrSrc)
		if err != nil {
			return false, fmt.Errorf("reading %s: %w", pcrKeyName, err)
		}
		if err := checkPCRKey(pcrBytes); err != nil {
			return false, fmt.Errorf("%s: %w", pcrKeyName, err)
		}
		files = append(files, keyFile{pcrKeyName, 0o600, pcrBytes})
		havePCR = true
	} else if !os.IsNotExist(err) {
		return false, fmt.Errorf("reading %s: %w", pcrSrc, err)
	}

	for _, f := range files {
		dst := filepath.Join(dstDir, f.name)
		if err := os.WriteFile(dst, f.data, f.mode); err != nil {
			return false, fmt.Errorf("writing %s: %w", dst, err)
		}
		if err := os.Chmod(dst, f.mode); err != nil {
			return false, fmt.Errorf("setting permissions on %s: %w", dst, err)
		}
	}
	log.Infof("Using existing PK, KEK and db keys from %s", srcDir)
	if havePCR {
		log.Infof("Using existing %s from %s", pcrKeyName, srcDir)
	} else {
		log.Infof("No %s in %s, a new one will be generated", pcrKeyName, srcDir)
	}

	return havePCR, nil
}

// checkPCRKey makes sure data is an unencrypted PEM encoded RSA private key.
func checkPCRKey(data []byte) error {
	block, _ := pem.Decode(data)
	if block == nil {
		return errors.New("is not a PEM encoded private key")
	}
	if block.Type == "ENCRYPTED PRIVATE KEY" || strings.Contains(block.Headers["Proc-Type"], "ENCRYPTED") {
		return errors.New("is encrypted; provide an unencrypted key")
	}
	if key, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		if _, ok := key.(*rsa.PrivateKey); !ok {
			return errors.New("must be an RSA private key")
		}
		return nil
	}
	if _, err := x509.ParsePKCS1PrivateKey(block.Bytes); err != nil {
		return errors.New("must be an unencrypted RSA private key in PKCS#1 or PKCS#8 PEM form")
	}
	return nil
}

// generateAuthKeys produces the .auth and .esl files for a given key type using
// the foxboron/sbctl and foxboron/go-uefi libraries. PK signs itself and KEK;
// KEK signs db.
func generateAuthKeys(log logger.KairosLogger, guid efiutil.EFIGUID, keyPath, keyType, customDerCertDir string, skipMicrosoftCerts bool) error {
	// Enrollment chain of trust: PK is self-signed; KEK is signed by PK;
	// db is signed by KEK. The signer's key AND certificate must match
	// each other so that PKCS7.Verify (which checks both IssuerAndSerial
	// and the signature bytes against the cert's public key) accepts the
	// resulting .auth file.
	var signerName string
	switch keyType {
	case "PK", "KEK":
		signerName = "PK"
	case "db":
		signerName = "KEK"
	default:
		return fmt.Errorf("unsupported key type %s", keyType)
	}

	signerKeyBytes, err := os.ReadFile(filepath.Join(keyPath, signerName+".key"))
	if err != nil {
		return fmt.Errorf("reading signer key file: %w", err)
	}
	signerPemBytes, err := os.ReadFile(filepath.Join(keyPath, signerName+".pem"))
	if err != nil {
		return fmt.Errorf("reading signer pem file: %w", err)
	}

	pem, err := os.ReadFile(filepath.Join(keyPath, keyType+".pem"))
	if err != nil {
		return fmt.Errorf("reading the pem file: %w", err)
	}

	sigdb := signature.NewSignatureDatabase()

	if err := sigdb.Append(signature.CERT_X509_GUID, guid, pem); err != nil {
		return fmt.Errorf("appending signature: %w", err)
	}

	if keyType != "PK" && !skipMicrosoftCerts {
		oemSigDb, err := certs.GetOEMCerts("microsoft", keyType)
		if err != nil {
			return fmt.Errorf("loading microsoft keys (type %s): %w", keyType, err)
		}
		appendDatabaseSkippingDuplicates(sigdb, oemSigDb, log, keyType, "Microsoft")
	}

	if keyType != "PK" && customDerCertDir != "" {
		customSigDb, err := certs.GetCustomCerts(customDerCertDir, keyType)
		if err != nil {
			return fmt.Errorf("loading custom keys (type %s): %w", keyType, err)
		}
		appendDatabaseSkippingDuplicates(sigdb, customSigDb, log, keyType, "custom")

		customHashDb, err := getCustomHashes(customDerCertDir, keyType)
		if err != nil {
			return fmt.Errorf("loading custom hashes (type %s): %w", keyType, err)
		}
		appendDatabaseSkippingDuplicates(sigdb, customHashDb, log, keyType, "custom")
	}

	var efiVarType efivar.Efivar
	switch strings.ToLower(keyType) {
	case "pk":
		efiVarType = efivar.PK
	case "kek":
		efiVarType = efivar.KEK
	case "db":
		efiVarType = efivar.Db
	}

	signer, err := efiutil.ReadKey(signerKeyBytes)
	if err != nil {
		return fmt.Errorf("parsing signing key: %w", err)
	}
	cert, err := efiutil.ReadCert(signerPemBytes)
	if err != nil {
		return fmt.Errorf("parsing signing certificate: %w", err)
	}

	_, signedVar, err := signature.SignEFIVariable(efiVarType, sigdb, signer, cert)
	if err != nil {
		return fmt.Errorf("creating the signed db: %w", err)
	}

	if err := os.WriteFile(filepath.Join(keyPath, keyType+".auth"), signedVar.Bytes(), 0o644); err != nil {
		return fmt.Errorf("writing the auth file: %w", err)
	}

	if err := os.WriteFile(filepath.Join(keyPath, keyType+".esl"), sigdb.Bytes(), 0o644); err != nil {
		return fmt.Errorf("writing the esl file: %w", err)
	}

	return nil
}

// prepareCustomDerDir takes a cert directory with keys as they are exported
// from the UEFI firmware and prepares them for use with sbctl. The keys are
// expected to be in the "der" format in a specific directory structure under
// "db" and "KEK" subdirectories. It returns the prepared temporary directory.
func prepareCustomDerDir(l logger.KairosLogger, customCertDir string) (string, error) {
	if _, err := os.Stat(customCertDir); os.IsNotExist(err) {
		return "", fmt.Errorf("custom cert directory does not exist: %s", customCertDir)
	}

	tmpDir, err := os.MkdirTemp("", "sbctl-custom-certs-*")
	if err != nil {
		return "", fmt.Errorf("creating temporary directory: %w", err)
	}

	for _, keyType := range []string{"db", "KEK"} {
		b, err := os.ReadFile(filepath.Join(customCertDir, keyType))
		if err != nil {
			return "", fmt.Errorf("reading custom cert file %s: %w", keyType, err)
		}
		f := bytes.NewReader(b)
		siglist, err := signature.ReadSignatureDatabase(f)
		if err != nil {
			return "", fmt.Errorf("reading signature database: %w", err)
		}

		l.Infof("Converting custom certs (type: %s)\n", keyType)
		for _, sig := range siglist {
			for _, sigEntry := range sig.Signatures {
				l.Infof("	Signature Owner: %s\n", sigEntry.Owner.Format())
				switch sig.SignatureType {
				case signature.CERT_SHA256_GUID:
					if len(sigEntry.Data) != sha256.Size {
						l.Errorf("hash error: SHA256 entry of %d bytes", len(sigEntry.Data))
						continue
					}
					hashDir := filepath.Join(tmpDir, customHashesDir, keyType)
					if err := os.MkdirAll(hashDir, 0755); err != nil {
						return "", fmt.Errorf("creating hash directory for key type %s: %w", keyType, err)
					}
					if err := os.WriteFile(filepath.Join(hashDir, fmt.Sprintf("%s-%x", keyType, sigEntry.Data)), sigEntry.Data, 0644); err != nil {
						return "", fmt.Errorf("writing custom hash file: %w", err)
					}
				case signature.CERT_X509_GUID:
					cert, err := x509.ParseCertificate(sigEntry.Data)
					if err != nil {
						l.Errorf("cert error: %s", err)
						continue
					}
					keyDir := filepath.Join(tmpDir, "custom", keyType)
					if err := os.MkdirAll(keyDir, 0755); err != nil {
						return "", fmt.Errorf("creating directory for key type %s: %w", keyType, err)
					}
					if err := os.WriteFile(filepath.Join(keyDir, fmt.Sprintf("%s%s", keyType, cert.SerialNumber.String())), cert.Raw, 0644); err != nil {
						return "", fmt.Errorf("writing custom cert file: %w", err)
					}
				default:
					l.Errorf("Not implemented!\n%s\n", sig.SignatureType.Format())
				}
			}
		}
	}

	return tmpDir, nil
}

// customHashesDir holds the SHA256 entries prepareCustomDerDir keeps apart
// from the certificates, as sbctl reads everything under "custom" as X.509.
const customHashesDir = "custom-hashes"

// customOwnerGUID is the owner GUID certs.GetCustomCerts gives custom certs.
const customOwnerGUID = "88a69775-5ad7-45d9-9f34-cec43e1f1989"

// getCustomHashes reads the SHA256 entries prepareCustomDerDir wrote for a
// variable into a signature database.
func getCustomHashes(customDerCertDir, keyType string) (*signature.SignatureDatabase, error) {
	sigdb := signature.NewSignatureDatabase()
	hashDir := filepath.Join(customDerCertDir, customHashesDir, keyType)
	files, err := os.ReadDir(hashDir)
	if os.IsNotExist(err) {
		return sigdb, nil
	}
	if err != nil {
		return nil, err
	}
	owner := *efiutil.StringToGUID(customOwnerGUID)
	for _, file := range files {
		if !file.Type().IsRegular() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(hashDir, file.Name()))
		if err != nil {
			return nil, err
		}
		if err := sigdb.Append(signature.CERT_SHA256_GUID, owner, data); err != nil {
			return nil, fmt.Errorf("%s: %w", file.Name(), err)
		}
	}
	return sigdb, nil
}

// appendCustomDerCerts appends custom DER certificates to the final .der file
// for the given key type.
func appendCustomDerCerts(keyType, customDerCertDir, keyPath string) error {
	if customDerCertDir == "" {
		return nil
	}

	finalDerFile := filepath.Join(keyPath, keyType+".der")
	final, err := os.OpenFile(finalDerFile, os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("opening final der file %s: %w", finalDerFile, err)
	}
	defer final.Close()

	customDerDir := filepath.Join(customDerCertDir, "custom", keyType)
	return filepath.Walk(customDerDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasPrefix(info.Name(), keyType) {
			customData, err := os.ReadFile(filepath.Join(customDerDir, info.Name()))
			if err != nil {
				return fmt.Errorf("reading custom der file %s: %w", keyType, err)
			}
			if _, err := final.Write(customData); err != nil {
				return fmt.Errorf("appending custom der file %s: %w", keyType, err)
			}
		}
		return nil
	})
}

// containsData reports whether any of these signatures carries this payload.
func containsData(sigs []signature.SignatureData, data []byte) bool {
	for _, sig := range sigs {
		if bytes.Equal(sig.Data, data) {
			return true
		}
	}
	return false
}

// signatureDataPresent reports whether sd already carries this exact payload
// under the given signature type.
//
// The owner GUID is deliberately not part of the comparison. certs.GetOEMCerts
// stamps everything it returns with Microsoft's GUID and certs.GetCustomCerts
// stamps everything it returns with a fixed "custom" GUID, so one certificate
// read from both sources arrives under two different owners. What firmware
// ends up trusting is the certificate, so the certificate is what must be
// unique.
func signatureDataPresent(sd *signature.SignatureDatabase, certType efiutil.EFIGUID, data []byte) bool {
	for _, list := range *sd {
		if !efiutil.CmpEFIGUID(list.SignatureType, certType) {
			continue
		}
		if containsData(list.Signatures, data) {
			return true
		}
	}
	return false
}

// appendDatabaseSkippingDuplicates merges src into dst, leaving out signatures
// dst already carries.
//
// signature.SignatureDatabase.AppendDatabase appends whole signature lists
// without looking at what the destination already holds, so certificates a
// user exported from their firmware get merged in blind. Firmware that already
// trusts Microsoft exports the Microsoft certificates too, so handing that
// export to --custom-cert-dir without also passing --skip-microsoft-certs
// enrolls each of them twice. See kairos-io/kairos#2432.
//
// Lists are filtered rather than rebuilt, so a merge that drops nothing
// produces exactly the bytes AppendDatabase would have.
func appendDatabaseSkippingDuplicates(dst, src *signature.SignatureDatabase, log logger.KairosLogger, keyType, source string) {
	for _, list := range *src {
		kept := make([]signature.SignatureData, 0, len(list.Signatures))
		for _, sig := range list.Signatures {
			// dst catches a certificate enrolled from an earlier source;
			// kept catches a source that repeats itself.
			if signatureDataPresent(dst, list.SignatureType, sig.Data) || containsData(kept, sig.Data) {
				log.Infof("Skipping %s certificate already enrolled in %s: %s",
					source, keyType, describeSignature(list.SignatureType, sig.Data))
				continue
			}
			kept = append(kept, sig)
		}
		if len(kept) == 0 {
			continue
		}
		filtered := *list
		filtered.Signatures = kept
		filtered.ListSize = signature.SizeofSignatureList + filtered.HeaderSize + uint32(len(kept))*filtered.Size
		dst.AppendList(&filtered)
	}
}

// describeSignature renders a signature payload for a log line: the
// certificate subject when it parses as X.509, a size otherwise.
func describeSignature(certType efiutil.EFIGUID, data []byte) string {
	if efiutil.CmpEFIGUID(certType, signature.CERT_X509_GUID) {
		if cert, err := x509.ParseCertificate(data); err == nil {
			return cert.Subject.String()
		}
	}
	return fmt.Sprintf("%s entry of %d bytes", signature.ValidEFISignatureSchemes[certType], len(data))
}
