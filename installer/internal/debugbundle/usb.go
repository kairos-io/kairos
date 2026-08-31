package debugbundle

import (
	"io"
	"os"
	"path/filepath"

	"github.com/jaypipes/ghw/pkg/block"
	"github.com/jaypipes/ghw/pkg/option"
)

// RemovableMount is a mounted partition that lives on a removable disk.
type RemovableMount struct {
	Device     string // e.g. /dev/sdb1
	MountPoint string
}

// RemovableMounts lists mounted partitions on removable disks.
func RemovableMounts() ([]RemovableMount, error) {
	bl, err := block.New(option.WithDisableTools(), option.WithNullAlerter())
	if err != nil {
		return nil, err
	}
	var mounts []RemovableMount
	for _, disk := range bl.Disks {
		if !disk.IsRemovable {
			continue
		}
		for _, part := range disk.Partitions {
			if part.MountPoint != "" {
				mounts = append(mounts, RemovableMount{
					Device:     "/dev/" + part.Name,
					MountPoint: part.MountPoint,
				})
			}
		}
	}
	return mounts, nil
}

// CopyTo copies srcPath into destDir and returns the destination path.
func CopyTo(srcPath, destDir string) (string, error) {
	src, err := os.Open(srcPath)
	if err != nil {
		return "", err
	}
	defer src.Close()

	dest := filepath.Join(destDir, filepath.Base(srcPath))
	dst, err := os.Create(dest)
	if err != nil {
		return "", err
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return "", err
	}
	return dest, dst.Sync()
}
