package hook

import (
	"fmt"
	"path/filepath"
	"syscall"

	"github.com/kairos-io/kairos/v4/agent/pkg/constants"
	internalutils "github.com/kairos-io/kairos/v4/agent/pkg/utils"
	fsutils "github.com/kairos-io/kairos/v4/agent/pkg/utils/fs"
	"github.com/kairos-io/kairos/v4/sdk/kcrypt/lookup"
	"github.com/kairos-io/kairos/v4/sdk/machine"
	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
	sdkSpec "github.com/kairos-io/kairos/v4/sdk/types/spec"
	"github.com/kairos-io/kairos/v4/sdk/utils"
)

type CopyLogs struct{}

// Run for CopyLogs copies all current logs to the persistent partition.
// useful during install to keep the livecd logs. Its also run during reset
// best effort, no error handling
func (k CopyLogs) Run(c sdkConfig.Config, _ sdkSpec.Spec) error {
	// TODO: If we have encryption under RESET we need to make sure to:
	// - Unlock the partitions
	// - Mount OEM so we can read the config for encryption (remote server)
	// - Mount the persistent partition
	c.Logger.Logger.Info().Msg("Running CopyLogs hook")
	_ = machine.Umount(constants.PersistentDir)
	_ = machine.Umount(constants.OEMDir)
	_ = machine.Umount(constants.OEMPath)

	// Config passed during install ends up here, kcrypt challenger needs to read it if we are using a server for encryption
	_ = machine.Mount(constants.OEMLabel, constants.OEMPath)
	defer func() {
		_ = machine.Umount(constants.OEMPath)
	}()

	_, _ = utils.SH("udevadm trigger --type=all || udevadm trigger")
	_ = fsutils.MkdirAll(c.Fs, constants.PersistentDir, 0755)

	// Resolve the label to a concrete device instead of handing
	// /dev/disk/by-label/<X> to mount(2). This hook runs right after
	// Encrypt, while the persistent mapper is still unlocked, so on an
	// encrypted install the raw crypto_LUKS container and its plaintext
	// mapper both advertise COS_PERSISTENT and the symlink points at
	// whichever one udev settled last. Landing on the raw side fails the
	// mount and drops the install logs on exactly the nodes where they
	// matter most. See kairos-io/kairos#4403 and kairos-io/kairos#4685.
	source, err := lookup.MountSourceForLabel(constants.PersistentLabel)
	if err == nil && source == "" {
		err = fmt.Errorf("no device carries the %s label", constants.PersistentLabel)
	}
	if err != nil {
		c.Logger.Logger.Warn().Err(err).Msg("could not mount persistent")
		return nil
	}

	if err := c.Syscall.Mount(source, constants.PersistentDir, "ext4", 0, ""); err != nil {
		c.Logger.Logger.Warn().Err(err).Str("source", source).Msg("could not mount persistent")
		return nil
	}

	defer func() {
		err := machine.Umount(constants.PersistentDir)
		if err != nil {
			c.Logger.Errorf("could not unmount persistent partition: %s", err)
		}
	}()

	// Create the directory on persistent
	varLog := filepath.Join(constants.PersistentDir, ".state", "var-log.bind")

	err = fsutils.MkdirAll(c.Fs, varLog, 0755)
	if err != nil {
		c.Logger.Errorf("could not create directory on persistent partition: %s", err)
		return nil
	}
	// Copy all current logs to the persistent partition
	err = internalutils.SyncData(c.Logger, c.Runner, c.Fs, "/var/log/", varLog, []string{}...)
	if err != nil {
		c.Logger.Errorf("could not copy logs to persistent partition: %s", err)
		return nil
	}
	syscall.Sync()
	c.Logger.Debugf("Logs copied to persistent partition")
	c.Logger.Logger.Info().Msg("Finish CopyLogs hook")
	return nil
}
