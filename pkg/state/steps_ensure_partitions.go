package state

import (
	"context"
	"errors"
	"fmt"
	"time"

	cnst "github.com/kairos-io/immucore/internal/constants"
	internalUtils "github.com/kairos-io/immucore/internal/utils"
	sdkConstants "github.com/kairos-io/kairos-sdk/constants"
	"github.com/spectrocloud-labs/herd"
)

// EnsurePartitionsDagStep gates every downstream mount step in the in-RAM
// workflow. On first boot the workstation's disk may not yet carry the
// COS_OEM and COS_PERSISTENT partitions immucore needs; this step either
// creates them (when kairos.ram.create_partitions is set) or halts the
// boot with an actionable message pointing the operator at the exact cmdline
// tokens they need to add. Once it succeeds the rest of the DAG behaves as
// if the disk had been installed normally — MountOemDagStep, custom mounts
// and cloud-init all see present labels.
//
// The step is idempotent: on any boot where both partitions already exist it
// is a no-op and returns immediately.
//
// It intentionally does NOT drop to the emergency shell on the "flag missing"
// path — that is a configuration error, not a boot failure, and needs the
// operator to change the PXE cmdline and reboot rather than hand-fix state in
// a shell. Returning an error from the DAG lets the normal failure summary
// print, which now carries our own explicit message ahead of it.
func (s *State) EnsurePartitionsDagStep(g *herd.Graph, deps ...string) error {
	return g.Add(cnst.OpEnsurePartitions,
		herd.WithDeps(deps...),
		TimedCallback(cnst.OpEnsurePartitions, func(ctx context.Context) error {
			oemFound, persistentFound, err := internalUtils.KairosPartitionsPresent()
			if err != nil {
				return fmt.Errorf("scanning disks for kairos partitions: %w", err)
			}
			if oemFound && persistentFound {
				internalUtils.KLog.Logger.Info().Msg("COS_OEM and COS_PERSISTENT present; ensure-partitions is a no-op")
				return nil
			}

			explicit, autoSet := internalUtils.ParseAutoCreateDisk()
			if !autoSet {
				candidates := internalUtils.CandidateDisks()
				internalUtils.KLog.Logger.Error().
					Bool("oem_found", oemFound).
					Bool("persistent_found", persistentFound).
					Strs("candidate_disks", candidates).
					Msg("missing required partitions and kairos.ram.create_partitions not set")
				internalUtils.HaltWithBanner(
					internalUtils.RenderMissingPartitionsMessage(oemFound, persistentFound, candidates),
					"missing kairos partitions and kairos.ram.create_partitions not set",
					errors.New("missing kairos partitions"),
				)
				// Reached only on non-systemd hosts (Alpine/openrc), where
				// HaltWithBanner paints the screen and returns so we can fail
				// the step normally.
				return errors.New("missing kairos partitions; see console for details")
			}

			target, err := selectTargetDisk(explicit)
			if err != nil {
				return err
			}

			// Wipe consent guard. Touching a disk whose partitions all belong
			// to someone else — whether by writing a fresh GPT (both labels
			// missing) or appending a partition to its table (partial case,
			// e.g. an explicitly-selected disk that is in use by another
			// system) — requires the operator to opt in with kairos.ram.wipe.
			// A disk already carrying one of our labels is exempt: appending
			// the missing sibling next to it is the expected recovery path.
			if internalUtils.DiskHasPartitions(target) &&
				!internalUtils.DiskHasKairosPartitions(target) &&
				!internalUtils.AutoCreateWipeEnabled() {
				internalUtils.HaltWithBanner(
					internalUtils.RenderWipeRequiredMessage(target),
					fmt.Sprintf("target disk %s has existing partitions; add %s to overwrite",
						target, cnst.CmdlineAutoCreatePartitionsWipe),
					errors.New("refusing to touch non-empty disk without wipe flag"),
				)
				// Reached only on non-systemd hosts (Alpine/openrc), where
				// HaltWithBanner paints the screen and returns so we can
				// fail the step normally.
				return errors.New("refusing to touch non-empty disk; see console for details")
			}

			// If we are creating BOTH partitions we init a fresh GPT (either
			// the disk is empty or the operator consented to the wipe above).
			// When one of our labels is already present on the disk, append
			// only — the existing table is preserved.
			bothMissing := !oemFound && !persistentFound
			initDisk := bothMissing

			oemSize, oemErr := internalUtils.ParseAutoCreateSize(cnst.CmdlineAutoCreateOemSize, uint64(sdkConstants.OEMSize))
			persistentSize, persistentErr := internalUtils.ParseAutoCreateSize(cnst.CmdlineAutoCreatePersistentSize, uint64(sdkConstants.PersistentSize))
			if sizeErr := errors.Join(oemErr, persistentErr); sizeErr != nil {
				internalUtils.HaltWithBanner(
					internalUtils.RenderInvalidSizeMessage(sizeErr),
					"invalid partition size on the cmdline",
					sizeErr,
				)
				// Reached only on non-systemd hosts (Alpine/openrc), where
				// HaltWithBanner paints the screen and returns so we can fail
				// the step normally.
				return fmt.Errorf("invalid partition size on the cmdline: %w", sizeErr)
			}

			internalUtils.KLog.Logger.Info().
				Str("target", target).
				Bool("init_disk", initDisk).
				Bool("oem_present", oemFound).
				Bool("persistent_present", persistentFound).
				Uint64("oem_size_mib", oemSize).
				Uint64("persistent_size_mib", persistentSize).
				Msg("creating missing kairos partitions")

			yamlStage := internalUtils.BuildEnsurePartitionsStage(target, oemFound, persistentFound, oemSize, persistentSize, initDisk)
			if err := internalUtils.RunYipStageInline("ensure-partitions", yamlStage); err != nil {
				failErr := fmt.Errorf("yip layout stage failed: %w", err)
				internalUtils.HaltWithBanner(
					internalUtils.RenderPartitioningFailedMessage(target, failErr),
					"partition creation failed",
					failErr,
				)
				// Reached only on non-systemd hosts (Alpine/openrc), where
				// HaltWithBanner paints the screen and returns so we can fail
				// the step normally.
				return failErr
			}

			// yip's layout plugin already triggers a partition table reread,
			// but udev needs a beat to populate ID_FS_LABEL. Poll until both
			// labels are visible, or fail loudly.
			waitCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			if err := internalUtils.WaitForKairosPartitions(waitCtx, 30*time.Second); err != nil {
				failErr := fmt.Errorf("waiting for new partitions to become visible: %w", err)
				internalUtils.HaltWithBanner(
					internalUtils.RenderPartitioningFailedMessage(target, failErr),
					"new partitions never became visible",
					failErr,
				)
				// Reached only on non-systemd hosts (Alpine/openrc), where
				// HaltWithBanner paints the screen and returns so we can fail
				// the step normally.
				return failErr
			}
			internalUtils.KLog.Logger.Info().Msg("kairos partitions ready")
			return nil
		}))
}

