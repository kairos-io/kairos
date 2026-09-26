package agent

import (
	"fmt"

	"github.com/kairos-io/kairos/v4/agent/internal/bus"
	"github.com/kairos-io/kairos/v4/agent/pkg/action"
	agentConfig "github.com/kairos-io/kairos/v4/agent/pkg/config"
	"github.com/kairos-io/kairos/v4/agent/pkg/uki"
	sdkLogger "github.com/kairos-io/kairos/v4/sdk/types/logger"
)

// UpgradeFinalize runs the finalize step of an upgrade using the context
// the host wrote to contextFile before handing off to the target's
// kairos-agent. The context's Mode field selects which finalize applies:
//
//   - grub mode: the target agent runs inside a chroot into the deployed
//     rootfs with the host's partitions bind-mounted under /host. See
//     agent/pkg/action.RunFinalize and the block comment on
//     UpgradeAction.handoffFinalizeToTarget for the shape of the handoff.
//
//   - uki mode: the target agent binary was extracted from the .initrd
//     section of the signed .efi (see agent/pkg/uki.ExtractFromInitrd)
//     and runs directly on the host filesystem. No chroot, no /host
//     prefix; the target-owned steps write to the same paths the host
//     agent would have.
func UpgradeFinalize(contextFile string) error {
	bus.Manager.Initialize()

	// Build a minimal Config with just the logger and defaults. We
	// deliberately do NOT scan cloud config here: everything RunFinalize
	// needs is already in the context the host wrote, so re-reading
	// configuration under the chroot (or on the host in uki mode) cannot
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

	switch ctx.Mode {
	case action.UpgradeModeUki:
		return uki.RunFinalize(cfg, ctx)
	case action.UpgradeModeGrub, "":
		return action.RunFinalize(cfg, ctx)
	default:
		return fmt.Errorf("unknown upgrade-finalize mode %q", ctx.Mode)
	}
}
