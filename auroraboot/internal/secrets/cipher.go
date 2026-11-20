// Package secrets provides a small AES-256-GCM helper used to encrypt sensitive
// values (e.g. BMC passwords) before they are persisted at rest. The data
// encryption key (DEK) is a 32-byte key loaded from, or generated into, a
// 0600 file under the server's secrets directory.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
)

// keySize is the AES-256 key length in bytes.
const keySize = 32

// Cipher encrypts and decrypts short secrets with AES-256-GCM. The nonce is
// randomly generated per call and prepended to the ciphertext; the whole blob is
// base64 (standard, padded) encoded. A Cipher is safe for concurrent use.
type Cipher struct {
	aead cipher.AEAD
}

// NewCipher builds a Cipher from a 32-byte data encryption key.
func NewCipher(key []byte) (*Cipher, error) {
	if len(key) != keySize {
		return nil, fmt.Errorf("DEK must be %d bytes, got %d", keySize, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("creating AES cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("creating GCM: %w", err)
	}
	return &Cipher{aead: aead}, nil
}

// LoadOrGenerateCipher loads the 32-byte DEK from path, or generates a new one
// and persists it as a 0600 file when the file does not yet exist. The parent
// directory is assumed to already exist (the caller creates the 0700 secrets
// dir). The DEK is never logged.
func LoadOrGenerateCipher(path string) (*Cipher, error) {
	key, err := os.ReadFile(path)
	switch {
	case err == nil:
		if len(key) != keySize {
			return nil, fmt.Errorf("DEK file %s is %d bytes, expected %d", path, len(key), keySize)
		}
	case errors.Is(err, os.ErrNotExist):
		key = make([]byte, keySize)
		if _, err := rand.Read(key); err != nil {
			return nil, fmt.Errorf("generating DEK: %w", err)
		}
		if err := os.WriteFile(path, key, 0600); err != nil {
			return nil, fmt.Errorf("persisting DEK to %s: %w", path, err)
		}
	default:
		return nil, fmt.Errorf("reading DEK %s: %w", path, err)
	}
	return NewCipher(key)
}

// Encrypt seals plaintext and returns nonce||ciphertext, base64-encoded. An empty
// plaintext returns an empty string so callers can store "no password" without an
// encrypted blob of empty bytes.
func (c *Cipher) Encrypt(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generating nonce: %w", err)
	}
	sealed := c.aead.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// Decrypt reverses Encrypt. An empty string decrypts to an empty string. A blob
// that is not valid base64, is too short, or fails authentication returns an
// error (fail closed): callers must not treat a decrypt failure as success.
func (c *Cipher) Decrypt(ciphertext string) (string, error) {
	if ciphertext == "" {
		return "", nil
	}
	raw, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", fmt.Errorf("decoding ciphertext: %w", err)
	}
	ns := c.aead.NonceSize()
	if len(raw) < ns {
		return "", errors.New("ciphertext too short")
	}
	nonce, sealed := raw[:ns], raw[ns:]
	plaintext, err := c.aead.Open(nil, nonce, sealed, nil)
	if err != nil {
		return "", fmt.Errorf("decrypting: %w", err)
	}
	return string(plaintext), nil
}
