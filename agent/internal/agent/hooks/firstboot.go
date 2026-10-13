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
	// A failure here is logged, not returned. Propagating it would abort the
	// sentinel-gated block before the firstboot sentinel is written, so a node
	// with strict mode on would re-run this hook (and re-install bundles, and
	// rewrite grubenv) on every restart of the agent unit and never publish its
	// bootstrap event. RunStage already logs the details for the operator.
	if err := utils.RunStage(&c, cnst.FirstBootHook); err != nil {
		c.Logger.Logger.Err(err).Msgf("Failed running the %s stage, continuing so the firstboot sentinel still gets written", cnst.FirstBootHook)
	}
	c.Logger.Logger.Info().Msgf("Finish %s hook", cnst.FirstBootHook)
	return nil
}
