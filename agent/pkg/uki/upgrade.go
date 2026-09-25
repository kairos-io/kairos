package uki

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/kairos-io/kairos/v4/agent/pkg/action"
	"github.com/kairos-io/kairos/v4/agent/pkg/constants"
	"github.com/kairos-io/kairos/v4/agent/pkg/elemental"
	v1 "github.com/kairos-io/kairos/v4/agent/pkg/implementations/spec"
	elementalUtils "github.com/kairos-io/kairos/v4/agent/pkg/utils"
	events "github.com/kairos-io/kairos/v4/sdk/bus"
	"github.com/kairos-io/kairos/v4/sdk/signatures"
	"github.com/kairos-io/kairos/v4/sdk/state"
	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
	"github.com/kairos-io/kairos/v4/sdk/utils"
	"github.com/rs/zerolog"
)

type UpgradeAction struct {
	cfg  *sdkConfig.Config
	spec *v1.UpgradeUkiSpec
}

func NewUpgradeAction(cfg *sdkConfig.Config, spec *v1.UpgradeUkiSpec) *UpgradeAction {
	return &UpgradeAction{cfg: cfg, spec: spec}
}

func (i *UpgradeAction) Run() (err error) {
	e := elemental.NewElemental(i.cfg)
	cleanup := utils.NewCleanStack()
	defer func() { err = cleanup.Cleanup(err) }()
	// Run pre-install stage
	if err = elementalUtils.RunStage(i.cfg, "kairos-uki-upgrade.pre"); err != nil {
		i.cfg.Logger.Errorf("running kairos-uki-upgrade.pre stage: %s", err.Error())
	}

	if err = events.RunHookScript("/usr/bin/kairos-agent.uki.upgrade.pre.hook"); err != nil {
		i.cfg.Logger.Errorf("running kairos-uki-upgrade.pre hook script: %s", err.Error())
	}

	// REMOUNT /efi as RW (its RO by default)
	umount, err := e.MountRWPartition(i.spec.EfiPartition)
	if err != nil {
		i.cfg.Logger.Errorf("remounting efi as RW: %s", err.Error())
		return err
	}
	cleanup.Push(umount)

	// We copy first and then rotate, so the sizes that matter are only known
	// once the new set is on disk. The check runs after the dump below and
	// before the rotation, in checkSpaceForUpgradeRotation.

	// When upgrading recovery or single entries, we don't want to replace loader.conf or any other
	// files, thus we take a simpler approach and only install the new efi file
	// and the relevant conf
	if i.spec.RecoveryUpgrade() {
		i.cfg.Logger.Infof("installing entry: recovery")
		return i.installRecovery()
	}

	if i.spec.Entry != "" { // single entry upgrade
		i.cfg.Logger.Infof("installing entry: %s", i.spec.Entry)
		return i.installEntry(i.spec.Entry)
	}

	i.cfg.Logger.Infof("installing entry: active")
	// Dump artifact to efi dir
	_, err = e.DumpSource(constants.UkiEfiDir, i.spec.Active.Source)
	if err != nil {
		i.cfg.Logger.Errorf("dumping the source: %s", err.Error())
		return err
	}

	noroleEfi := filepath.Join(constants.UkiEfiDir, "EFI", "Kairos", fmt.Sprintf("%s.efi", UnassignedArtifactRole))

	// Check if the upgrade artifact contains the proper signature before copying
	err = signatures.CheckArtifactSignatureIsValid(i.cfg.Fs, noroleEfi, i.cfg.Logger)
	if err != nil {
		i.cfg.Logger.Logger.Error().Err(err).Msg("Checking signature before upgrading")
		// Remove efi file to not occupy space and leave stuff around
		cleanup.Push(func() error {
			return removeArtifactSetWithRole(i.cfg.Fs, constants.UkiEfiDir, UnassignedArtifactRole)
		})
		i.cfg.Logger.Logger.Warn().Msg("Upgrade artifact signature does not match, upgrading to this source would result in an unbootable active system.\n" +
			"Check the upgrade source and confirm that its signed with a valid key, that key is in the machine DB and it has not been blacklisted.")
		return err
	}

	// Above check answers "signed by SOMETHING in DB". For an upgrade
	// that is not enough: DB commonly holds several unrelated CAs, and
	// any of them being trusted for boot does not mean any of them
	// should be trusted to swap the Kairos system in place. The bar
	// for an upgrade is stricter: the new .efi must be signed by the
	// same cert that signs the currently-booted one, so an attacker
	// with a different-but-DB-trusted key cannot substitute a
	// malicious kairos-agent (which we would then extract from the
	// .initrd and exec below). Refuse and unwind the norole set on
	// mismatch.
	if err := requireSameSignerAsBootedFn(i.cfg, noroleEfi); err != nil {
		cleanup.Push(func() error {
			return removeArtifactSetWithRole(i.cfg.Fs, constants.UkiEfiDir, UnassignedArtifactRole)
		})
		return err
	}

	// Extract the target's kairos binary + its kairos-release from the
	// freshly-dumped norole.efi's .initrd, run the KAIROS_INIT_VERSION
	// downgrade gate against it, and stage the extracted binary for the
	// post-rotation handoff. Doing this BEFORE rotation means a refused
	// upgrade unwinds the norole set and leaves the ESP identical to
	// how we found it — kairos-io/kairos#4917's "abort before the first
	// write" line. On any failure below we push a cleanup for the
	// norole set so the ESP is not left with a half-installed upgrade.
	stage, err := i.prepareFinalize(noroleEfi)
	if err != nil {
		cleanup.Push(func() error {
			return removeArtifactSetWithRole(i.cfg.Fs, constants.UkiEfiDir, UnassignedArtifactRole)
		})
		return err
	}
	cleanup.Push(func() error { return os.RemoveAll(stage.tempDir) })

	// The rotation deletes passive and then active before it writes anything in
	// their place, so a copy that runs out of room part way leaves nothing to
	// boot. Check that both copies fit while both sets are still on disk.
	if err = checkSpaceForUpgradeRotation(i.cfg.Fs, constants.UkiEfiDir, i.cfg.Logger); err != nil {
		i.cfg.Logger.Errorf("checking space on the EFI partition: %s", err.Error())
		// Drop the set we just dumped, it is no use to anyone now
		cleanup.Push(func() error {
			return removeArtifactSetWithRole(i.cfg.Fs, constants.UkiEfiDir, UnassignedArtifactRole)
		})
		return err
	}

	// Rotate first
	err = overwriteArtifactSetRole(i.cfg.Fs, constants.UkiEfiDir, "active", "passive", i.cfg.Logger)
	if err != nil {
		i.cfg.Logger.Errorf("rotating active to passive: %s", err.Error())
		return fmt.Errorf("rotating active to passive: %w", err)
	}

	// Install the new artifacts as "active"
	err = overwriteArtifactSetRole(i.cfg.Fs, constants.UkiEfiDir, UnassignedArtifactRole, "active", i.cfg.Logger)
	if err != nil {
		i.cfg.Logger.Errorf("installing the new artifacts as active: %s", err.Error())
		return fmt.Errorf("installing the new artifacts as active: %w", err)
	}

	if err = removeArtifactSetWithRole(i.cfg.Fs, constants.UkiEfiDir, UnassignedArtifactRole); err != nil {
		i.cfg.Logger.Errorf("removing artifact set: %s", err.Error())
		return fmt.Errorf("removing artifact set: %w", err)
	}

	// Format-writing work (sort key, boot assessment, default boot entry,
	// loader.conf key cleanup, EFI key upgrades, kairos-uki-upgrade.after
	// stage/hook) runs from the target's own kairos-agent, which was
	// staged out of the norole.efi's .initrd above, so a change to any
	// of those file shapes ships with the image that owns them.
	return i.runFinalizeStep(stage)
}

