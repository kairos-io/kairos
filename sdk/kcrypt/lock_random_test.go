package kcrypt

import (
	"strings"
	"testing"
)

// The passphrase created here is what the LUKS container is formatted with in
// luksifyMeasurements, and it survives in the header on every path that
// returns before `systemd-cryptenroll --wipe-slot=password` runs.
func TestGetRandomStringLengthAndAlphabet(t *testing.T) {
	pass := getRandomString(32)
	if len(pass) != 32 {
		t.Fatalf("got %d characters, want 32", len(pass))
	}
	for _, r := range pass {
		if !strings.ContainsRune(passphraseCharset, r) {
			t.Fatalf("%q is not in the passphrase charset", r)
		}
	}
}

func TestGetRandomStringDoesNotRepeat(t *testing.T) {
	const samples = 1000
	seen := make(map[string]struct{}, samples)
	for i := 0; i < samples; i++ {
		pass := getRandomString(32)
		if _, dup := seen[pass]; dup {
			t.Fatalf("call %d produced the passphrase %q a second time", i, pass)
		}
		seen[pass] = struct{}{}
	}
}
