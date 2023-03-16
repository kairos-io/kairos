package hook

import (
	config "github.com/kairos-io/kairos/pkg/config"
	"github.com/kairos-io/kairos/pkg/utils"

	events "github.com/kairos-io/kairos-sdk/bus"
)

type RunStage struct{}

func (r RunStage) Run(c config.Config) error {
	utils.SH("elemental run-stage kairos-install.after")             //nolint:errcheck
	events.RunHookScript("/usr/bin/kairos-agent.install.after.hook") //nolint:errcheck
	return nil
}