// finalizeStage holds the outputs of prepareFinalize: a temp dir on the
// host holding the extracted target binary + its kairos-release, and the
// FinalizeContext the target agent will read via its --context-file flag.
type finalizeStage struct {
	tempDir        string
	extractedAgent string
	ctx            action.FinalizeContext
}

// requireSameSignerAsBootedFn enforces the signer-match rule described at
// the call site in Run. A package-level var so tests exercising the
// surrounding UKI rotation / cleanup logic can swap in a no-op instead
// of hand-building a valid signed active.efi + DB under vfs.
var requireSameSignerAsBootedFn = requireSameSignerAsBooted

// requireSameSignerAsBooted refuses the upgrade if noroleEfi's Authenticode
// signer is not the same cert that verifies the .efi the machine is
// currently booted from. Being signed by "anything in DB" is not the
// bar for an upgrade — see the block comment at the call site in Run
// and the doc on signatures.VerifyingDBCert. Called BEFORE rotation
// so a refusal leaves the ESP unchanged.
//
// The reference file is picked from the current UKI boot role rather
// than assumed to be active.efi: fallback boots from passive.efi or
// recovery.efi are a normal state that upgrades still have to work
// from, and comparing to active.efi in that case would enforce the
// stale slot's key instead of the actually running one.
func requireSameSignerAsBooted(cfg *sdkConfig.Config, noroleEfi string) error {
	role, err := currentUkiBootRole(cfg.Logger.Logger)
	if err != nil {
		return fmt.Errorf("cannot identify the current UKI boot role for the signer-match check: %w", err)
	}
	bootedEfi := filepath.Join(constants.UkiEfiDir, "EFI", "Kairos", role+".efi")

	bootedCert, err := signatures.VerifyingDBCert(cfg.Fs, bootedEfi)
	if err != nil {
		return fmt.Errorf("cannot identify the signer of the currently-booted %s: %w", bootedEfi, err)
	}
	targetCert, err := signatures.VerifyingDBCert(cfg.Fs, noroleEfi)
	if err != nil {
		return fmt.Errorf("cannot identify the signer of %s: %w", noroleEfi, err)
	}
	if !signatures.SameSigner(bootedCert, targetCert) {
		return fmt.Errorf(
			"refusing to upgrade: %s is signed by %q but the currently-booted %s is signed by %q; "+
				"an upgrade must be signed by the same key that signed the running system, "+
				"having both keys in the machine DB is not sufficient",
			noroleEfi, targetCert.Subject.String(),
			bootedEfi, bootedCert.Subject.String(),
		)
	}
	return nil
}

