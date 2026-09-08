package verify_test

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	httpimpl "github.com/kairos-io/kairos/v4/agent/pkg/implementations/http"
	"github.com/kairos-io/kairos/v4/sdk/types/logger"
	"github.com/kairos-io/kairos/v4/sdk/verify"
)

// client is the same sdk/types/http.Client implementation every real
// download call site in this tree uses; tests exercise it against local
// httptest servers rather than faking the interface, since the point under
// test here is VerifiedDownload/FetchChecksums' own hashing and error
// handling, not whether a call was made.
func client() *httpimpl.Client {
	return httpimpl.NewClient()
}

func sumOf(b []byte) verify.SHA256Sum {
	sum := sha256.Sum256(b)
	return verify.SHA256Sum(hex.EncodeToString(sum[:]))
}

func TestVerifiedDownload_HappyPath(t *testing.T) {
	body := []byte("totally legitimate release tarball bytes")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "artifact")
	err := verify.VerifiedDownload(client(), logger.NewNullLogger(), srv.URL, dest, sumOf(body))
	if err != nil {
		t.Fatalf("VerifiedDownload returned error: %v", err)
	}

	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("reading destination: %v", err)
	}
	if string(got) != string(body) {
		t.Fatalf("VerifiedDownload wrote %q, want %q", got, body)
	}
}

func TestVerifiedDownload_TamperedContentMismatch(t *testing.T) {
	served := []byte("this is what the attacker's mirror actually sends")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(served)
	}))
	defer srv.Close()

	// want is pinned to the *expected* content, not what the server serves.
	want := sumOf([]byte("this is what we actually expect"))

	dest := filepath.Join(t.TempDir(), "artifact")
	err := verify.VerifiedDownload(client(), logger.NewNullLogger(), srv.URL, dest, want)
	if err == nil {
		t.Fatal("VerifiedDownload succeeded on tampered content, want error")
	}
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Fatalf("tampered content must not be left at %s", dest)
	}
}

func TestVerifiedDownload_NetworkError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	// Close before the request lands so the client sees a connection error.
	srv.Close()

	validLookingDigest := verify.SHA256Sum(hex.EncodeToString(make([]byte, sha256.Size)))
	dest := filepath.Join(t.TempDir(), "artifact")
	err := verify.VerifiedDownload(client(), logger.NewNullLogger(), url, dest, validLookingDigest)
	if err == nil {
		t.Fatal("VerifiedDownload succeeded against a closed server, want error")
	}
}

func TestVerifiedDownload_NonSuccessStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "artifact")
	err := verify.VerifiedDownload(client(), logger.NewNullLogger(), srv.URL, dest, sumOf([]byte("anything")))
	if err == nil {
		t.Fatal("VerifiedDownload succeeded on a 404, want error")
	}
}

func TestVerifiedDownload_RejectsMalformedWant(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("hello"))
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "artifact")
	err := verify.VerifiedDownload(client(), logger.NewNullLogger(), srv.URL, dest, verify.SHA256Sum("not-a-real-digest"))
	if err == nil {
		t.Fatal("VerifiedDownload succeeded with a malformed digest, want error")
	}
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Fatalf("nothing should be written to %s for a malformed digest", dest)
	}
}

func TestFetchChecksums_HappyPath(t *testing.T) {
	body := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa  something.tar.gz\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	got, err := verify.FetchChecksums(client(), logger.NewNullLogger(), srv.URL)
	if err != nil {
		t.Fatalf("FetchChecksums returned error: %v", err)
	}
	if string(got) != body {
		t.Fatalf("FetchChecksums = %q, want %q", got, body)
	}
}

func TestFetchChecksums_NonSuccessStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	_, err := verify.FetchChecksums(client(), logger.NewNullLogger(), srv.URL)
	if err == nil {
		t.Fatal("FetchChecksums succeeded on a 404, want error")
	}
}

func TestChecksumFromSumsFile(t *testing.T) {
	sums := []byte(
		"0514aaba7e0f037a6254d625ea463dcb6bf0de9ffc34f35bc932dc7d4655dc3d  kairos-agent-v2.31.4-linux-amd64-fips.tar.gz\n" +
			"ee1caddedd70b5aef3b03db80a1121eabb27a338c4be49a83eb68451e2140424  kairos-agent-v2.31.4-linux-amd64.tar.gz\n",
	)

	got, err := verify.ChecksumFromSumsFile(sums, "kairos-agent-v2.31.4-Linux-amd64.tar.gz")
	if err != nil {
		t.Fatalf("ChecksumFromSumsFile returned error: %v", err)
	}
	want := verify.SHA256Sum("ee1caddedd70b5aef3b03db80a1121eabb27a338c4be49a83eb68451e2140424")
	if got != want {
		t.Fatalf("ChecksumFromSumsFile = %q, want %q (case-insensitive filename match)", got, want)
	}
}

func TestChecksumFromSumsFile_NotFound(t *testing.T) {
	sums := []byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa  something-else.tar.gz\n")

	_, err := verify.ChecksumFromSumsFile(sums, "kairos-agent-v2.31.4-linux-amd64.tar.gz")
	if err == nil {
		t.Fatal("ChecksumFromSumsFile succeeded for a filename not in the listing, want error")
	}
}
