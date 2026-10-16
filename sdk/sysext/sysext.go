package sysext

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	sdkLogger "github.com/kairos-io/kairos/v4/sdk/types/logger"
)

var ErrorImageNoLayers = errors.New("image")

// DefaultAllowListRegex provided for easy use of defaults for confext and sysext
var DefaultAllowListRegex = regexp.MustCompile(`^usr/*|^/usr/*|^etc/*|^/etc/*`)

// ExtractFilesFromLastLayer will get an image and a destination and extract the files from the last layer in the image
// into that destination.
// It will skip anything that doesn't start with /usr or /etc as its purpose is to get the files for creating a
// sysextension or a confextension
// Accepts an allowList in form of regexp.Regexp that will match the files and allow copying
func ExtractFilesFromLastLayer(image v1.Image, dst string, log sdkLogger.KairosLogger, allowList *regexp.Regexp) error {
	layers, _ := image.Layers()
	numLayers := len(layers)
	if len(layers) <= 0 {
		return ErrorImageNoLayers
	}
	return extractFilesFromLayer(image, dst, log, allowList, numLayers-1)
}

func extractFilesFromLayer(image v1.Image, dst string, log sdkLogger.KairosLogger, allowList *regexp.Regexp, layerNumber int) error {
	layers, _ := image.Layers()
	layerToExtract := layers[layerNumber]
	layerReader, _ := layerToExtract.Uncompressed()
	defer func(layerReader io.ReadCloser) {
		_ = layerReader.Close()
	}(layerReader)
	tr := tar.NewReader(layerReader)
	// Every write goes through root, which confines extraction to dst: it
	// refuses any path, "..", or symlink that resolves outside it. This
	// keeps a malformed or hand-crafted layer from writing elsewhere on the
	// build host, and creates parent directories a layer omits.
	root, err := os.OpenRoot(dst)
	if err != nil {
		return fmt.Errorf("open destination: %w", err)
	}
	defer func() {
		_ = root.Close()
	}()
	// TODO: Support whiteout? https://github.com/opencontainers/image-spec/blob/79b036d80240ae530a8de15e1d21c7ab9292c693/layer.md#whiteouts
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("tar read: %w", err)
		}

		header.Name = filepath.Clean(header.Name)
		if !allowList.MatchString(header.Name) {
			log.Debug("Skipping ", header.Name)
			continue
		}

		// Layer paths are relative to the image root, with or without a
		// leading slash.
		name := strings.TrimPrefix(header.Name, "/")
		if !filepath.IsLocal(name) {
			return fmt.Errorf("%s: path is outside the destination", header.Name)
		}
		mode := header.FileInfo().Mode()

		switch header.Typeflag {
		case tar.TypeDir:
			log.Debugf("%s is a directory", header.Name)
			if err := root.MkdirAll(name, mode.Perm()); err != nil {
				return fmt.Errorf("mkdir: %w", err)
			}
			if err := restoreSpecialBits(root, name, mode); err != nil {
				return err
			}
		case tar.TypeReg:
			log.Debugf("%s is a file", header.Name)
			if err := root.MkdirAll(filepath.Dir(name), 0o755); err != nil {
				return fmt.Errorf("mkdir: %w", err)
			}
			file, err := root.OpenFile(name, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode.Perm())
			if err != nil {
				return fmt.Errorf("open: %w", err)
			}
			if _, err := io.Copy(file, tr); err != nil {
				_ = file.Close()
				return fmt.Errorf("copy: %w", err)
			}
			if err := file.Close(); err != nil {
				return fmt.Errorf("close: %w", err)
			}
			if err := restoreSpecialBits(root, name, mode); err != nil {
				return err
			}
		case tar.TypeSymlink:
			log.Debugf("%s is a symlink", header.Name)
			// The target is stored as is: an absolute target resolves against
			// the merged system at runtime, and root refuses to follow it
			// out of dst during extraction.
			if err := root.MkdirAll(filepath.Dir(name), 0o755); err != nil {
				return fmt.Errorf("mkdir: %w", err)
			}
			if err := root.Symlink(header.Linkname, name); err != nil {
				return fmt.Errorf("symlink: %w", err)
			}
		case tar.TypeLink:
			log.Debugf("%s is a hard link to %s", header.Name, header.Linkname)
			// A hard link names its target relative to the root of the
			// archive, which is the one place it differs from a symlink,
			// whose target is relative to the entry. The target is always an
			// earlier entry of the same archive. git's "make install" emits
			// one per helper in /usr/libexec/git-core, and busybox and
			// coreutils install trees do the same, so an image that carries
			// any of them is full of them.
			if !allowList.MatchString(header.Linkname) {
				// The target was skipped by this same loop, so there is
				// nothing on disk to link to. The allow list decided that,
				// not the layer, so drop the entry with a message that names
				// both paths rather than failing the whole extraction.
				log.Warnf("Skipping hard link %s: its target %s is outside the extension", header.Name, header.Linkname)
				continue
			}
			target := strings.TrimPrefix(filepath.Clean(header.Linkname), "/")
			if !filepath.IsLocal(target) {
				return fmt.Errorf("%s: hard link target is outside the destination", header.Name)
			}
			if err := root.MkdirAll(filepath.Dir(name), 0o755); err != nil {
				return fmt.Errorf("mkdir: %w", err)
			}
			if err := root.Link(target, name); err != nil {
				return fmt.Errorf("hard link: %w", err)
			}
		default:
			return fmt.Errorf("unsupported type: %d", header.Typeflag)
		}
	}
	return nil
}

// restoreSpecialBits reapplies the setuid, setgid and sticky bits a packaged
// binary or directory may carry. root.MkdirAll and root.OpenFile accept only
// the permission bits in their mode argument, so these are set with a
// follow-up chmod.
func restoreSpecialBits(root *os.Root, name string, mode os.FileMode) error {
	special := mode & (os.ModeSetuid | os.ModeSetgid | os.ModeSticky)
	if special == 0 {
		return nil
	}
	if err := root.Chmod(name, mode.Perm()|special); err != nil {
		return fmt.Errorf("chmod: %w", err)
	}
	return nil
}
