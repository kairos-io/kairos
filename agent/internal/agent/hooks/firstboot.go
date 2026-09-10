package hook

import (
	cnst "github.com/kairos-io/kairos/v4/agent/pkg/constants"
	"github.com/kairos-io/kairos/v4/agent/pkg/utils"
	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
	sdkSpec "github.com/kairos-io/kairos/v4/sdk/types/spec"
)

// FirstBootStage runs the first-boot cloud-config stage. Unlike boot, network or
// fs, this stage has no systemd unit driving it: the agent fires it once, from
// the sentinel-gated first boot block, so a cloud-config can hook into the very
// first boot of a node and never again.
type FirstBootStage struct{}

func (b FirstBootStage) Run(c sdkConfig.Config, _ sdkSpec.Spec) error {
	c.Logger.Logger.Info().Msgf("Running %s hook", cnst.FirstBootHook)
	if err := utils.RunStage(&c, cnst.FirstBootHook); err != nil {
		return err
	}
	c.Logger.Logger.Info().Msgf("Finish %s hook", cnst.FirstBootHook)
	return nil
}
