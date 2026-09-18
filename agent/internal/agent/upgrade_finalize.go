package agent

import (
	"fmt"

	"github.com/kairos-io/kairos/v4/agent/internal/bus"
	"github.com/kairos-io/kairos/v4/agent/pkg/action"
	agentConfig "github.com/kairos-io/kairos/v4/agent/pkg/config"
	sdkLogger "github.com/kairos-io/kairos/v4/sdk/types/logger"
)

// UpgradeFinalize runs the finalize step of a non-UKI upgrade using the
// context the host wrote to contextFile before chrooting into the deployed
// target rootfs. See action.RunFinalize and the block comment on
// UpgradeAction.handoffFinalizeToTarget for the shape of the handoff.
func UpgradeFinalize(contextFile string) error {
	bus.Manager.Initialize()

	// Build a minimal Config with just the logger and defaults; the target
	// agent runs inside the deployed rootfs, so filesystem/runner/mounter
	// defaults are the real ones. We deliberately do NOT scan cloud config
	// here: everything RunFinalize needs is already in the context the
	// host wrote, so re-reading configuration under the chroot cannot
	// disagree with what the host observed.
	logger := sdkLogger.NewKairosLogger("agent", "info", false)
	cfg := agentConfig.NewConfig(
		agentConfig.WithLogger(logger),
	)

	ctx, err := action.ReadFinalizeContext(cfg.Fs, contextFile)
	if err != nil {
		return fmt.Errorf("reading upgrade-finalize context from %s: %w", contextFile, err)
	}
	if ctx.Arch != "" {
		cfg.Arch = ctx.Arch
	}

	return action.RunFinalize(cfg, ctx)
}
