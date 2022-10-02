package hook

import (
	config "github.com/kairos-io/kairos/pkg/config"
	"github.com/kairos-io/kairos/pkg/machine"
	"github.com/kairos-io/kairos/sdk/bundles"
)

type BundleOption struct{}

func (b BundleOption) Run(c config.Config) error {

	machine.Mount("COS_PERSISTENT", "/usr/local") //nolint:errcheck
	defer func() {
		machine.Umount("/usr/local")
	}()

	machine.Mount("COS_OEM", "/oem") //nolint:errcheck
	defer func() {
		machine.Umount("/oem")
	}()

	opts := c.Install.Bundles.Options()
	err := bundles.RunBundles(opts...)
	if c.FailOnBundleErrors && err != nil {
		return err
	}

	return nil
}
