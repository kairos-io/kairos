package hook

import (
	"github.com/kairos-io/kairos/v4/agent/pkg/constants"
	"github.com/kairos-io/kairos/v4/sdk/machine"
	sdkFs "github.com/kairos-io/kairos/v4/sdk/types/fs"
)

// cloudConfigPerm is the mode of the cloud-config files dropped for the
// installed system. They are read by yip as root and can carry secrets, so
// nothing else gets to see them.
const cloudConfigPerm = 0400

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
