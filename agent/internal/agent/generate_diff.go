package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"

	"github.com/kairos-io/kairos/v4/agent/pkg/elemental"
	"github.com/kairos-io/kairos/v4/sdk/diffupgrade"
	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
	sdkImages "github.com/kairos-io/kairos/v4/sdk/types/images"
)

// defaultDiffTag is the name recorded in a diff archive that is not pushed.
const defaultDiffTag = "kairos/diff:latest"

// GenerateDiff computes the diff that turns base into target, and writes it
// to output as an image archive, pushes it to push, or both.
//
// base and target are each one of: an image reference, pulled from a registry
// or the local daemon; an image archive, as docker save writes; a root
// filesystem tarball, compressed or not; or a directory. oci:, ocifile: and
// dir: prefixes force the kind. Both are unpacked under a temporary directory
// in workDir, so it needs room for two root filesystems.
func GenerateDiff(c *sdkConfig.Config, base, target, output, push, workDir string) error {
	work, err := os.MkdirTemp(workDir, "kairos-diff-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(work) }()

	e := elemental.NewElemental(c)
	baseRoot, err := unpackDiffInput(c, e, base, filepath.Join(work, "base"))
	if err != nil {
		return fmt.Errorf("base %s: %w", base, err)
	}
	targetRoot, err := unpackDiffInput(c, e, target, filepath.Join(work, "target"))
	if err != nil {
		return fmt.Errorf("target %s: %w", target, err)
	}

	c.Logger.Infof("Computing the diff from %s to %s", baseRoot.Ref, targetRoot.Ref)
	img, m, release, err := diffupgrade.NewImage(baseRoot, targetRoot, work)
	if err != nil {
		return err
	}
	defer release()

	tag := defaultDiffTag
	if push != "" {
		ref, err := name.ParseReference(push)
		if err != nil {
			return fmt.Errorf("parsing %s: %w", push, err)
		}
		platform, err := v1.ParsePlatform(c.Platform.String())
		if err != nil {
			return err
		}
		c.Logger.Infof("Adding the diff to the index at %s", ref.Name())
		entries, err := diffupgrade.PushToIndex(ref, img, m, platform, remote.WithAuthFromKeychain(authn.DefaultKeychain))
		if err != nil {
			return err
		}
		c.Logger.Infof("The index at %s now holds %d diffs, one per base release", ref.Name(), entries)
		if t, ok := ref.(name.Tag); ok {
			tag = t.Name()
		}
	}
	if output != "" {
		t, err := name.NewTag(tag)
		if err != nil {
			return err
		}
		if err := tarball.WriteToFile(output, t, img); err != nil {
			return err
		}
		c.Logger.Infof("Diff written to %s", output)
	}
	c.Logger.Infof("Diff from %s (%s) to %s (%s), %d deleted paths", m.BaseRef, m.Base, m.TargetRef, m.Target, len(m.Deleted))
	return nil
}

// unpackDiffInput makes a root filesystem out of one side of a diff. A
// directory is used where it is; anything else is unpacked into dest.
func unpackDiffInput(c *sdkConfig.Config, e *elemental.Elemental, input, dest string) (diffupgrade.Root, error) {
	var src *sdkImages.ImageSource
	var err error
	switch {
	case hasSourcePrefix(input):
		src, err = sdkImages.NewSrcFromURI(input)
	default:
		info, statErr := os.Stat(input)
		switch {
		case statErr == nil && info.IsDir():
			src = sdkImages.NewDirSrc(input)
		case statErr == nil:
			isImage, err := diffupgrade.IsImageArchive(input)
			if err != nil {
				return diffupgrade.Root{}, err
			}
			if !isImage {
				c.Logger.Infof("Unpacking root filesystem tarball %s", input)
				if err := os.MkdirAll(dest, 0o755); err != nil {
					return diffupgrade.Root{}, err
				}
				return diffupgrade.Root{Dir: dest, Ref: filepath.Base(input)}, diffupgrade.UnpackTarball(input, dest)
			}
			src = sdkImages.NewOCIFileSrc(input)
		default:
			src, err = sdkImages.NewSrcFromURI(input)
		}
	}
	if err != nil {
		return diffupgrade.Root{}, err
	}

	if src.IsDir() {
		return diffupgrade.Root{Dir: src.Value(), Ref: src.Value()}, nil
	}
	if src.IsFile() {
		return diffupgrade.Root{}, fmt.Errorf("file: is not a supported input, pass the path of the tarball")
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return diffupgrade.Root{}, err
	}
	if _, err := e.DumpSource(dest, src); err != nil {
		return diffupgrade.Root{}, err
	}
	ref := src.Value()
	if src.IsOCIFile() {
		ref = filepath.Base(ref)
	}
	return diffupgrade.Root{Dir: dest, Ref: ref}, nil
}

func hasSourcePrefix(input string) bool {
	for _, prefix := range []string{"oci:", "docker:", "ocifile:", "dir:", "file:"} {
		if strings.HasPrefix(input, prefix) {
			return true
		}
	}
	return false
}
