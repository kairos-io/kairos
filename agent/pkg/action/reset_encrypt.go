package action

import (
	"fmt"
	"slices"

	hook "github.com/kairos-io/kairos/v4/agent/internal/agent/hooks"
	internalutils "github.com/kairos-io/kairos/v4/agent/pkg/utils"
	"github.com/kairos-io/kairos/v4/sdk/kcrypt/lookup"
	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
	"github.com/kairos-io/kairos/v4/sdk/types/partitions"
)

// Stub seams for the reset encryption specs, next to the ones in
// kcrypt_encrypt.go they complement.
var (
	resetIsUkiFn       = internalutils.IsUki
	resetMountSourceFn = lookup.MountSourceForLabel
)

// encryptFormattedPersistent restores encryption on the persistent partition
// right after a reset reformatted it. A reset format produces a plaintext
// filesystem, so a node whose configuration lists persistent in
// install.encrypted_partitions would come back from reset unencrypted, with
// nothing but a QA eye to notice. This is the reset half of
// kairos-io/kairos#4556: reset ends in the same state install ends in.
//
// It is called only from the FormatPersistent branch of the reset, which is
// the one point where the partition is empty by construction, so encrypting
// it cannot destroy data. Everything else is defensive:
//
//   - Nothing configured for this label: no-op. The opt-out stays the same
//     as install's.
//   - The partition is already a LUKS container: no-op. This is the normal
//     case on a node encrypted at install, where the reset formatted the
//     unlocked mapper inside the container rather than the partition.
//   - The classification cannot answer (label not found, filesystem
//     undeterminable): the reset fails rather than guesses.
//   - Encryption or the unlock after it fails: the reset fails. A node
//     whose configuration demands encryption must not come back from reset
//     plaintext, the same fail closed semantics the boot time step has.
//
// After encrypting it unlocks the partition and repoints the spec at the
// mapper device, because the rest of the reset (state record, log copy)
// still mounts persistent through the spec's path.
func (r *ResetAction) encryptFormattedPersistent(persistent *partitions.Partition) error {
	if persistent == nil || persistent.FilesystemLabel == "" {
		return nil
	}
	label := persistent.FilesystemLabel

	if !resetWantsEncrypted(r.cfg, label) {
		r.cfg.Logger.Debugf("partition %s is not configured for encryption; leaving it plaintext after the format", label)
		return nil
	}

	// The classification feeds a luksFormat decision, so it goes through the
	// same settle-scan-classify sequence as the encrypt subcommand: never a
	// stale udev view, and an unanswerable question fails the reset instead
	// of being guessed at.
	pending, err := stillPlaintextLabels(r.cfg, []string{label})
	if err != nil {
		return fmt.Errorf("reset encryption: classifying %s after the format: %w", label, err)
	}
	if len(pending) == 0 {
		r.cfg.Logger.Infof("partition %s is still a LUKS container after the format; nothing to re-encrypt", label)
		return nil
	}

	r.cfg.Logger.Logger.Info().Str("partition", label).
		Msg("configuration lists the partition as encrypted; encrypting it again after the reset format")
	if err := kcryptEncryptFn(r.cfg, []string{label}); err != nil {
		return fmt.Errorf("reset encryption: encrypting %s: %w", label, err)
	}

	// The rest of the reset still needs the partition: unlock it and point
	// the spec at the mapper, which is the only unambiguous device now that
	// the label also exists inside a LUKS container.
	if err := kcryptUnlockFn(r.cfg, []string{label}); err != nil {
		return fmt.Errorf("reset encryption: unlocking %s after encrypting it: %w", label, err)
	}
	if err := kcryptUdevSettleFn(r.cfg); err != nil {
		return fmt.Errorf("reset encryption: waiting for udev after the unlock: %w", err)
	}
	source, err := resetMountSourceFn(label)
	if err != nil {
		return fmt.Errorf("reset encryption: resolving the mapper for %s: %w", label, err)
	}
	r.cfg.Logger.Logger.Info().Str("partition", label).Str("device", source).
		Msg("partition encrypted and unlocked; the reset continues against the mapper")
	persistent.Path = source
	return nil
}

// resetWantsEncrypted reports whether the configuration says the partition
// carrying label must be encrypted: it is listed in
// install.encrypted_partitions, or the node is UKI, where install encrypts
// the hook.DefaultUKIEncryptionTargets even with an empty list. The UKI
// default comes from the install hook itself, so the two paths cannot
// drift.
func resetWantsEncrypted(cfg *sdkConfig.Config, label string) bool {
	// Install is a pointer on Config and a reset scan is not obliged to
	// fill it in; an absent block means nothing is configured, not a crash.
	if cfg.Install != nil && len(cfg.Install.Encrypt) > 0 {
		return slices.Contains(cfg.Install.Encrypt, label)
	}
	if resetIsUkiFn() {
		return slices.Contains(hook.DefaultUKIEncryptionTargets(), label)
	}
	return false
}