// currentUkiBootRole returns the UKI loader-entry role name of the .efi
// this machine is currently booted from ("active", "passive", "recovery").
// Delegates to kairos-sdk state.DetectBoot, which reads systemd-boot's
// LoaderEntrySelected from efivarfs — that is the only reliable source
// under UKI, since the boot role does not appear on the kernel cmdline
// the way it does with GRUB. Boot states we cannot map to a role we own
// (LiveCD, AutoReset, Unknown) return an error rather than default to
// active, so a refusal is preferred over comparing against the wrong
// slot's key.
func currentUkiBootRole(logger zerolog.Logger) (string, error) {
	switch b := state.DetectBoot(logger); b {
	case state.Active:
		return "active", nil
	case state.Passive:
		return "passive", nil
	case state.Recovery:
		return "recovery", nil
	default:
		return "", fmt.Errorf("cannot pick a signer-reference .efi from boot state %q", b)
	}
}

// extractFromInitrd is what prepareFinalize uses to pull files out of the
// target's .initrd. A package-level var so tests exercising the
// surrounding UKI rotation logic can swap in a no-op instead of
// hand-building a real signed .efi + initrd fixture.
var extractFromInitrd = ExtractFromInitrd

// prepareFinalize extracts the target's kairos multi-call binary and its
// /etc/kairos-release from noroleEfi's .initrd into a host temp dir, then
// runs the KAIROS_INIT_VERSION downgrade gate against the extracted
// release file. On success the returned finalizeStage carries paths the caller
// exec's after rotation. On failure the caller is responsible for
// cleaning up the norole set on the ESP; this function only cleans up
// the temp dir it created (via os.RemoveAll on the error return).
//
// Runs BEFORE rotation so a refusal leaves the ESP unchanged.
func (i *UpgradeAction) prepareFinalize(noroleEfi string) (*finalizeStage, error) {
	tempDir, err := os.MkdirTemp("", "kairos-uki-finalize-*")
	if err != nil {
		return nil, fmt.Errorf("creating temp dir for target agent extraction: %w", err)
	}
	cleanupTempDir := func() { _ = os.RemoveAll(tempDir) }

	extractedAgent := filepath.Join(tempDir, "kairos-agent")
	extractedRelease := filepath.Join(tempDir, "kairos-release")
	found, err := extractFromInitrd(noroleEfi, map[string]string{
		"/usr/bin/kairos":           extractedAgent,
		constants.KairosReleaseFile: extractedRelease,
	})
	if err != nil {
		cleanupTempDir()
		return nil, fmt.Errorf("reading .initrd of %s: %w", noroleEfi, err)
	}
	if !containsPath(found, "/usr/bin/kairos") {
		cleanupTempDir()
		return nil, fmt.Errorf("target %s carries no /usr/bin/kairos in its .initrd; refusing upgrade (kairos-io/kairos#4917)", noroleEfi)
	}
	if !containsPath(found, constants.KairosReleaseFile) {
		cleanupTempDir()
		return nil, fmt.Errorf("target %s carries no %s in its .initrd; refusing upgrade (kairos-io/kairos#4917)", noroleEfi, constants.KairosReleaseFile)
	}
	current, err := action.ReadKairosInitVersionFromFs(i.cfg.Fs, constants.KairosReleaseFile)
	if err != nil {
		cleanupTempDir()
		return nil, fmt.Errorf("reading current KAIROS_INIT_VERSION: %w", err)
	}
	target, err := action.ReadKairosInitVersionFromDisk(extractedRelease)
	if err != nil {
		cleanupTempDir()
		return nil, fmt.Errorf("reading target KAIROS_INIT_VERSION from extracted %s: %w", extractedRelease, err)
	}
	if err := action.RefuseIfTargetIsDowngrade(current, target); err != nil {
		cleanupTempDir()
		return nil, err
	}
	if err := os.Chmod(extractedAgent, 0o755); err != nil {
		cleanupTempDir()
		return nil, fmt.Errorf("chmod extracted target agent: %w", err)
	}

	return &finalizeStage{
		tempDir:        tempDir,
		extractedAgent: extractedAgent,
		ctx:            i.buildFinalizeContext(),
	}, nil
}

