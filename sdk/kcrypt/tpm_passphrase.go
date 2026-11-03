package kcrypt

import (
	"github.com/kairos-io/tpm-helpers"
	"github.com/mudler/yip/pkg/utils"
)

const (
	// DefaultLocalPassphraseNVIndex is the default TPM NV index for storing local passphrases.
	DefaultLocalPassphraseNVIndex = "0x1500000"
)

// localTPMHandles holds the two independent TPM handles the local passphrase
// path uses. They address different objects and must never be resolved from a
// single shared option slice.
//
// storeIndex is the NV index the encrypted passphrase is stored at. keyIndex is
// the persistent RSA key that encrypts and decrypts it; empty means the
// tpm-helpers default. Encrypting with one key and decrypting with another
// leaves a passphrase nobody can recover, and the node stays locked on the next
// boot.
type localTPMHandles struct {
	storeIndex string
	keyIndex   string
	device     string
}

// resolveLocalTPMHandles fills in the default NV index when the caller did not
// configure one. An empty keyIndex or device is passed through, so tpm-helpers
// applies its own default.
func resolveLocalTPMHandles(nvIndex, cIndex, tpmDevice string) localTPMHandles {
	if nvIndex == "" {
		nvIndex = DefaultLocalPassphraseNVIndex
	}

	return localTPMHandles{
		storeIndex: nvIndex,
		keyIndex:   cIndex,
		device:     tpmDevice,
	}
}

// nvOpts addresses the NV index holding the encrypted passphrase. Use it for
// ReadBlob and StoreBlob.
func (h localTPMHandles) nvOpts() []tpm.TPMOption {
	opts := []tpm.TPMOption{tpm.WithIndex(h.storeIndex)}
	if h.device != "" {
		opts = append(opts, tpm.WithDevice(h.device))
	}

	return opts
}

// keyOpts addresses the persistent key that encrypts and decrypts the
// passphrase. Use it for EncryptBlob and DecryptBlob.
func (h localTPMHandles) keyOpts() []tpm.TPMOption {
	var opts []tpm.TPMOption
	if h.keyIndex != "" {
		opts = append(opts, tpm.WithIndex(h.keyIndex))
	}
	if h.device != "" {
		opts = append(opts, tpm.WithDevice(h.device))
	}

	return opts
}

// localTPMPassphrase is the passphrase kept in TPM NV memory for local
// encryption, together with the TPM calls that reach it. The calls are fields
// rather than direct references so the read and write halves can be exercised
// against the go-tpm simulator, which is the only way to prove they agree on a
// handle without real TPM hardware.
type localTPMPassphrase struct {
	handles localTPMHandles

	readBlob    func(...tpm.TPMOption) ([]byte, error)
	storeBlob   func([]byte, ...tpm.TPMOption) error
	encryptBlob func([]byte, ...tpm.TPMOption) ([]byte, error)
	decryptBlob func([]byte, ...tpm.TPMOption) ([]byte, error)
}

func newLocalTPMPassphrase(nvIndex, cIndex, tpmDevice string) localTPMPassphrase {
	return localTPMPassphrase{
		handles:     resolveLocalTPMHandles(nvIndex, cIndex, tpmDevice),
		readBlob:    tpm.ReadBlob,
		storeBlob:   tpm.StoreBlob,
		encryptBlob: tpm.EncryptBlob,
		decryptBlob: tpm.DecryptBlob,
	}
}

// getOrCreateLocalTPMPassphrase retrieves a passphrase from TPM NV memory, or generates and stores one if it doesn't exist.
// This is used for local encryption (non-UKI mode without remote KMS).
// Logic moved from kcrypt-challenger/cmd/discovery/client/enc.go.
func getOrCreateLocalTPMPassphrase(nvIndex, cIndex, tpmDevice string) (string, error) {
	return newLocalTPMPassphrase(nvIndex, cIndex, tpmDevice).getOrCreate()
}

func (p localTPMPassphrase) getOrCreate() (string, error) {
	encodedPass, err := p.readBlob(p.handles.nvOpts()...)
	if err != nil {
		return p.generateAndStore()
	}

	pass, err := p.decryptBlob(encodedPass, p.handles.keyOpts()...)
	return string(pass), err
}

// generateAndStore generates a new random passphrase and stores it in TPM NV memory.
func (p localTPMPassphrase) generateAndStore() (string, error) {
	rand := utils.RandomString(32)

	blob, err := p.encryptBlob([]byte(rand), p.handles.keyOpts()...)
	if err != nil {
		return "", err
	}

	return rand, p.storeBlob(blob, p.handles.nvOpts()...)
}
