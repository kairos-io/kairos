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

	httpimpl "github.com/kairos-io/kairos/v4/agent/pkg/implementations/http"
	"github.com/kairos-io/kairos/v4/sdk/types/logger"
)

func testClient() *httpimpl.Client {
	return httpimpl.NewClient()
}

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

	err := DownloadAndExtract(testClient(), logger.NewNullLogger(), srv.URL+"/release.tar.gz", srv.URL+"/checksums.txt", dest)
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

	err := DownloadAndExtract(testClient(), logger.NewNullLogger(), srv.URL+"/release.tar.gz", srv.URL+"/checksums.txt", dest)
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

	err := DownloadAndExtract(testClient(), logger.NewNullLogger(), srv.URL+"/release.tar.gz", srv.URL+"/checksums.txt", dest)
	if err == nil {
		t.Fatal("DownloadAndExtract succeeded with no matching checksum entry, want error")
	}
}

// TestMonorepoBinaryURLs_KnownAssets locks down the URL shape every
// kairos-io-owned binary resolves against today: a single shared
// checksums.txt per kairos-io/kairos release tag, not a per-component
// "*-checksums.txt" sibling — the thing the old per-repo resolution got
// wrong after the monorepo consolidation.
func TestMonorepoBinaryURLs_KnownAssets(t *testing.T) {
	cases := []struct {
		name       string
		reponame   string
		fips       bool
		wantAsset  string
		wantBinary string
	}{
		{
			name:       "agent resolves to the shared kairos multi-call tarball",
			reponame:   "kairos-agent",
			wantAsset:  "https://github.com/kairos-io/kairos/releases/download/v4.3.0/kairos-v4.3.0-linux-amd64.tar.gz",
			wantBinary: "kairos",
		},
		{
			name:       "immucore resolves to the shared kairos multi-call tarball",
			reponame:   "immucore",
			wantAsset:  "https://github.com/kairos-io/kairos/releases/download/v4.3.0/kairos-v4.3.0-linux-amd64.tar.gz",
			wantBinary: "kairos",
		},
		{
			name:       "kcrypt-discovery-challenger renamed to kcrypt-challenger",
			reponame:   "kcrypt-discovery-challenger",
			wantAsset:  "https://github.com/kairos-io/kairos/releases/download/v4.3.0/kcrypt-challenger-v4.3.0-linux-amd64.tar.gz",
			wantBinary: "",
		},
		{
			name:       "provider-kairos keeps its own name",
			reponame:   "provider-kairos",
			wantAsset:  "https://github.com/kairos-io/kairos/releases/download/v4.3.0/provider-kairos-v4.3.0-linux-amd64.tar.gz",
			wantBinary: "",
		},
		{
			name:       "fips appends the -fips suffix before .tar.gz",
			reponame:   "provider-kairos",
			fips:       true,
			wantAsset:  "https://github.com/kairos-io/kairos/releases/download/v4.3.0/provider-kairos-v4.3.0-linux-amd64-fips.tar.gz",
			wantBinary: "",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotAsset, gotChecksums, gotBinary, ok := monorepoBinaryURLs(c.reponame, "v4.3.0", "amd64", c.fips)
			if !ok {
				t.Fatalf("monorepoBinaryURLs(%q) reported no mapping, want one", c.reponame)
			}
			if gotAsset != c.wantAsset {
				t.Fatalf("asset URL = %q, want %q", gotAsset, c.wantAsset)
			}
			wantChecksums := "https://github.com/kairos-io/kairos/releases/download/v4.3.0/checksums.txt"
			if gotChecksums != wantChecksums {
				t.Fatalf("checksums URL = %q, want %q (one shared file per release, not a per-component sibling)", gotChecksums, wantChecksums)
			}
			if gotBinary != c.wantBinary {
				t.Fatalf("binary name = %q, want %q", gotBinary, c.wantBinary)
			}
		})
	}
}

// TestMonorepoBinaryURLs_UnknownReponameFallsBack ensures a reponame with no
// monorepo mapping is rejected with ok=false and no URLs, rather than
// guessing (e.g. mudler/edgevpn, which was never part of the monorepo and is
// routed to downloadLegacyPerComponentBinary directly by its own org check,
// never through this function).
func TestMonorepoBinaryURLs_UnknownReponameFallsBack(t *testing.T) {
	_, _, _, ok := monorepoBinaryURLs("edgevpn", "v0.35.5", "amd64", false)
	if ok {
		t.Fatal("monorepoBinaryURLs(\"edgevpn\") reported a mapping, want none (edgevpn is not part of the monorepo)")
	}
}

// TestLegacyPerComponentURLs matches the archived pre-monorepo repos' own
// goreleaser layout: each publishes its own "*-checksums.txt" sibling next
// to its own tarball, under its own repo.
func TestLegacyPerComponentURLs(t *testing.T) {
	gotAsset, gotChecksums := legacyPerComponentURLs("kairos-io", "kairos-agent", "v2.31.4", "amd64", false)
	wantAsset := "https://github.com/kairos-io/kairos-agent/releases/download/v2.31.4/kairos-agent-v2.31.4-Linux-amd64.tar.gz"
	wantChecksums := "https://github.com/kairos-io/kairos-agent/releases/download/v2.31.4/kairos-agent-v2.31.4-checksums.txt"
	if gotAsset != wantAsset {
		t.Fatalf("asset URL = %q, want %q", gotAsset, wantAsset)
	}
	if gotChecksums != wantChecksums {
		t.Fatalf("checksums URL = %q, want %q", gotChecksums, wantChecksums)
	}

	// mudler/edgevpn uses a different org and is never routed through the
	// monorepo mapping at all.
	gotAsset, gotChecksums = legacyPerComponentURLs("mudler", "edgevpn", "v0.35.5", "x86_64", false)
	wantAsset = "https://github.com/mudler/edgevpn/releases/download/v0.35.5/edgevpn-v0.35.5-Linux-x86_64.tar.gz"
	wantChecksums = "https://github.com/mudler/edgevpn/releases/download/v0.35.5/edgevpn-v0.35.5-checksums.txt"
	if gotAsset != wantAsset {
		t.Fatalf("asset URL = %q, want %q", gotAsset, wantAsset)
	}
	if gotChecksums != wantChecksums {
		t.Fatalf("checksums URL = %q, want %q", gotChecksums, wantChecksums)
	}
}
