package verify_test

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
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

// TestVerifiedDownload_StaleDestinationIsReplaced covers a caller that
// passes a fixed path rather than a temp file — the shape
// provider/internal/provider/buildEvent.go's k0sBinaryDest has, where a
// previous run's artifact is already on disk. The download path goes
// through the grab-backed sdkhttp.Client, whose grab.Request defaults
// NoResume to false, so a file left at destination would otherwise be
// treated as a partial copy of this url and continued with a Range
// request: grab HEADs first, and finding the local file *larger* than what
// the remote now reports it fails closed with grab.ErrBadLength before
// ever opening destination for writing. VerifiedDownload clears
// destination up front, so the stale bytes cannot poison the request and
// the download runs to completion against the current remote.
func TestVerifiedDownload_StaleDestinationIsReplaced(t *testing.T) {
	newBody := []byte("new release bytes")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Advertise range support and a Content-Length shorter than the
		// stale local file below: the combination that makes grab's
		// HEAD-driven resume check bail out instead of performing a GET.
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(newBody)))
		if r.Method == http.MethodHead {
			return
		}
		_, _ = w.Write(newBody)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "artifact")
	staleBody := []byte("stale k0s binary bytes left over from a previous, unrelated install run")
	if err := os.WriteFile(dest, staleBody, 0o644); err != nil {
		t.Fatalf("seeding stale destination: %v", err)
	}

	err := verify.VerifiedDownload(client(), logger.NewNullLogger(), srv.URL, dest, sumOf(newBody))
	if err != nil {
		t.Fatalf("VerifiedDownload over a stale destination returned error: %v", err)
	}

	got, readErr := os.ReadFile(dest)
	if readErr != nil {
		t.Fatalf("reading destination: %v", readErr)
	}
	if string(got) != string(newBody) {
		t.Fatalf("VerifiedDownload left %q at destination, want the freshly downloaded %q", got, newBody)
	}
}

// TestVerifiedDownload_PartialDownloadRemovedOnError covers the other half
// of the same contract: a download that fails part-way through has already
// written bytes to destination, and those bytes are unverified. The
// download-error path removes them, so no caller finds a truncated
// artifact where it expects a verified one.
func TestVerifiedDownload_PartialDownloadRemovedOnError(t *testing.T) {
	fullBody := []byte("the complete release artifact, all of these bytes")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Promise the full length, then hand over a prefix and hang up, so
		// the client writes a partial file and then sees an unexpected EOF.
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(fullBody)))
		if r.Method == http.MethodHead {
			return
		}
		_, _ = w.Write(fullBody[:10])
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "artifact")
	err := verify.VerifiedDownload(client(), logger.NewNullLogger(), srv.URL, dest, sumOf(fullBody))
	if err == nil {
		t.Fatal("VerifiedDownload succeeded on a truncated response, want error")
	}

	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Fatalf("partially downloaded content must not be left at %s (stat error was %v)", dest, statErr)
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
