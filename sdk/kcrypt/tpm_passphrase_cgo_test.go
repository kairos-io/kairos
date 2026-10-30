//go:build cgo

// The go-tpm simulator these tests run against is a cgo package, so the whole
// file drops out of a CGO_ENABLED=0 build rather than failing in it. CI builds
// with cgo, which is where the round trip is covered.

package kcrypt

import (
	"testing"

	"github.com/kairos-io/tpm-helpers"
)

// emulatedLocalTPMPassphrase runs the real passphrase code against the go-tpm
// simulator by appending tpm.EmulatedTPM to whatever options the code under
// test chose. The options themselves stay untouched, so a handle the code gets
// wrong is still wrong here.
func emulatedLocalTPMPassphrase(nvIndex, cIndex string) localTPMPassphrase {
	p := newLocalTPMPassphrase(nvIndex, cIndex, "")
	p.readBlob = func(opts ...tpm.TPMOption) ([]byte, error) {
		return tpm.ReadBlob(append(opts, tpm.EmulatedTPM)...)
	}
	p.storeBlob = func(blob []byte, opts ...tpm.TPMOption) error {
		return tpm.StoreBlob(blob, append(opts, tpm.EmulatedTPM)...)
	}
	p.encryptBlob = func(blob []byte, opts ...tpm.TPMOption) ([]byte, error) {
		return tpm.EncryptBlob(blob, append(opts, tpm.EmulatedTPM)...)
	}
	p.decryptBlob = func(blob []byte, opts ...tpm.TPMOption) ([]byte, error) {
		return tpm.DecryptBlob(blob, append(opts, tpm.EmulatedTPM)...)
	}

	return p
}

// TestGetOrCreateLocalTPMPassphraseRoundTrip is the regression test for the
// passphrase being encrypted with the default key handle while the next boot
// decrypts it with the configured c_index. The first call creates and stores
// it, the second reads it back the way a reboot does.
func TestGetOrCreateLocalTPMPassphraseRoundTrip(t *testing.T) {
	tests := []struct {
		name            string
		nvIndex, cIndex string
	}{
		{
			name:    "default key handle",
			nvIndex: "0x1500020",
		},
		{
			name:    "configured c_index",
			nvIndex: "0x1500021",
			cIndex:  "0x81000020",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer tpm.CloseEmulatedDevice()

			created, err := emulatedLocalTPMPassphrase(tt.nvIndex, tt.cIndex).getOrCreate()
			if err != nil {
				t.Fatalf("creating the passphrase: %v", err)
			}
			if created == "" {
				t.Fatal("created an empty passphrase")
			}

			read, err := emulatedLocalTPMPassphrase(tt.nvIndex, tt.cIndex).getOrCreate()
			if err != nil {
				t.Fatalf("reading the passphrase back: %v", err)
			}
			if read != created {
				t.Errorf("read back %q, want the stored %q", read, created)
			}
		})
	}
}
