package diffupgrade

import (
	"archive/tar"
	"context"
	"errors"
	"io"
	"os"
	"path"

	"github.com/containerd/containerd/v2/pkg/archive"
	"github.com/containerd/containerd/v2/pkg/archive/compression"
)

// IsImageArchive reports whether the tarball at p is an image archive, as
// docker save or an OCI layout export writes, rather than a root filesystem.
// Image archives carry their metadata at the top level, which no root
// filesystem has. The tarball may be compressed.
func IsImageArchive(p string) (bool, error) {
	f, err := os.Open(p)
	if err != nil {
		return false, err
	}
	defer func() { _ = f.Close() }()
	r, err := compression.DecompressStream(f)
	if err != nil {
		return false, err
	}
	defer func() { _ = r.Close() }()

	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		switch path.Clean(hdr.Name) {
		case "manifest.json", "index.json", "oci-layout":
			return true, nil
		}
	}
}

// UnpackTarball unpacks the root filesystem tarball at p into dest. The
// tarball may be compressed with gzip, zstd or not at all.
func UnpackTarball(p, dest string) error {
	f, err := os.Open(p)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	r, err := compression.DecompressStream(f)
	if err != nil {
		return err
	}
	defer func() { _ = r.Close() }()
	_, err = archive.Apply(context.Background(), dest, r)
	return err
}
