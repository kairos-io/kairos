package action

import (
	"fmt"
	"path/filepath"
	"strings"

	v1 "github.com/kairos-io/kairos/v4/agent/pkg/implementations/spec"
	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
	sdkImages "github.com/kairos-io/kairos/v4/sdk/types/images"
	"golang.org/x/sys/unix"
)

const mib = int64(1024 * 1024)

// statFilesystem reports the size and the free space (Bfree, so the blocks
// reserved for root count: the installer runs as root) of the filesystem
// mounted at path.
var statFilesystem = func(path string) (total, available int64, err error) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return 0, 0, err
	}
	return int64(st.Blocks) * int64(st.Bsize), int64(st.Bfree) * int64(st.Bsize), nil
}

// allocatedBytes reports the disk space a file really takes, which is less
// than its length for the sparse image files. A missing file takes nothing.
var allocatedBytes = func(path string) int64 {
	var st unix.Stat_t
	if err := unix.Stat(path, &st); err != nil {
		return 0
	}
	return st.Blocks * 512
}

// imageHolder is a mounted partition and the images the install is going to
// write on it.
type imageHolder struct {
	name       string
	mountPoint string
	images     []sdkImages.Image
}

// checkPartitionsFitImages makes sure the partitions an install without
// formatting reuses can hold the images about to be written to them: the
// state partition holds the active and passive images, the recovery partition
// the recovery image. It has to run once they are mounted and before anything
// is written, because a formatting install sizes its partitions from the
// images but this one has to take the partitions as they are.
//
// Two things are checked for each partition. The size of its filesystem has to
// be at least the size of its images. And the space still free has to be at
// least that too, counting as free what the images already there take, since
// they are replaced (the install stops earlier unless `force` is set). Space
// for later upgrades is not required, so a partition that is only big enough
// for the current images still installs.
//
// A partition that cannot be measured is logged and let through, as before
// this check existed.
func checkPartitionsFitImages(cfg *sdkConfig.Config, sp *v1.InstallSpec) error {
	holders := []imageHolder{}
	if sp.Partitions.State != nil && sp.Partitions.State.MountPoint != "" {
		holders = append(holders, imageHolder{
			name:       "state",
			mountPoint: sp.Partitions.State.MountPoint,
			images:     []sdkImages.Image{sp.Active, sp.Passive},
		})
	}
	if sp.Partitions.Recovery != nil && sp.Partitions.Recovery.MountPoint != "" {
		rec := imageHolder{
			name:       "recovery",
			mountPoint: sp.Partitions.Recovery.MountPoint,
			images:     []sdkImages.Image{sp.Recovery},
		}
		if len(holders) > 0 && holders[0].mountPoint == rec.mountPoint {
			// One partition holds everything.
			holders[0].images = append(holders[0].images, rec.images...)
		} else {
			holders = append(holders, rec)
		}
	}

	for _, h := range holders {
		raw, err := cfg.Fs.RawPath(h.mountPoint)
		if err != nil {
			cfg.Logger.Warnf("Not checking the %s partition against its images, cannot resolve %s: %s", h.name, h.mountPoint, err)
			continue
		}
		total, available, err := statFilesystem(raw)
		if err != nil {
			cfg.Logger.Warnf("Not checking the %s partition against its images, cannot read %s: %s", h.name, h.mountPoint, err)
			continue
		}

		var needed, replaced int64
		names := []string{}
		for _, img := range h.images {
			needed += int64(img.Size) * mib
			names = append(names, imageName(img))
			if img.File != "" {
				if rawImg, err := cfg.Fs.RawPath(img.File); err == nil {
					replaced += allocatedBytes(rawImg)
				}
			}
		}
		cfg.Logger.Debugf("%s partition at %s: %d bytes in size, %d free, %d taken by the images it replaces, %d needed for %s",
			h.name, h.mountPoint, total, available, replaced, needed, strings.Join(names, ", "))

		if total < needed {
			return fmt.Errorf("the existing %s partition is too small: it is %dMiB and has to hold the %s image(s) of %dMiB. Use a bigger partition or install with formatting",
				h.name, total/mib, strings.Join(names, " and "), needed/mib)
		}
		if available+replaced < needed {
			return fmt.Errorf("the existing %s partition has not enough free space: %dMiB are free (counting the images it replaces) and the %s image(s) need %dMiB",
				h.name, (available+replaced)/mib, strings.Join(names, " and "), needed/mib)
		}
	}
	return nil
}

func imageName(img sdkImages.Image) string {
	if img.File != "" {
		return filepath.Base(img.File)
	}
	return "unnamed"
}
