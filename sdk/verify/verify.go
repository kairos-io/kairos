// Package verify gives every download-and-execute (or download-and-extract)
// call site in this tree one place to check a remote artifact against a
// known sha256 digest before it is trusted, instead of each site inventing
// its own ad-hoc checksum handling.
//
// It intentionally does not retry: a caller that needs a retry-with-backoff
// loop around VerifiedDownload should wrap it with a general-purpose retry
// helper (e.g. avast/retry-go) rather than have this package grow one.
package verify

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// SHA256Sum is a lowercase hex-encoded sha256 digest (64 hex characters).
type SHA256Sum string

var hexSHA256 = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Valid reports whether s is a well-formed sha256 digest. It does not check
// that the digest matches anything — only that it is shaped like one, so a
// caller cannot accidentally pass an empty string through and have every
// comparison against it silently succeed.
func (s SHA256Sum) Valid() bool {
	return hexSHA256.MatchString(strings.ToLower(string(s)))
}

// HTTPClient is used for every request this package makes. It carries a
// deliberately finite timeout: every call site in this tree fetches a
// single small artifact (an installer script, a release tarball, a
// manifest), never a stream an origin is entitled to hold open forever.
// Tests may point it at an httptest.Server; production code may swap it
// out for one with different transport settings.
var HTTPClient = &http.Client{Timeout: 60 * time.Second}

// VerifiedDownload fetches url's body, hashing it as it streams off the
// wire, and returns the bytes only if the computed sha256 digest matches
// want exactly. It fails closed: a malformed want, a network error, a
// non-2xx status, or a digest mismatch all return an error and no bytes, so
// a tampered, truncated, or wrong-version artifact never reaches a caller.
func VerifiedDownload(url string, want SHA256Sum) ([]byte, error) {
	if !want.Valid() {
		return nil, fmt.Errorf("verify: refusing to download %s: %q is not a valid sha256 digest", url, want)
	}

	resp, err := HTTPClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("verify: download %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("verify: download %s: unexpected status %s", url, resp.Status)
	}

	digest := sha256.New()
	var body bytes.Buffer
	if _, err := io.Copy(io.MultiWriter(&body, digest), resp.Body); err != nil {
		return nil, fmt.Errorf("verify: download %s: %w", url, err)
	}

	got := SHA256Sum(hex.EncodeToString(digest.Sum(nil)))
	if !strings.EqualFold(string(got), string(want)) {
		return nil, fmt.Errorf("verify: %s has sha256 %s, want %s", url, got, want)
	}

	return body.Bytes(), nil
}

// FetchChecksums does a plain (unverified) GET of url and returns the body.
// It exists for the one thing in this package that cannot itself be
// checksum-verified: the checksums listing itself. Callers are expected to
// fetch it from an origin they trust more than the artifact's own host —
// e.g. the upstream project's own repo over TLS — and to treat a pinned
// release tag as the actual security boundary, the same way go.sum pins a
// module version rather than re-verifying the registry that serves it.
func FetchChecksums(url string) ([]byte, error) {
	resp, err := HTTPClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("verify: fetch checksums %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("verify: fetch checksums %s: unexpected status %s", url, resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("verify: fetch checksums %s: %w", url, err)
	}
	return body, nil
}

// ChecksumFromSumsFile parses a goreleaser-style checksums listing — lines
// of "<sha256>  <filename>", one entry per released artifact, the format
// kairos-io's own release pipelines publish as "*-checksums.txt" and that
// k3s-io/k3s publishes as "install.sh.sha256sum" — and returns the digest
// recorded for filename. Matching is case-insensitive on the filename since
// GitHub's release asset download URLs are themselves case-insensitive.
func ChecksumFromSumsFile(sums []byte, filename string) (SHA256Sum, error) {
	scanner := bufio.NewScanner(bytes.NewReader(sums))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 2 {
			continue
		}
		// The reference sha256sum(1) format prefixes a leading "*" on the
		// filename for binary mode; strip it before comparing.
		name := strings.TrimPrefix(fields[1], "*")
		if strings.EqualFold(name, filename) {
			return SHA256Sum(strings.ToLower(fields[0])), nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("verify: parse checksums: %w", err)
	}
	return "", fmt.Errorf("verify: no checksum entry for %q", filename)
}
