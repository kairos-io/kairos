package state

import (
	"os"

	cnst "github.com/kairos-io/kairos/v4/immucore/internal/constants"
	internalUtils "github.com/kairos-io/kairos/v4/immucore/internal/utils"
)

// runMountFailureStage runs the cloud-init stage a distribution maintainer can
// hook to recover from a half mounted node. It is a variable so the specs can
// watch it run without a yip tree on disk, the same way breakpointHook in
// timing.go stands in for MaybeBreakpoint.
var runMountFailureStage = internalUtils.RunStage

// onMountFailure is what OpCustomMounts and OpMountBind do once they know they
// could not mount everything they were asked to. It decides what the step
// returns, which is what decides whether the boot stops: OpMountBind is a
// fatal op, and OpCustomMounts is a hard dependency of it.
//
// Two things happen here, in this order.
//
// First the StageMountFailure cloud-init stage runs. The state the node is in
// is one immucore cannot resolve on its own, and what should happen next is a
// distribution's decision rather than ours: remount the device by hand, call
// home, write a sentinel for the next boot, or take the console. The step name
// and the error are exported into the stage's environment so a `commands:`
// block has something to branch on.
//
// Then the cmdline decides. Stopping is the default, because a node that is
// missing part of its persistent state writes to the ephemeral overlay and
// loses it on the next reboot, and nothing used to report that. An operator
// who would rather have the node than the diagnosis passes
// CmdlineIgnoreMountFailure and gets the old behaviour back.
//
// The stage runs on both paths. It is the hook for the state, not for the
// halt, so a maintainer gets their turn whether or not the boot continues.
func onMountFailure(step string, failure error) error {
	setStageEnv(cnst.EnvMountFailureStep, step)
	setStageEnv(cnst.EnvMountFailure, failure.Error())

	internalUtils.KLog.Logger.Info().Str("stage", cnst.StageMountFailure).Str("step", step).
		Msg("Running the mount failure stage")

	if err := runMountFailureStage(cnst.StageMountFailure); err != nil {
		// A broken hook must not replace the failure that got us here. It is
		// reported and dropped, and the mount error below is still the one
		// the boot stops on.
		internalUtils.KLog.Logger.Err(err).Str("stage", cnst.StageMountFailure).
			Msg("The mount failure stage returned an error")
	}

	if len(internalUtils.ReadCMDLineArg(cnst.CmdlineIgnoreMountFailure)) > 0 {
		internalUtils.KLog.Logger.Warn().Err(failure).Str("step", step).
			Str("cmdline", cnst.CmdlineIgnoreMountFailure).
			Msg("Booting on past a mount failure because the cmdline asked for it. The paths that did not mount are ephemeral for this boot and anything written to them is lost on reboot.")

		return nil
	}

	return failure
}

// setStageEnv reports rather than fails. The stage is a best-effort hook, and
// an environment that could not be set is not a reason to change what the boot
// does about the mount that actually broke.
func setStageEnv(name, value string) {
	if err := os.Setenv(name, value); err != nil {
		internalUtils.KLog.Logger.Err(err).Str("name", name).
			Msg("Could not export the mount failure to the stage environment")
	}
}
