package hook

import (
	config "github.com/c3os-io/c3os/pkg/config"
	"github.com/c3os-io/c3os/pkg/utils"

	events "github.com/c3os-io/c3os/sdk/bus"
)

type RunStage struct{}

func (r RunStage) Run(c config.Config) error {
	utils.SH("elemental run-stage c3os-install.after")             //nolint:errcheck
	events.RunHookScript("/usr/bin/c3os-agent.install.after.hook") //nolint:errcheck
	return nil
}
