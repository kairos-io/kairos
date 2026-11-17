package secrets

import (
	"crypto/rand"
	"path/filepath"
	"strings"
	"testing"
)

func newKey(t *testing.T) []byte {
	t.Helper()
	k := make([]byte, keySize)
	if _, err := rand.Read(k); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return k
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	c, err := NewCipher(newKey(t))
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}

	plaintext := "s3cr3t-p@ssw0rd"
	ct, err := c.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if ct == plaintext {
		t.Fatal("ciphertext must not equal plaintext")
	}
	if strings.Contains(ct, plaintext) {
		t.Fatal("ciphertext must not contain plaintext")
	}

	got, err := c.Decrypt(ct)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if got != plaintext {
		t.Fatalf("round-trip mismatch: got %q want %q", got, plaintext)
	}
}

func TestEncryptUsesRandomNonce(t *testing.T) {
	c, err := NewCipher(newKey(t))
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}
	a, _ := c.Encrypt("same")
	b, _ := c.Encrypt("same")
	if a == b {
		t.Fatal("two encryptions of the same plaintext must differ (random nonce)")
	}
}

func TestEmptyPassword(t *testing.T) {
	c, err := NewCipher(newKey(t))
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}
	ct, err := c.Encrypt("")
	if err != nil {
		t.Fatalf("Encrypt empty: %v", err)
	}
	if ct != "" {
		t.Fatalf("empty plaintext must encrypt to empty string, got %q", ct)
	}
	pt, err := c.Decrypt("")
	if err != nil {
		t.Fatalf("Decrypt empty: %v", err)
	}
	if pt != "" {
		t.Fatalf("empty ciphertext must decrypt to empty string, got %q", pt)
	}
}

func TestWrongKeyFailsClosed(t *testing.T) {
	c1, _ := NewCipher(newKey(t))
	c2, _ := NewCipher(newKey(t))

	ct, err := c1.Encrypt("topsecret")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if _, err := c2.Decrypt(ct); err == nil {
		t.Fatal("decrypting with the wrong key must fail, not return garbage")
	}
}

func TestDecryptRejectsTamperedAndMalformed(t *testing.T) {
	c, _ := NewCipher(newKey(t))
	if _, err := c.Decrypt("not-base64!!!"); err == nil {
		t.Fatal("non-base64 ciphertext must error")
	}
	if _, err := c.Decrypt("AAAA"); err == nil {
		t.Fatal("too-short ciphertext must error")
	}
}

func TestNewCipherRejectsBadKeyLength(t *testing.T) {
	if _, err := NewCipher(make([]byte, 16)); err == nil {
		t.Fatal("a 16-byte key must be rejected")
	}
}

func TestLoadOrGenerateCipher(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bmc-key")

	c1, err := LoadOrGenerateCipher(path)
	if err != nil {
		t.Fatalf("first LoadOrGenerateCipher: %v", err)
	}
	ct, err := c1.Encrypt("hello")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	// A second load of the same file must reuse the persisted DEK and decrypt the
	// earlier ciphertext.
	c2, err := LoadOrGenerateCipher(path)
	if err != nil {
		t.Fatalf("second LoadOrGenerateCipher: %v", err)
	}
	got, err := c2.Decrypt(ct)
	if err != nil {
		t.Fatalf("Decrypt across reloads: %v", err)
	}
	if got != "hello" {
		t.Fatalf("got %q want %q", got, "hello")
	}
}
