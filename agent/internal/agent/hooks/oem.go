package hook

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/kairos-io/kairos/v4/agent/pkg/constants"
	fsutils "github.com/kairos-io/kairos/v4/agent/pkg/utils/fs"
	"github.com/kairos-io/kairos/v4/sdk/machine"
	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
	sdkFs "github.com/kairos-io/kairos/v4/sdk/types/fs"
)

// cloudConfigPerm is the mode of the cloud-config files dropped for the
// installed system. They are read by yip as root and can carry secrets, so
// nothing else gets to see them.
const cloudConfigPerm = 0400

// cloudConfigDirPerm is the mode of a cloud-config directory created on a
// system that has no OEM partition to mount.
const cloudConfigDirPerm = 0755

// mountOEM mounts the OEM partition on constants.OEMPath and returns the
// function that unmounts it again.
func mountOEM() (func(), error) {
	if err := machine.Mount(constants.OEMLabel, constants.OEMPath); err != nil {
		return nil, err
	}
	return func() {
		_ = machine.Umount(constants.OEMPath)
	}, nil
}

// writeCloudConfig writes content as a cloud-config file for the installed
// system.
func writeCloudConfig(fs sdkFs.KairosFS, path, content string) error {
	return fs.WriteFile(path, []byte(content), cloudConfigPerm)
}

// cloudConfigDir returns the directory the installed system reads
// cloud-config from, together with the cleanup that releases it.
//
// The OEM partition is the answer whenever there is one. Devices flashed from
// a prebuilt image (Raspberry Pi and friends) have no OEM partition, so there
// we settle for the first of the remaining user config directories we can
// write to.
func cloudConfigDir(c sdkConfig.Config) (string, func(), error) {
	umount, err := mountOEM()
	if err == nil {
		return constants.OEMPath, umount, nil
	}
	c.Logger.Logger.Debug().Err(err).Msg("Could not mount the OEM partition, looking for another cloud-config directory")

	for _, dir := range installedConfigDirs() {
		// machine.Mount creates its mountpoint before mounting, so an
		// existing constants.OEMPath here is an empty directory on the
		// running system, not the partition we just failed to mount.
		if dir == constants.OEMPath {
			continue
		}
		if ok, err := fsutils.Exists(c.Fs, filepath.Dir(dir)); err != nil || !ok {
			continue
		}
		if err := fsutils.MkdirAll(c.Fs, dir, cloudConfigDirPerm); err != nil {
			c.Logger.Logger.Debug().Err(err).Str("dir", dir).Msg("Could not use cloud-config directory")
			continue
		}
		return dir, func() {}, nil
	}

	return "", nil, fmt.Errorf("no cloud-config directory available, tried %s", strings.Join(installedConfigDirs(), ", "))
}

// installedConfigDirs returns the user config directories that a file written
// during install still lives in once the machine reboots, most preferred
// first. constants.GetUserConfigDirs is ordered the other way around, lowest
// precedence first, and its live media entry is gone by the time the
// installed system boots.
func installedConfigDirs() []string {
	dirs := constants.GetUserConfigDirs()
	out := make([]string, 0, len(dirs))
	for i := len(dirs) - 1; i >= 0; i-- {
		if strings.HasPrefix(dirs[i], "/run/") {
			continue
		}
		out = append(out, dirs[i])
	}
	return out
}
