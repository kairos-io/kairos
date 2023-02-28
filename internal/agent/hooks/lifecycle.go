package hook

import (
	config "github.com/kairos-io/kairos/pkg/config"
	schema "github.com/kairos-io/kairos/pkg/config/schemas"
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

// KRun is a temporary function that does the same as Run. It will be removed as soon as the transition from config.Config to schema.KConfig is finished.
func (s Lifecycle) KRun(c schema.KConfig) error {
	if c.Install.Power().Reboot {
		utils.Reboot()
	}

	if c.Install.Power().Poweroff {
		utils.PowerOFF()
	}
	return nil
}
