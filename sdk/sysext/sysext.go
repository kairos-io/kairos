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
	sdkLogger "github.com/kairos-io/kairos-sdk/types/logger"
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

		path := filepath.Join(dst, header.Name)
		fi := header.FileInfo()
		mask := fi.Mode()
		if !allowList.MatchString(header.Name) {
			log.Debug("Skipping ", header.Name)
			continue
		}

		switch header.Typeflag {
		case tar.TypeDir:
			log.Debugf("%s is a directory", header.Name)
			if fi, err := os.Lstat(path); !(err == nil && fi.IsDir()) {
				if err := os.MkdirAll(path, mask); err != nil {
					return fmt.Errorf("mkdir: %w", err)
				}
			}
		case tar.TypeReg:
			log.Debugf("%s is a file", header.Name)
			file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, mask)
			if err != nil {
				return fmt.Errorf("open: %w", err)
			}
			if _, err := io.Copy(file, tr); err != nil {
				file.Close()
				return fmt.Errorf("copy: %w", err)
			}
			file.Close()
		case tar.TypeSymlink:
			log.Debugf("%s is a symlink", header.Name)
			targetPath := filepath.Join(filepath.Dir(path), header.Linkname)
			if !strings.HasPrefix(targetPath, dst) {
				return fmt.Errorf("symlink: %w", err)
			}
			if err := os.Symlink(header.Linkname, path); err != nil {
				return fmt.Errorf("symlink: %w", err)
			}
		default:
			return fmt.Errorf("unsupported type: %d", header.Typeflag)
		}
	}
	return nil
}
