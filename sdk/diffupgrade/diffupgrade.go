// Package diffupgrade defines the artifact a diff upgrade applies, and the two
// halves that produce and consume it.
//
// A diff is an ordinary OCI image with a single layer. The layer holds every
// file of the target release that is new or different from the base release,
// plus a manifest at ManifestPath. The manifest names the base the diff was
// computed against, by its RootfsID, and lists the paths the target no
// longer has.
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
	// BaseLabel is the image config label, and the index annotation, that
	// carries Manifest.Base, so the base of a diff can be read from a
	// registry without pulling it.
	BaseLabel = "io.kairos.diff.base"
	// TargetLabel is the image config label, and the index annotation, that
	// carries Manifest.Target.
	TargetLabel = "io.kairos.diff.target"

	// fingerprintFile is the file whose checksum identifies a release.
	fingerprintFile = "etc/kairos-release"
)

// ErrBaseMismatch is returned when a diff was computed against a different
// release than the one it is being applied to.
var ErrBaseMismatch = errors.New("diff base does not match the system it is applied to")

// Manifest describes a diff.
type Manifest struct {
	// Version is the manifest format version.
	Version int `json:"version"`
	// Base is the RootfsID of the release the diff was computed against. A
	// diff applies only onto a copy of exactly that release.
	Base string `json:"base"`
	// Target is the RootfsID of the release the diff produces. The node
	// records it as the ID of the image the diff built.
	Target string `json:"target"`
	// BaseRelease and TargetRelease are the Fingerprint of each side. They
	// are what a person reads to tell releases apart, and Apply checks the
	// target one once the diff is applied.
	BaseRelease   string `json:"base-release"`
	TargetRelease string `json:"target-release"`
	// BaseRef and TargetRef name the images the diff was computed from.
	// They are informational, the fingerprints are what is checked.
	BaseRef   string `json:"base-ref,omitempty"`
	TargetRef string `json:"target-ref,omitempty"`
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

// Fingerprint is the checksum of the /etc/kairos-release under root. Every
// Kairos build writes that file with the version, flavor and build tooling of
// the image, so it tells releases apart for a person. It does not tell two
// builds of one release apart, or an image from a derivative that kept the
// file, which is why a diff is matched to its base by RootfsID instead.
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
// its base. base is the RootfsID of the release root was copied from, which
// the caller knows from when that release was deployed: root itself has been
// changed on the node since, and cannot be measured again.
//
// Apply checks the manifest against base, removes the deleted paths and then
// the manifest itself, and checks that root ended up as the target release.
// It returns the manifest, whose Target is the ID of the result. On error
// root is in an undefined state and has to be discarded.
func Apply(fs applyFS, root, base string) (*Manifest, error) {
	m, err := ReadManifest(fs, root)
	if err != nil {
		return nil, err
	}
	if m.Base != base {
		return nil, fmt.Errorf("%w: diff is for %s, system is %s", ErrBaseMismatch, m.Base, base)
	}
	for _, p := range m.Deleted {
		clean, err := relativePath(p)
		if err != nil {
			return nil, err
		}
		if err := fs.RemoveAll(filepath.Join(root, clean)); err != nil {
			return nil, fmt.Errorf("removing %s: %w", p, err)
		}
	}
	if err := fs.RemoveAll(filepath.Join(root, ManifestDir)); err != nil {
		return nil, err
	}
	got, err := Fingerprint(fs, root)
	if err != nil {
		return nil, err
	}
	if got != m.TargetRelease {
		return nil, fmt.Errorf("diff applied but the result is release %s, expected %s", got, m.TargetRelease)
	}
	return m, nil
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

// BaseRefFor returns the reference a diff from base is also published at,
// next to the index at indexRef: the index tag with "-" and the first 16 hex
// digits of base appended, so quay.io/kairos/hadron:v1.2.0.diff has the diff
// from a base whose ID starts with 0123456789abcdef at
// quay.io/kairos/hadron:v1.2.0.diff-0123456789abcdef.
//
// Each such tag is written by one publisher only, so it survives what the
// index may not: two publishers updating the index at the same time, where
// the last write drops the other's entry. A node that finds no entry for
// its base in the index looks here.
func BaseRefFor(indexRef, base string) (string, error) {
	parsed, err := name.ParseReference(indexRef)
	if err != nil {
		return "", fmt.Errorf("parsing %s: %w", indexRef, err)
	}
	tag, ok := parsed.(name.Tag)
	if !ok {
		return "", fmt.Errorf("%s is pinned by digest, it has no tag to publish a diff next to", indexRef)
	}
	short := strings.TrimPrefix(base, "sha256:")
	if len(short) > 16 {
		short = short[:16]
	}
	if short == "" {
		return "", fmt.Errorf("no base ID to name the diff by")
	}
	return tag.Context().Tag(tag.TagStr() + "-" + short).Name(), nil
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
