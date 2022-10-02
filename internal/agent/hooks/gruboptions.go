package hook

import (
	"fmt"

	config "github.com/kairos-io/kairos/pkg/config"
	"github.com/kairos-io/kairos/pkg/machine"
	"github.com/kairos-io/kairos/pkg/utils"
)

type GrubOptions struct{}

func (b GrubOptions) Run(c config.Config) error {

	machine.Mount("COS_OEM", "/tmp/oem") //nolint:errcheck
	defer func() {
		machine.Umount("/tmp/oem")
	}()
	for k, v := range c.Install.GrubOptions {
		out, err := utils.SH(fmt.Sprintf("grub2-editenv /tmp/oem/grubenv set %s=%s", k, v))
		if err != nil {
			fmt.Printf("could not set boot option: %s\n", out+err.Error())
			return nil // do not error out
		}
	}

	return nil
}
