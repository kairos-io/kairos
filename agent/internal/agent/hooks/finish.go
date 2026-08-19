package hook

import (
	sdkConfig "github.com/kairos-io/kairos-sdk/types/config"
	sdkSpec "github.com/kairos-io/kairos-sdk/types/spec"
)

// Finish is a hook that runs after the install process.
// It is used to encrypt partitions and run the BundlePostInstall, CustomMounts and CopyLogs hooks
type Finish struct{}

func (k Finish) Run(c sdkConfig.Config, spec sdkSpec.Spec) error {
	var err error

	// Run encryption (handles both UKI and non-UKI, returns early if nothing to encrypt)
	err = Encrypt(c)
	defer lockPartitions(c.Logger) // partitions are unlocked, make sure to lock them before we end
	if err != nil {
		c.Logger.Logger.Error().Err(err).Msg("could not encrypt partitions")
		return err
	}

	// Now that we have everything encrypted and ready to mount if needed
	err = GrubPostInstallOptions{}.Run(c, spec)
	if err != nil {
		c.Logger.Logger.Warn().Err(err).Msg("Could not set grub options post install")
		return err
	}
	err = BundlePostInstall{}.Run(c, spec)
	if err != nil {
		c.Logger.Logger.Warn().Err(err).Msg("could not copy run bundles post install")
		if c.FailOnBundleErrors {
			return err
		}
	}
	err = CustomMounts{}.Run(c, spec)
	if err != nil {
		c.Logger.Logger.Warn().Err(err).Msg("could not create custom mounts")
	}
	err = SSHHardening{}.Run(c, spec)
	if err != nil {
		c.Logger.Logger.Warn().Err(err).Msg("could not install ssh_hardening drop-in")
	}
	err = CopyLogs{}.Run(c, spec)
	if err != nil {
		c.Logger.Logger.Warn().Err(err).Msg("could not copy logs")
	}
	return nil
}
