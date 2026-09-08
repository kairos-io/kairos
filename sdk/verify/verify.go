// Package verify gives every download-and-execute (or download-and-extract)
// call site in this tree one place to check a remote artifact against a
// known sha256 digest before it is trusted, instead of each site inventing
// its own ad-hoc checksum handling.
//
// Downloads go through sdk/types/http.Client — the same download interface
// (and, in tests, the same agent/tests/mocks.FakeHTTPClient) every other
// download call site in agent/pkg already depends on — rather than this
// package rolling its own *http.Client, so callers share one client
// implementation and one mock instead of two.
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
	"os"
	"regexp"
	"strings"

	sdkhttp "github.com/kairos-io/kairos/v4/sdk/types/http"
	"github.com/kairos-io/kairos/v4/sdk/types/logger"
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

// VerifiedDownload downloads url through client to destination and verifies
// the downloaded content's sha256 digest matches want before leaving it in
// place. It fails closed: a malformed want, a download error, or a digest
// mismatch all leave no verified content at destination — on a mismatch it
// removes whatever client.GetURL wrote before returning, so a tampered,
// truncated, or wrong-version artifact is never left where a caller expects
// a verified one.
func VerifiedDownload(client sdkhttp.Client, log logger.KairosLogger, url, destination string, want SHA256Sum) error {
	if !want.Valid() {
		return fmt.Errorf("verify: refusing to download %s: %q is not a valid sha256 digest", url, want)
	}

	if err := client.GetURL(log, url, destination); err != nil {
		return fmt.Errorf("verify: download %s: %w", url, err)
	}

	got, err := sha256OfFile(destination)
	if err != nil {
		_ = os.Remove(destination)
		return fmt.Errorf("verify: hash %s: %w", destination, err)
	}
	if !strings.EqualFold(string(got), string(want)) {
		_ = os.Remove(destination)
		return fmt.Errorf("verify: %s has sha256 %s, want %s", url, got, want)
	}

	return nil
}

// sha256OfFile hashes the file at path without holding its whole content in
// memory at once, so verifying a large release tarball does not cost as
// much as the tarball's size in RAM on top of the copy already on disk.
func sha256OfFile(path string) (SHA256Sum, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return SHA256Sum(hex.EncodeToString(h.Sum(nil))), nil
}

// FetchChecksums does a plain (unverified) fetch of url through client and
// returns its content. It exists for the one thing in this package that
// cannot itself be checksum-verified: the checksums listing itself. Callers
// are expected to fetch it from an origin they trust more than the
// artifact's own host — e.g. the upstream project's own repo over TLS — and
// to treat a pinned release tag as the actual security boundary, the same
// way go.sum pins a module version rather than re-verifying the registry
// that serves it.
//
// The listing is fetched to a scratch temp file (via the same client.GetURL
// every verified download in this package uses) and removed once read, since
// client's interface is destination-based rather than returning a body.
func FetchChecksums(client sdkhttp.Client, log logger.KairosLogger, url string) ([]byte, error) {
	tmp, err := os.CreateTemp("", "kairos-checksums-*")
	if err != nil {
		return nil, fmt.Errorf("verify: fetch checksums %s: %w", url, err)
	}
	tmpName := tmp.Name()
	_ = tmp.Close()
	defer func() { _ = os.Remove(tmpName) }()

	if err := client.GetURL(log, url, tmpName); err != nil {
		return nil, fmt.Errorf("verify: fetch checksums %s: %w", url, err)
	}

	body, err := os.ReadFile(tmpName)
	if err != nil {
		return nil, fmt.Errorf("verify: fetch checksums %s: %w", url, err)
	}
	return body, nil
}

// ChecksumFromSumsFile parses a goreleaser-style checksums listing — lines
// of "<sha256>  <filename>", one entry per released artifact, the format
// kairos-io's own release pipelines publish (either as a per-component
// "*-checksums.txt" sibling, pre-monorepo, or as the single shared
// "checksums.txt" every kairos-io/kairos release publishes today) — and that
// k3s-io/k3s publishes as "install.sh.sha256sum", and k0sproject/k0s
// publishes as "sha256sums.txt" — and returns the digest recorded for
// filename. Matching is case-insensitive on the filename since GitHub's
// release asset download URLs are themselves case-insensitive.
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
