package image

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	v1 "github.com/google/go-containerregistry/pkg/v1"
)

// Kairos publishes system and configuration extensions as OCI artifacts whose
// single layer is the extension image itself, not a tar stream wrapping it.
// hadron-layers builds them with
//
//	oras push --artifact-type application/vnd.kairos.sysext.raw \
//	    ghcr.io/kairos-io/hadron-layers/sysext/<layer>:<version>-<arch> \
//	    <layer>.sysext.raw:application/vnd.kairos.sysext.raw
//
// which yields an image manifest carrying an artifactType, an empty config and
// one layer whose media type is the artifact type. Extracting such a manifest
// as a container image fails with `archive/tar: invalid tar header`, because
// there is no tar to read: the blob is a squashfs (or erofs) image that has to
// be written to disk verbatim.
const (
	// RawSysextArtifactType marks an artifact holding one raw system extension image.
	RawSysextArtifactType = "application/vnd.kairos.sysext.raw"
	// RawConfextArtifactType marks an artifact holding one raw configuration extension image.
	RawConfextArtifactType = "application/vnd.kairos.confext.raw"

	// imageTitleAnnotation carries the original file name of a pushed blob.
	// oras sets it from the file it uploaded, so it is where the `.raw` name
	// comes from. systemd-sysext identifies an extension by that file name.
	imageTitleAnnotation = "org.opencontainers.image.title"
)

// ErrNotRawExtension reports that a manifest is an ordinary container image
// rather than a Kairos raw extension artifact. Callers use it to fall back to
// tar extraction.
var ErrNotRawExtension = errors.New("not a Kairos raw extension artifact")

// IsRawExtensionArtifactType reports whether an OCI artifactType names one of
// the Kairos raw extension artifacts.
func IsRawExtensionArtifactType(artifactType string) bool {
	return artifactType == RawSysextArtifactType || artifactType == RawConfextArtifactType
}

// rawExtensionLayer returns the descriptor of the raw extension blob in a
// manifest. It requires the artifact to declare a Kairos raw extension
// artifactType and to carry exactly one layer of the same media type, so an
// ordinary image whose first layer happens to be uncompressed is never mistaken
// for one.
func rawExtensionLayer(manifest *v1.Manifest) (v1.Descriptor, error) {
	if manifest == nil {
		return v1.Descriptor{}, ErrNotRawExtension
	}
	if !IsRawExtensionArtifactType(manifest.ArtifactType) {
		return v1.Descriptor{}, ErrNotRawExtension
	}
	if len(manifest.Layers) != 1 {
		return v1.Descriptor{}, fmt.Errorf("artifact %q must carry exactly one layer, got %d", manifest.ArtifactType, len(manifest.Layers))
	}
	layer := manifest.Layers[0]
	if string(layer.MediaType) != manifest.ArtifactType {
		return v1.Descriptor{}, fmt.Errorf("artifact %q layer media type is %q, want %q", manifest.ArtifactType, layer.MediaType, manifest.ArtifactType)
	}
	return layer, nil
}

// rawExtensionFileName derives the on-disk name of a raw extension from its
// layer descriptor. The name has to survive as-is: systemd-sysext and the
// agent's own listing both key extensions off the file name, and the catalog
// records no other name for the blob. Anything that is not a plain file name is
// rejected instead of sanitized, so a hostile registry cannot pick the path
// that gets written.
func rawExtensionFileName(layer v1.Descriptor) (string, error) {
	name := layer.Annotations[imageTitleAnnotation]
	if name == "" {
		return "", fmt.Errorf("raw extension layer has no %s annotation to name the file after", imageTitleAnnotation)
	}
	if name != filepath.Base(name) || strings.ContainsRune(name, os.PathSeparator) || name == "." || name == ".." {
		return "", fmt.Errorf("raw extension file name %q must be a plain file name", name)
	}
	return name, nil
}

// ExtractRawExtension writes the raw extension image carried by img into
// destination and returns the path it wrote. It returns ErrNotRawExtension when
// img is an ordinary container image, which lets callers fall back to tar
// extraction.
//
// The blob is streamed into a temporary file in destination and hashed while it
// is copied. It is renamed into place only once the digest and the size match
// the descriptor, so a truncated or tampered download never appears as an
// installed extension.
func ExtractRawExtension(img v1.Image, destination string) (string, error) {
	manifest, err := img.Manifest()
	if err != nil {
		return "", err
	}
	descriptor, err := rawExtensionLayer(manifest)
	if err != nil {
		return "", err
	}
	name, err := rawExtensionFileName(descriptor)
	if err != nil {
		return "", err
	}

	layer, err := img.LayerByDigest(descriptor.Digest)
	if err != nil {
		return "", fmt.Errorf("read raw extension layer %s: %w", descriptor.Digest, err)
	}
	// Compressed returns the blob exactly as it is stored. Uncompressed would
	// try to gunzip it, and a raw extension image is not a compressed stream.
	blob, err := layer.Compressed()
	if err != nil {
		return "", fmt.Errorf("open raw extension layer %s: %w", descriptor.Digest, err)
	}
	defer blob.Close()

	target := filepath.Join(destination, name)
	temporary, err := os.CreateTemp(destination, "."+name+".*")
	if err != nil {
		return "", fmt.Errorf("create temporary file for %s: %w", target, err)
	}
	temporaryName := temporary.Name()
	defer func() {
		_ = temporary.Close()
		_ = os.Remove(temporaryName)
	}()

	digest := sha256.New()
	written, err := io.Copy(io.MultiWriter(temporary, digest), blob)
	if err != nil {
		return "", fmt.Errorf("download raw extension %s: %w", name, err)
	}
	if descriptor.Size != 0 && written != descriptor.Size {
		return "", fmt.Errorf("raw extension %s is %d bytes, manifest declares %d", name, written, descriptor.Size)
	}
	if got := "sha256:" + hex.EncodeToString(digest.Sum(nil)); got != descriptor.Digest.String() {
		return "", fmt.Errorf("raw extension %s has digest %s, manifest declares %s", name, got, descriptor.Digest)
	}
	if err := temporary.Chmod(0644); err != nil {
		return "", fmt.Errorf("set permissions on %s: %w", target, err)
	}
	if err := temporary.Close(); err != nil {
		return "", fmt.Errorf("write %s: %w", target, err)
	}
	if err := os.Rename(temporaryName, target); err != nil {
		return "", fmt.Errorf("move raw extension into %s: %w", target, err)
	}
	return target, nil
}