// runFinalizeStep exec's the pre-extracted target kairos-agent (see
// prepareFinalize) against the FinalizeContext written into the stage's
// temp dir. Runtime failure of the target agent is propagated, not
// retried: by this point the target has likely rewritten loader.conf
// keys / sort keys / boot assessment, and an inline retry with the
// older host code would double-write. Downgrade / capability-missing
// refusals were already handled up in prepareFinalize before rotation.
func (i *UpgradeAction) runFinalizeStep(stage *finalizeStage) error {
	ctxPath := filepath.Join(stage.tempDir, "context.json")
	if err := action.WriteFinalizeContext(i.cfg.Fs, ctxPath, stage.ctx); err != nil {
		return fmt.Errorf("writing finalize context: %w", err)
	}

	i.cfg.Logger.Infof("Handing off upgrade finalize to target kairos-agent (%s)", stage.extractedAgent)
	out, err := i.cfg.Runner.Run(stage.extractedAgent, "upgrade-finalize", "--context-file", ctxPath)
	if len(out) > 0 {
		i.cfg.Logger.Infof("upgrade-finalize output: %s", string(out))
	}
	return err
}

// buildFinalizeContext packs the fields uki.RunFinalize (and the target's
// upgrade-finalize subcommand) need out of the spec into a serializable
// FinalizeContext.
func (i *UpgradeAction) buildFinalizeContext() action.FinalizeContext {
	return action.FinalizeContext{
		Mode:            action.UpgradeModeUki,
		Arch:            i.cfg.Arch,
		RecoveryUpgrade: i.spec.RecoveryUpgrade(),
		UkiEntry:        i.spec.Entry,
		EFIPartition: &action.SerializedPartition{
			Path:            i.spec.EfiPartition.Path,
			Name:            i.spec.EfiPartition.Name,
			MountPoint:      i.spec.EfiPartition.MountPoint,
			FS:              i.spec.EfiPartition.FS,
			FilesystemLabel: i.spec.EfiPartition.FilesystemLabel,
		},
	}
}

