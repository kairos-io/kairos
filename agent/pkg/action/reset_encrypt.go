package action

import (
	"fmt"
	"slices"
	"strings"

	hook "github.com/kairos-io/kairos/v4/agent/internal/agent/hooks"
	internalutils "github.com/kairos-io/kairos/v4/agent/pkg/utils"
	sdkConstants "github.com/kairos-io/kairos/v4/sdk/constants"
	"github.com/kairos-io/kairos/v4/sdk/kcrypt"
	"github.com/kairos-io/kairos/v4/sdk/kcrypt/lookup"
	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
	"github.com/kairos-io/kairos/v4/sdk/types/partitions"
)

// Stub seams for the reset encryption specs, next to the ones in
// kcrypt_encrypt.go they complement.
var (
	resetIsUkiFn       = internalutils.IsUki
	resetMountSourceFn = lookup.MountSourceForLabel
	resetFsProbeFn     = lookup.FilesystemType
	resetEncryptorFn   = func(cfg *sdkConfig.Config) (kcrypt.PartitionEncryptor, error) {
		return kcrypt.GetEncryptorFromConfig(cfg.Logger, &cfg.Collector)
	}
)

// ResetPreflightFn is what both reset implementations call for every
// partition they are about to format, before any of them is formatted. A
// variable for the same reason as ResetEncryptFn.
var ResetPreflightFn = PreflightResetFormat

// PreflightResetFormats runs the preflight for every partition a reset is
// about to format, before any of them is formatted, so a reset that cannot
// end in the state the configuration demands stops while nothing has been
// destroyed. Both reset implementations call it.
func PreflightResetFormats(cfg *sdkConfig.Config, persistent, oem *partitions.Partition, formatPersistent, formatOEM bool) error {
	if formatPersistent {
		if err := ResetPreflightFn(cfg, persistent, formatOEM); err != nil {
			return err
		}
	}
	if formatOEM {
		if err := ResetPreflightFn(cfg, oem, formatOEM); err != nil {
			return err
		}
	}
	return nil
}

// PreflightResetFormat checks, before a reset formats part, that the format
// and any re-encryption after it can succeed, so a reset that cannot end
// encrypted stops while nothing has been destroyed yet. formatOEM says
// whether this reset also formats OEM. It refuses:
//
//   - A raw LUKS container. The reset must format the unlocked mapper
//     inside it, which keeps the container and its key (recovery unlocks
//     persistent before the reset runs, so this is the normal case).
//     Formatting the raw container would destroy it, and the re-encryption
//     after it would silently re-key the node. That can happen when the
//     reset spec resolves the raw partition instead of the mapper, for
//     example on pre kairos-io/kairos#4403 installs where the container and
//     the inner filesystem share a label.
//   - A partition the configuration lists as encrypted when the encryptor
//     cannot be built or validated (no TPM device, for example). Note the
//     remote KMS encryptor does not validate reachability before encrypting,
//     so an unreachable KMS is still only caught after the format.
//   - Re-encryption through a remote KMS when OEM is formatted in the same
//     reset: the challenger configuration usually lives in OEM, so the next
//     boot would have nothing telling it how to unlock the new container.
//
// When the partition is on its mapper the container survives the format and
// no re-encryption is needed, so only the raw container check applies.
func PreflightResetFormat(cfg *sdkConfig.Config, part *partitions.Partition, formatOEM bool) error {
	if part == nil || part.FilesystemLabel == "" {
		return nil
	}
	label := part.FilesystemLabel
	wantsEncrypted := resetWantsEncrypted(cfg, label)

	if strings.HasPrefix(part.Path, "/dev/mapper/") {
		if wantsEncrypted && formatOEM {
			cfg.Logger.Warnf("partition %s stays encrypted across this reset, but OEM is formatted too: "+
				"if its unlock configuration (a kcrypt challenger server) lives only in OEM, the next boot cannot unlock it", label)
		}
		return nil
	}

	fs, err := resetFsProbeFn(part.Path)
	if err != nil || fs == "" {
		if wantsEncrypted {
			return fmt.Errorf("reset preflight: cannot determine the filesystem on %s (%s), refusing to format a partition configured as encrypted; nothing was formatted", label, part.Path)
		}
		cfg.Logger.Warnf("reset preflight: could not determine the filesystem on %s (%s): %v", label, part.Path, err)
	}
	if fs == sdkConstants.LUKSFs {
		return fmt.Errorf("reset preflight: %s resolves to the raw LUKS container %s, not its unlocked mapper; "+
			"formatting it would destroy the container and its key. Unlock it (kairos-agent kcrypt unlock-all) and retry; nothing was formatted", label, part.Path)
	}

	if !wantsEncrypted {
		return nil
	}
	encryptor, err := resetEncryptorFn(cfg)
	if err != nil {
		return fmt.Errorf("reset preflight: %s is configured as encrypted but cannot be encrypted: %w; nothing was formatted", label, err)
	}
	if _, remote := encryptor.(*kcrypt.RemoteKMSEncryptor); formatOEM && remote {
		return fmt.Errorf("reset preflight: refusing to encrypt %s through a remote KMS while this reset also formats OEM, "+
			"which holds the challenger configuration the next boot needs to unlock it; reset without --reset-oem, or encrypt locally; nothing was formatted", label)
	}
	return nil
}

