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

// highBits are the mode bits os.Root.OpenFile and os.Root.MkdirAll refuse to
// take: both reject anything outside the 0o777 permission bits. An image that
// installs sudo, util-linux or passwd ships files that carry them, so they
// have to be applied after the entry exists rather than dropped.
const highBits = os.ModeSetuid | os.ModeSetgid | os.ModeSticky

// restoreHighBits puts setuid, setgid and sticky back on an entry that was
// created with its permission bits only. os.Root.Chmod races with a symlink
// swap on the path, so it is used only for directories, which this function
// has just created itself; a regular file is chmodded through its own open
// handle instead.
func restoreHighBits(root *os.Root, name string, mask os.FileMode) error {
	if mask&highBits == 0 {
		return nil
	}
	if err := root.Chmod(name, mask.Perm()|mask&highBits); err != nil {
		return fmt.Errorf("chmod %s: %w", name, err)
	}
	return nil
}

func extractFilesFromLayer(image v1.Image, dst string, log sdkLogger.KairosLogger, allowList *regexp.Regexp, layerNumber int) error {
	layers, _ := image.Layers()
	layerToExtract := layers[layerNumber]
	layerReader, _ := layerToExtract.Uncompressed()
	defer func(layerReader io.ReadCloser) {
		_ = layerReader.Close()
	}(layerReader)

	// Every write goes through the root handle rather than through a path
	// built with filepath.Join, so an entry can never land outside dst. The
	// image is untrusted input: it can ship a symlink under /usr that points
	// somewhere else on the build host and then a file "inside" that symlink,
	// which a plain os.OpenFile would follow.
	root, err := os.OpenRoot(dst)
	if err != nil {
		return fmt.Errorf("open destination %s: %w", dst, err)
	}
	defer root.Close()

	tr := tar.NewReader(layerReader)
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

		fi := header.FileInfo()
		mask := fi.Mode()
		if !allowList.MatchString(header.Name) {
			log.Debug("Skipping ", header.Name)
			continue
		}

		// The allowList accepts both usr/foo and /usr/foo, and the root handle
		// wants a path relative to dst.
		name := strings.TrimPrefix(header.Name, "/")
		if name == "" || name == "." {
			continue
		}

		switch header.Typeflag {
		case tar.TypeDir:
			log.Debugf("%s is a directory", header.Name)
			if fi, err := root.Lstat(name); err != nil || !fi.IsDir() {
				if err := root.MkdirAll(name, mask.Perm()); err != nil {
					return fmt.Errorf("mkdir %s: %w", name, err)
				}
			}
			// Set setgid or sticky on the directory itself. It also covers a
			// directory MkdirAll created implicitly for an earlier entry,
			// which never saw this header's mode.
			if err := restoreHighBits(root, name, mask); err != nil {
				return err
			}
		case tar.TypeReg:
			log.Debugf("%s is a file", header.Name)
			// O_TRUNC because the same path can appear twice in one layer, and
			// without it the tail of the first copy survives under the second.
			file, err := root.OpenFile(name, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mask.Perm())
			if err != nil {
				return fmt.Errorf("open %s: %w", name, err)
			}
			if _, err := io.Copy(file, tr); err != nil {
				file.Close()
				return fmt.Errorf("copy %s: %w", name, err)
			}
			// Through the handle we already hold, so there is no window in
			// which the path could become a symlink to somewhere else.
			if mask&highBits != 0 {
				if err := file.Chmod(mask.Perm() | mask&highBits); err != nil {
					file.Close()
					return fmt.Errorf("chmod %s: %w", name, err)
				}
			}
			file.Close()
		case tar.TypeSymlink:
			log.Debugf("%s is a symlink", header.Name)
			// An absolute target is kept as it is: a system extension is
			// merged onto /, so /usr/bin/vi -> /usr/bin/vim has to stay
			// absolute. A relative target that climbs above the extension
			// root is rejected, it can only be a mistake or an attack.
			if !filepath.IsAbs(header.Linkname) &&
				!filepath.IsLocal(filepath.Join(filepath.Dir(name), header.Linkname)) {
				return fmt.Errorf("symlink %s points outside the extension root: %s", name, header.Linkname)
			}
			if err := root.Symlink(header.Linkname, name); err != nil {
				return fmt.Errorf("symlink %s: %w", name, err)
			}
		default:
			return fmt.Errorf("unsupported type: %d", header.Typeflag)
		}
	}
	return nil
}
