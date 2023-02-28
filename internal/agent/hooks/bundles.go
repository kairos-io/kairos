package hook

import (
	config "github.com/kairos-io/kairos/pkg/config"
	schema "github.com/kairos-io/kairos/pkg/config/schemas"
	"github.com/kairos-io/kairos/pkg/machine"
	"github.com/kairos-io/kairos/sdk/bundles"
)

type BundleOption struct{}

func (b BundleOption) Run(c config.Config) error {

	machine.Mount("COS_PERSISTENT", "/usr/local") //nolint:errcheck
	defer func() {
		machine.Umount("/usr/local") //nolint:errcheck
	}()

	machine.Mount("COS_OEM", "/oem") //nolint:errcheck
	defer func() {
		machine.Umount("/oem") //nolint:errcheck
	}()

	opts := c.Install.Bundles.Options()
	err := bundles.RunBundles(opts...)
	if c.FailOnBundleErrors && err != nil {
		return err
	}

	return nil
}

// KRun is a temporary function that does the same as Run. It will be removed as soon as the transition from config.Config to schema.KConfig is finished.
func (b BundleOption) KRun(kc schema.KConfig) error {

	machine.Mount("COS_PERSISTENT", "/usr/local") //nolint:errcheck
	defer func() {
		machine.Umount("/usr/local") //nolint:errcheck
	}()

	machine.Mount("COS_OEM", "/oem") //nolint:errcheck
	defer func() {
		machine.Umount("/oem") //nolint:errcheck
	}()

	opts := kc.Install.Bundles.Options()
	err := bundles.RunBundles(opts...)
	if kc.FailOnBundleErrors && err != nil {
		return err
	}

	return nil
}

type BundlePostInstall struct{}

func (b BundlePostInstall) Run(c config.Config) error {
	opts := c.Bundles.Options()
	err := bundles.RunBundles(opts...)
	if c.FailOnBundleErrors && err != nil {
		return err
	}
	return nil
}

// KRun is a temporary function that does the same as Run. It will be removed as soon as the transition from config.Config to schema.KConfig is finished.
func (b BundlePostInstall) KRun(kc schema.KConfig) error {
	opts := kc.Bundles.Options()
	err := bundles.RunBundles(opts...)
	if kc.FailOnBundleErrors && err != nil {
		return err
	}
	return nil
}
