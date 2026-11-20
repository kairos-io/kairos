package secureboot

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGenerateKeySet_RefusesExistingKeys(t *testing.T) {
	tmp := t.TempDir()
	opts := Options{Name: "auroraboot-test", OutputDir: tmp, SkipMicrosoftCerts: true}
	if err := GenerateKeySet(opts); err != nil {
		t.Fatalf("first GenerateKeySet: %v", err)
	}

	files := []string{"PK.key", "KEK.key", "db.key", "tpm2-pcr-private.pem"}
	before := map[string][]byte{}
	for _, f := range files {
		b, err := os.ReadFile(filepath.Join(tmp, f))
		if err != nil {
			t.Fatal(err)
		}
		before[f] = b
	}

	if err := GenerateKeySet(opts); err == nil {
		t.Fatal("second GenerateKeySet into the same directory succeeded, want an error")
	}
	for _, f := range files {
		b, err := os.ReadFile(filepath.Join(tmp, f))
		if err != nil {
			t.Fatal(err)
		}
		if string(b) != string(before[f]) {
			t.Errorf("%s changed after a refused GenerateKeySet", f)
		}
	}

	opts.Overwrite = true
	if err := GenerateKeySet(opts); err != nil {
		t.Fatalf("GenerateKeySet with Overwrite: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(tmp, "PK.key"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) == string(before["PK.key"]) {
		t.Error("PK.key not replaced with Overwrite set")
	}
}

func TestGenerateKeySet_RefusesLoneExistingPCRKey(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "tpm2-pcr-private.pem"), []byte("ORIGINAL"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := GenerateKeySet(Options{Name: "auroraboot-test", OutputDir: tmp, SkipMicrosoftCerts: true})
	if err == nil {
		t.Fatal("GenerateKeySet succeeded over an existing tpm2-pcr-private.pem, want an error")
	}
	if b, _ := os.ReadFile(filepath.Join(tmp, "tpm2-pcr-private.pem")); string(b) != "ORIGINAL" {
		t.Error("tpm2-pcr-private.pem was changed")
	}
}
