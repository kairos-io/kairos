package stages

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// buildTarGz packs a single file named binaryName with the given content into
// a gzipped tar archive, mirroring the shape of a real kairos-io release
// tarball (a single binary at the archive root).
func buildTarGz(t *testing.T, binaryName string, content []byte) []byte {
	t.Helper()

	var tarBuf bytes.Buffer
	gzw := gzip.NewWriter(&tarBuf)
	tw := tar.NewWriter(gzw)

	if err := tw.WriteHeader(&tar.Header{
		Name: binaryName,
		Mode: 0755,
		Size: int64(len(content)),
	}); err != nil {
		t.Fatalf("write tar header: %v", err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatalf("write tar content: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar writer: %v", err)
	}
	if err := gzw.Close(); err != nil {
		t.Fatalf("close gzip writer: %v", err)
	}
	return tarBuf.Bytes()
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// newReleaseServer serves a release tarball at /release.tar.gz and its
// checksums.txt sibling at /checksums.txt, the same layout goreleaser
// produces for a kairos-io (or mudler/edgevpn) release.
func newReleaseServer(t *testing.T, tarball []byte, checksumsBody string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/release.tar.gz", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(tarball)
	})
	mux.HandleFunc("/checksums.txt", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(checksumsBody))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestDownloadAndExtract_VerifiesAgainstChecksumsFile(t *testing.T) {
	want := []byte("#!/bin/sh\necho totally-a-real-binary\n")
	tarball := buildTarGz(t, "somebinary", want)
	checksums := sha256Hex(tarball) + "  release.tar.gz\n"

	srv := newReleaseServer(t, tarball, checksums)

	dir := t.TempDir()
	dest := filepath.Join(dir, "somebinary")

	err := DownloadAndExtract(srv.URL+"/release.tar.gz", srv.URL+"/checksums.txt", dest)
	if err != nil {
		t.Fatalf("DownloadAndExtract returned error: %v", err)
	}

	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("reading extracted binary: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("extracted content = %q, want %q", got, want)
	}
}

func TestDownloadAndExtract_RejectsTamperedTarball(t *testing.T) {
	realTarball := buildTarGz(t, "somebinary", []byte("the real thing"))
	tamperedTarball := buildTarGz(t, "somebinary", []byte("a malicious substitute"))
	// The checksums file (as fetched from the trusted origin) records the
	// digest of the REAL tarball, but the server actually hands back the
	// tampered one — simulating a compromised or MITM'd release host.
	checksums := sha256Hex(realTarball) + "  release.tar.gz\n"

	srv := newReleaseServer(t, tamperedTarball, checksums)

	dir := t.TempDir()
	dest := filepath.Join(dir, "somebinary")

	err := DownloadAndExtract(srv.URL+"/release.tar.gz", srv.URL+"/checksums.txt", dest)
	if err == nil {
		t.Fatal("DownloadAndExtract succeeded against a tampered tarball, want error")
	}
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Fatalf("tampered content must not be written to %s", dest)
	}
}

func TestDownloadAndExtract_MissingChecksumEntry(t *testing.T) {
	tarball := buildTarGz(t, "somebinary", []byte("content"))
	// checksums.txt exists but has no entry for this artifact's filename.
	checksums := sha256Hex([]byte("unrelated")) + "  some-other-file.tar.gz\n"

	srv := newReleaseServer(t, tarball, checksums)

	dir := t.TempDir()
	dest := filepath.Join(dir, "somebinary")

	err := DownloadAndExtract(srv.URL+"/release.tar.gz", srv.URL+"/checksums.txt", dest)
	if err == nil {
		t.Fatal("DownloadAndExtract succeeded with no matching checksum entry, want error")
	}
}