// selectTargetDisk resolves the effective target disk. explicit wins when
// non-empty (operator gave us a path). Otherwise, prefer the largest EMPTY
// candidate disk: a disk that already carries a partition table is most
// likely in use by another system, and grabbing it just because it is the
// biggest would either halt on the wipe guard or, with kairos.ram.wipe set,
// destroy the wrong disk. Only when no empty disk exists do we fall back to
// the largest disk overall, letting the wipe guard downstream explain the
// situation. The largest-first ordering matches kairos-agent's
// `device: auto` install rule so RAM mode and a regular install land on the
// same disk. Only "no candidates at all" halts boot via HaltWithBanner —
// same reasoning as the missing-flag branch: returning an error would let
// systemd proceed into a broken userland.
func selectTargetDisk(explicit string) (string, error) {
	if explicit != "" {
		if !internalUtils.DiskExists(explicit) {
			internalUtils.HaltWithBanner(
				internalUtils.RenderDiskNotFoundMessage(explicit, internalUtils.CandidateDisks()),
				fmt.Sprintf("requested disk %s does not exist", explicit),
				errors.New("requested disk not found"),
			)
			// Reached only on non-systemd hosts (Alpine/openrc), where
			// HaltWithBanner paints the screen and returns so we can fail the
			// step normally.
			return "", fmt.Errorf("requested disk %s does not exist; see console for details", explicit)
		}
		return explicit, nil
	}
	candidates := internalUtils.CandidateDisks()
	if len(candidates) > 0 {
		selected := candidates[0]
		// With kairos.ram.wipe the operator already declared "destroy whatever
		// is on the target" — the empty-disk preference would only second-guess
		// that, so the largest disk wins no matter its state. Without wipe,
		// prefer the largest EMPTY disk and fall back to the largest overall
		// (which then halts at the wipe guard with instructions).
		if !internalUtils.AutoCreateWipeEnabled() {
			for _, c := range candidates {
				if !internalUtils.DiskHasPartitions(c) {
					selected = c
					break
				}
			}
		}
		if len(candidates) > 1 {
			internalUtils.KLog.Logger.Info().
				Strs("candidates", candidates).
				Str("selected", selected).
				Msg(fmt.Sprintf("multiple candidate disks; auto-selected (override with %s=/dev/xxx)",
					cnst.CmdlineAutoCreatePartitions))
		}
		return selected, nil
	}
	internalUtils.HaltWithBanner(
		internalUtils.RenderNoDisksMessage(),
		"auto-create requested but no candidate disks visible",
		errors.New("no candidate disks"),
	)
	// Reached only on non-systemd hosts (Alpine/openrc), where HaltWithBanner
	// paints the screen and returns so we can fail the step normally.
	return "", errors.New("could not resolve a target disk; see console for details")
}
