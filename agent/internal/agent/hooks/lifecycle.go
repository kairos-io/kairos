package hook

import (
	"fmt"
	"time"

	sdkConfig "github.com/kairos-io/kairos-sdk/types/config"
	sdkSpec "github.com/kairos-io/kairos-sdk/types/spec"
	"github.com/kairos-io/kairos-sdk/utils"
)

// lifecycleGracePeriod is how long we wait before rebooting or powering off, so
// that an interactive user has a chance to cancel the request.
const lifecycleGracePeriod = 5 * time.Second

type Lifecycle struct{}

func (s Lifecycle) Run(c sdkConfig.Config, spec sdkSpec.Spec) error {
	c.Logger.Logger.Debug().Msg("Running Lifecycle hook")
	if spec.ShouldReboot() {
		c.Logger.Logger.Info().Msg(gracePeriodMessage("Rebooting node"))
		time.Sleep(lifecycleGracePeriod)
		utils.Reboot()
	}

	if spec.ShouldShutdown() {
		c.Logger.Logger.Info().Msg(gracePeriodMessage("Powering off node"))
		time.Sleep(lifecycleGracePeriod)
		utils.PowerOFF()
	}
	c.Logger.Logger.Debug().Msg("Finish Lifecycle hook")
	return nil
}

// gracePeriodMessage builds the message shown to the user before the node
// reboots or powers off, telling them how long they have to cancel.
func gracePeriodMessage(action string) string {
	return fmt.Sprintf("%s in %s, press Ctrl+C to cancel", action, lifecycleGracePeriod)
}
