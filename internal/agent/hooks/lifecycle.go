package hook

import (
	"github.com/kairos-io/kairos-sdk/utils"
	"github.com/kairos-io/kairos/pkg/config"
)

type Lifecycle struct{}

func (s Lifecycle) Run(c config.Config) error {
	if c.Install.Reboot {
		utils.Reboot()
	}

	if c.Install.Poweroff {
		utils.PowerOFF()
	}
	return nil
}
