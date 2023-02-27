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

func (s Lifecycle) KRun(c schema.KConfig) error {
	if c.Install.Foo().Reboot {
		utils.Reboot()
	}

	if c.Install.Foo().Poweroff {
		utils.PowerOFF()
	}
	return nil
}