func containsPath(paths []string, want string) bool {
	for _, p := range paths {
		if p == want {
			return true
		}
	}
	return false
}

func (i *UpgradeAction) installEntry(entry string) error {
	targetEntryFile := filepath.Join(constants.UkiEfiDir, "EFI", "kairos", fmt.Sprintf("%s.efi", entry))
	if _, err := os.Stat(targetEntryFile); err != nil {
		return fmt.Errorf("could not stat target efi file for entry %s: %s", entry, err)
	}

	tmpDir, err := os.MkdirTemp("", "")
	if err != nil {
		i.cfg.Logger.Errorf("creating a tmp dir: %s", err.Error())
		return fmt.Errorf("creating a tmp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// Dump artifact to tmp dir
	e := elemental.NewElemental(i.cfg)
	_, err = e.DumpSource(tmpDir, i.spec.Active.Source)
	if err != nil {
		i.cfg.Logger.Errorf("dumping the source to the tmp dir: %s", err.Error())
		return err
	}

	err = copyFile(filepath.Join(tmpDir, "EFI", "kairos", UnassignedArtifactRole+".efi"), targetEntryFile)
	if err != nil {
		i.cfg.Logger.Errorf("copying efi files: %s", err.Error())
		return err
	}

	targetConfPath := filepath.Join(constants.UkiEfiDir, "loader", "entries", fmt.Sprintf("%s.conf", entry))
	err = copyFile(
		filepath.Join(tmpDir, "loader", "entries", UnassignedArtifactRole+".conf"),
		targetConfPath)
	if err != nil {
		i.cfg.Logger.Errorf("copying conf files: %s", err.Error())
		return err
	}
	err = replaceRoleInKey(targetConfPath, "efi", UnassignedArtifactRole, entry, i.cfg.Logger)
	if err != nil {
		// Maybe a newer system where we use the "uki" key instead of "efi"
		if err := replaceRoleInKey(targetConfPath, "uki", UnassignedArtifactRole, entry, i.cfg.Logger); err != nil {
			i.cfg.Logger.Errorf("replacing role in in key %s: %s", "uki", err.Error())
			return err
		}
	}

	return nil
}

// installRecovery replaces the "recovery" role efi and conf files with
// the UnassignedArtifactRole efi and loader files from dir
func (i *UpgradeAction) installRecovery() error {
	if err := i.installEntry("recovery"); err != nil {
		return err
	}

	targetConfPath := filepath.Join(constants.UkiEfiDir, "loader", "entries", "recovery.conf")
	err := replaceConfTitle(targetConfPath, "recovery")
	if err != nil {
		i.cfg.Logger.Errorf("replacing conf title: %s", err.Error())
		return err
	}

	return nil
}
