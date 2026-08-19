package hook

import (
	"os"
	"os/exec"
	"syscall"

	"github.com/kairos-io/kairos-agent/v2/pkg/constants"
	"github.com/kairos-io/kairos-sdk/bundles"
	"github.com/kairos-io/kairos-sdk/machine"
	sdkConfig "github.com/kairos-io/kairos-sdk/types/config"
	sdkSpec "github.com/kairos-io/kairos-sdk/types/spec"
	"github.com/kairos-io/kairos-sdk/utils"
)

// BundlePostInstall install bundles just after installation
type BundlePostInstall struct{}

func (b BundlePostInstall) Run(c sdkConfig.Config, _ sdkSpec.Spec) error {
	if len(c.Install.Bundles) == 0 {
		c.Logger.Logger.Info().Msg("No bundles to run in BundlePostInstall hook")
		return nil
	}
	// system extension are now installed to /var/lib/extensions
	// https://github.com/kairos-io/kairos/issues/1821
	// so if we want them to work as expected we need to mount the persistent dir and the bind dir that will
	// then end up in the system as /usr/lib/extensions is not valid anymore
	// So after the install from livecd we need to:
	// - mount persistent in /usr/local
	// - create the dir that will be binded into /var/lib/extensions after reboot in /usr/local/.state/var-lib-extensions.bind
	// - rsync whatever is in the /var/lib/extensions to /usr/local/.state/var-lib-extensions.bind so we dont lost thing shipped with the install media
	// - bind the dir so the extension gets persistent after reboots
	// - umount the bind dir
	// Note that the binding of /usr/local/.state/var-lib-extensions.bind to /var/lib/extensions on active/passive its done by inmmucore based on the
	// 00_rootfs.yaml config which sets the bind and ephemeral paths.
	c.Logger.Logger.Info().Msg("Running BundlePostInstall hook")
	_ = machine.Umount(constants.OEMDir)        //nolint:errcheck
	_ = machine.Umount(constants.PersistentDir) //nolint:errcheck

	c.Logger.Logger.Debug().Msg("Mounting OEM partition")
	err := machine.Mount(constants.OEMLabel, constants.OEMPath)
	if err != nil {
		c.Logger.Logger.Err(err).Msg("could not mount OEM")
		return err
	}
	defer func() {
		c.Logger.Debugf("Unmounting OEM partition")
		err = machine.Umount(constants.OEMPath)
		if err != nil {
			c.Logger.Errorf("could not unmount oem partition: %s", err)
		}
	}()

	_, _ = utils.SH("udevadm trigger --type=all || udevadm trigger")
	syscall.Sync()
	c.Logger.Logger.Debug().Msg("Mounting persistent partition")
	err = machine.Mount(constants.PersistentLabel, constants.UsrLocalPath)
	if err != nil {
		c.Logger.Logger.Err(err).Msg("could not mount persistent")
		return err
	}

	defer func() {
		c.Logger.Debugf("Unmounting persistent partition")
		err = machine.Umount(constants.UsrLocalPath)
		if err != nil {
			c.Logger.Errorf("could not unmount persistent partition: %s", err)
		}
	}()

	err = os.MkdirAll("/usr/local/.state/var-lib-extensions.bind", os.ModeDir|os.ModePerm)
	if c.FailOnBundleErrors && err != nil {
		return err
	}

	cmd := exec.Command("rsync", "-aqAX", "/var/lib/extensions/", "/usr/local/.state/var-lib-extensions.bind")
	_, err = cmd.CombinedOutput()
	if c.FailOnBundleErrors && err != nil {
		return err
	}
	err = c.Syscall.Mount("/usr/local/.state/var-lib-extensions.bind", "/var/lib/extensions", "", syscall.MS_BIND, "")
	if c.FailOnBundleErrors && err != nil {
		return err
	}
	defer func() {
		_ = machine.Umount("/var/lib/extensions")
	}()

	opts := c.Install.Bundles.Options()
	err = bundles.RunBundles(opts...)
	if c.FailOnBundleErrors && err != nil {
		return err
	}
	c.Logger.Logger.Info().Msg("Finish BundlePostInstall hook")
	return nil
}

// BundleFirstBoot installs bundles during the first boot of the machine
type BundleFirstBoot struct{}

func (b BundleFirstBoot) Run(c sdkConfig.Config, _ sdkSpec.Spec) error {
	c.Logger.Logger.Debug().Msg("Running BundleFirstBoot hook")
	opts := c.Bundles.Options()
	err := bundles.RunBundles(opts...)
	if c.FailOnBundleErrors && err != nil {
		return err
	}
	c.Logger.Logger.Debug().Msg("Finish BundleFirstBoot hook")
	return nil
}
