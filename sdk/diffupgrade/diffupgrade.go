// Package diffupgrade defines the artifact a diff upgrade applies, and the two
// halves that produce and consume it.
//
// A diff is an ordinary OCI image with a single layer. The layer holds every
// file of the target release that is new or different from the base release,
// plus a manifest at ManifestPath. The manifest names the base the diff was
// computed against and lists the paths the target no longer has.
//
// Deletions travel in the manifest rather than as OCI whiteouts on purpose. The
// agent unpacks images by flattening their layers, and flattening consumes
// whiteouts instead of writing them out, so a whiteout would never reach the
// root filesystem it was meant for. With the list in a plain file the diff
// unpacks through the same extractor, registry credentials, retries and cosign
// verification as a full image does.
//
// The diff is applied onto a copy of the base image. If anything about it is
// wrong, the copy is thrown away and the caller does a full upgrade instead.
package diffupgrade

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"
)

const (
	// ManifestDir is the directory, relative to the root of the diff, that
	// holds the manifest. Apply removes it once the diff is applied, so the
	// upgraded system does not carry it.
	ManifestDir = ".kairos-diff"
	// ManifestPath is the manifest, relative to the root of the diff.
	ManifestPath = ManifestDir + "/manifest.json"
	// FormatVersion is the manifest version this package writes and reads.
	FormatVersion = 1
	// TagSuffix is appended to the tag of a full image to name its diff, so
	// quay.io/kairos/hadron:v1.2.0 has its diff at
	// quay.io/kairos/hadron:v1.2.0.diff.
	TagSuffix = ".diff"
	// BaseLabel is the image config label that carries Manifest.Base, so the
	// base of a diff can be read from a registry without pulling it.
	BaseLabel = "io.kairos.diff.base"

	// fingerprintFile is the file whose checksum identifies a release.
	fingerprintFile = "etc/kairos-release"
)

// ErrBaseMismatch is returned by Apply when the diff was computed against a
// different release than the one it is being applied to.
var ErrBaseMismatch = errors.New("diff base does not match the system it is applied to")

// Manifest describes a diff.
type Manifest struct {
	// Version is the manifest format version.
	Version int `json:"version"`
	// Base is the Fingerprint of the release the diff was computed against.
	Base string `json:"base"`
	// Deleted lists the paths, relative to the root, that the base has and
	// the target does not. A deleted directory is listed once, not with its
	// contents.
	Deleted []string `json:"deleted,omitempty"`
}

// readFS is the part of a filesystem Fingerprint needs.
type readFS interface {
	ReadFile(name string) ([]byte, error)
}

// applyFS is the part of a filesystem Apply needs.
type applyFS interface {
	readFS
	RemoveAll(path string) error
}

// Fingerprint identifies the release installed under root by the checksum of
// its /etc/kairos-release. Every Kairos build writes that file with the
// version, flavor and build tooling of the image, so two images with the same
// fingerprint were built as the same release.
func Fingerprint(fs readFS, root string) (string, error) {
	data, err := fs.ReadFile(filepath.Join(root, fingerprintFile))
	if err != nil {
		return "", fmt.Errorf("reading the release fingerprint: %w", err)
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// ReadManifest reads the manifest of a diff unpacked under root.
func ReadManifest(fs readFS, root string) (*Manifest, error) {
	data, err := fs.ReadFile(filepath.Join(root, ManifestPath))
	if err != nil {
		return nil, fmt.Errorf("reading the diff manifest: %w", err)
	}
	m := &Manifest{}
	if err := json.Unmarshal(data, m); err != nil {
		return nil, fmt.Errorf("parsing the diff manifest: %w", err)
	}
	if m.Version != FormatVersion {
		return nil, fmt.Errorf("diff manifest version %d is not supported, expected %d", m.Version, FormatVersion)
	}
	return m, nil
}

// Apply finishes a diff that has been unpacked under root on top of a copy of
// its base. base is the Fingerprint that root had before the diff was
// unpacked, which the caller has to take first, because the diff overwrites
// the file it is computed from.
//
// Apply checks the manifest against base, removes the deleted paths and then
// the manifest itself. On error root is in an undefined state and has to be
// discarded.
func Apply(fs applyFS, root, base string) error {
	m, err := ReadManifest(fs, root)
	if err != nil {
		return err
	}
	if m.Base != base {
		return fmt.Errorf("%w: diff is for %s, system is %s", ErrBaseMismatch, m.Base, base)
	}
	for _, p := range m.Deleted {
		clean, err := relativePath(p)
		if err != nil {
			return err
		}
		if err := fs.RemoveAll(filepath.Join(root, clean)); err != nil {
			return fmt.Errorf("removing %s: %w", p, err)
		}
	}
	return fs.RemoveAll(filepath.Join(root, ManifestDir))
}

// relativePath refuses a manifest entry that would reach outside the root it
// is applied to.
func relativePath(p string) (string, error) {
	clean := filepath.Clean(strings.TrimPrefix(p, "/"))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("diff manifest lists a path outside the root: %q", p)
	}
	return clean, nil
}

// RefFor returns the reference of the diff for a full image reference: the
// same repository, with TagSuffix appended to the tag. A reference without a
// tag gets the diff of latest. A reference pinned by digest has no diff name,
// because a digest cannot be extended.
func RefFor(ref string) (string, error) {
	parsed, err := name.ParseReference(ref)
	if err != nil {
		return "", fmt.Errorf("parsing %s: %w", ref, err)
	}
	tag, ok := parsed.(name.Tag)
	if !ok {
		return "", fmt.Errorf("%s is pinned by digest, a diff can only be found next to a tag", ref)
	}
	return tag.Context().Tag(tag.TagStr() + TagSuffix).Name(), nil
}
