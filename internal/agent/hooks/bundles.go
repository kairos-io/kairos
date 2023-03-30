package hook

import (
	"github.com/kairos-io/kairos-sdk/bundles"
	"github.com/kairos-io/kairos-sdk/machine"
	config "github.com/kairos-io/kairos/v2/pkg/config"
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

type BundlePostInstall struct{}

func (b BundlePostInstall) Run(c config.Config) error {
	opts := c.Bundles.Options()
	err := bundles.RunBundles(opts...)
	if c.FailOnBundleErrors && err != nil {
		return err
	}
	return nil
}