// ResetEncryptFn is what both reset implementations call right after they
// format a partition. It is a variable so the reset specs, here and in
// agent/pkg/uki, can check through ResetAction.Run that every format branch
// is wired to it: UKI nodes are dispatched to a separate reset
// implementation, and specs that only call the helper directly cannot see
// a branch that never calls it.
var ResetEncryptFn = EncryptFormattedPartition

// EncryptFormattedPartition restores encryption on a partition right after
// a reset reformatted it. A reset format produces a plaintext filesystem,
// so a node whose configuration lists the partition in
// install.encrypted_partitions would come back from reset unencrypted,
// with nothing but a QA eye to notice. This is the reset half of
// kairos-io/kairos#4556: reset ends in the same state install ends in.
//
// It is called only from the format branches of both reset
// implementations (FormatPersistent and FormatOEM in ResetAction here and
// in agent/pkg/uki, which UKI nodes are dispatched to), which are the points where the
// partition is empty by construction, so encrypting it cannot destroy
// data. OEM needs no backup dance here for the same reason: unlike at
// install time, the reset format has already emptied it on purpose.
// Everything else is defensive:
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
// mapper device, because the rest of the reset (the OEM remount, state
// record, log copy) still mounts the partition through the spec's path.
func EncryptFormattedPartition(cfg *sdkConfig.Config, part *partitions.Partition) error {
	if part == nil || part.FilesystemLabel == "" {
		return nil
	}
	label := part.FilesystemLabel

	if !resetWantsEncrypted(cfg, label) {
		cfg.Logger.Debugf("partition %s is not configured for encryption; leaving it plaintext after the format", label)
		return nil
	}

	// The classification feeds a luksFormat decision, so it goes through the
	// same settle-scan-classify sequence as the encrypt subcommand: never a
	// stale udev view, and an unanswerable question fails the reset instead
	// of being guessed at.
	pending, err := stillPlaintextLabels(cfg, []string{label})
	if err != nil {
		return fmt.Errorf("reset encryption: classifying %s after the format: %w", label, err)
	}
	if len(pending) == 0 {
		cfg.Logger.Infof("partition %s is still a LUKS container after the format; nothing to re-encrypt", label)
		return nil
	}

	cfg.Logger.Logger.Info().Str("partition", label).
		Msg("configuration lists the partition as encrypted; encrypting it again after the reset format")
	if err := kcryptEncryptFn(cfg, []string{label}); err != nil {
		return fmt.Errorf("reset encryption: encrypting %s: %w", label, err)
	}

	// The rest of the reset still needs the partition: unlock it and point
	// the spec at the mapper, which is the only unambiguous device now that
	// the label also exists inside a LUKS container.
	if err := kcryptUnlockFn(cfg, []string{label}); err != nil {
		return fmt.Errorf("reset encryption: unlocking %s after encrypting it: %w", label, err)
	}
	if err := kcryptUdevSettleFn(cfg); err != nil {
		return fmt.Errorf("reset encryption: waiting for udev after the unlock: %w", err)
	}
	source, err := resetMountSourceFn(label)
	if err != nil {
		return fmt.Errorf("reset encryption: resolving the mapper for %s: %w", label, err)
	}
	cfg.Logger.Logger.Info().Str("partition", label).Str("device", source).
		Msg("partition encrypted and unlocked; the reset continues against the mapper")
	part.Path = source
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
