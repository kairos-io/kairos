package hook

import (
	config "github.com/kairos-io/kairos/pkg/config"
	"github.com/kairos-io/kairos/pkg/utils"
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
