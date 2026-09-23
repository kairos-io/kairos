package state

import (
	"context"
	"fmt"
	"time"

	cnst "github.com/kairos-io/kairos/v4/immucore/internal/constants"
	internalUtils "github.com/kairos-io/kairos/v4/immucore/internal/utils"
	"github.com/kairos-io/kairos/v4/sdk/collector"
	sdkConstants "github.com/kairos-io/kairos/v4/sdk/constants"
	"github.com/kairos-io/kairos/v4/sdk/ghw"
	"github.com/kairos-io/kairos/v4/sdk/kcrypt"
	"github.com/kairos-io/kairos/v4/sdk/kcrypt/lookup"
	"github.com/kairos-io/kairos/v4/sdk/types/partitions"
	"github.com/spectrocloud-labs/herd"
)

// EncryptPendingDagStep encrypts partitions that the configuration marks for
// encryption but that are still plaintext on disk, before anything mounts
// them (kairos-io/kairos#4556). This is the same operation the install time
// Encrypt hook performs, moved to the first boot of the machine that owns the
// data: a golden image or template installed without encryption (and without
// a TPM) encrypts itself against each clone's own TPM on its first boot.
//
// Encrypting a plaintext partition destroys its contents, so the step never
// acts on install.encrypted_partitions alone: it requires the explicit
// kcrypt.encrypt_on_boot opt-in and is a no-op without it. With the opt-in,
// failure is fail-closed: if the configuration cannot be read at all, or a
// configured partition cannot be encrypted (no TPM, KMS unreachable), the
// boot halts with an actionable screen rather than continuing on plaintext.
//
// The step is idempotent: a partition that is already a LUKS container is
// skipped, so every boot after the first is a no-op. Partitions the running
// boot depends on (OEM, state, recovery, EFI) are refused: encrypting them
// from here would destroy the system that is booting.
//
// It runs only on the normal boot DAG and only when OEM is plaintext (the
// policy is read from the mounted /run/cos/oem). UKI installs are always
// encrypted at install time, so there is nothing pending under trusted boot.
func (s *State) EncryptPendingDagStep(g *herd.Graph, opts ...herd.OpOption) error {
	return g.Add(cnst.OpEncryptPending,
		append(opts, TimedCallback(cnst.OpEncryptPending, s.runEncryptPending))...)
}

// Package variables so the specs can stub the config scan, the encryption
// itself (which needs a TPM or a KMS), the halt screen and the device probes
// (udev settle and blkid do not exist on a test host) away. Same pattern as
// encryptPartitionFn in the ensure-partitions step.
var (
	scanEncryptOnBootConfigFn = func() (*collector.Config, error) {
		return kcrypt.ScanCollectorConfig(internalUtils.KLog)
	}
	encryptPendingFn = func(config *collector.Config, labels []string) error {
		encryptor, err := kcrypt.GetEncryptorFromConfig(internalUtils.KLog, config)
		if err != nil {
			return err
		}
		return encryptor.Encrypt(labels)
	}
	haltWithBannerFn = internalUtils.HaltWithBanner
	udevSettleFn     = func() error {
		return kcrypt.UdevAdmSettle(&internalUtils.KLog, encryptPendingSettleTimeout)
	}
	blkidLookupFn     = lookup.FindByBlkid
	filesystemProbeFn = lookup.FilesystemType
)

// The lookups behind the pending/encrypted classification read the udev
// database, which the unlock path already distrusts this early in the
// initramfs (it retries 10 times with a growing delay): a slow-probing disk
// is routinely still missing from it. A wrong read here does not just fail
// an unlock, it feeds a luksFormat decision, so the classification settles
// udev first and retries on the unlock path's schedule. Variables rather
// than constants so the specs can shorten the schedule.
var (
	encryptPendingLookupAttempts = 10
	encryptPendingRetryBase      = time.Second
)

const encryptPendingSettleTimeout = 30 * time.Second

// runEncryptPending is the callback behind OpEncryptPending. Split from the
// DAG registration so the specs can drive it without building a graph.
func (s *State) runEncryptPending(_ context.Context) error {
	config, err := scanEncryptOnBootConfigFn()
	if err != nil {
		// A failed scan is not an absent policy: the node may have opted in
		// and we cannot see it. Fail closed rather than silently boot what
		// could be a node that wanted its partitions encrypted.
		failErr := fmt.Errorf("encrypt on boot: reading the configuration: %w", err)
		haltWithBannerFn(
			internalUtils.RenderEncryptOnBootConfigFailedMessage(failErr),
			"encrypt on boot: configuration could not be read",
			failErr,
		)
		// Reached only on non-systemd hosts (Alpine/openrc), where
		// HaltWithBanner paints the screen and returns so we can fail the
		// step normally.
		return failErr
	}

	policy := kcrypt.EncryptOnBootPolicyFromConfig(config, internalUtils.KLog)
	if !policy.Enabled {
		internalUtils.KLog.Logger.Debug().Msg("kcrypt.encrypt_on_boot not enabled; encrypt-pending is a no-op")
		return nil
	}
	if len(policy.Partitions) == 0 {
		internalUtils.KLog.Logger.Info().Msg("kcrypt.encrypt_on_boot is set but install.encrypted_partitions is empty; nothing to encrypt")
		return nil
	}

	pending, err := pendingEncryptionLabels(policy.Partitions)
	if err != nil {
		haltWithBannerFn(
			internalUtils.RenderEncryptOnBootLookupFailedMessage(policy.Partitions, err),
			"encrypt on boot: configured partition not found",
			err,
		)
		// Reached only on non-systemd hosts (Alpine/openrc), where
		// HaltWithBanner paints the screen and returns so we can fail the
		// step normally.
		return fmt.Errorf("encrypt on boot: %w", err)
	}
	if len(pending) == 0 {
		internalUtils.KLog.Logger.Info().
			Strs("partitions", policy.Partitions).
			Msg("all partitions configured for encryption are already LUKS; encrypt-pending is a no-op")
		return nil
	}

	// A pending partition the running boot depends on cannot be encrypted
	// from here: OEM is the mounted config source this policy was read from,
	// and state, recovery and EFI carry the system that is booting. The
	// install time hook (which backs OEM up and restores it after
	// encryption) is the supported path for those.
	if label, reason, found := protectedPendingLabel(pending); found {
		err := fmt.Errorf("boot time encryption of %s is not supported: %s", label, reason)
		haltWithBannerFn(
			internalUtils.RenderEncryptOnBootProtectedMessage(label, reason),
			fmt.Sprintf("encrypt on boot: %s cannot be encrypted during the boot", label),
			err,
		)
		// Reached only on non-systemd hosts (Alpine/openrc), where
		// HaltWithBanner paints the screen and returns so we can fail the
		// step normally.
		return err
	}

	internalUtils.KLog.Logger.Info().
		Strs("partitions", pending).
		Msg("encrypting pending partitions before mount")
	if err := encryptPendingFn(config, pending); err != nil {
		failErr := fmt.Errorf("encrypting pending partitions: %w", err)
		haltWithBannerFn(
			internalUtils.RenderEncryptOnBootFailedMessage(pending, failErr),
			"encrypt on boot: partition encryption failed",
			failErr,
		)
		// Reached only on non-systemd hosts (Alpine/openrc), where
		// HaltWithBanner paints the screen and returns so we can fail the
		// step normally.
		return failErr
	}

	internalUtils.KLog.Logger.Info().
		Strs("partitions", pending).
		Msg("pending partitions encrypted; the unlock step will open them")
	return nil
}

// protectedPendingLabel returns the first pending label naming a partition
// the running boot depends on, with the reason it cannot be encrypted from
// here. OEM is matched by its effective label as well as the constant: the
// cmdline overrides (rd.cos.oemlabel=, rd.immucore.oemlabel=) rename it, and
// a renamed OEM is still the mounted configuration source.
func protectedPendingLabel(pending []string) (string, string, bool) {
	const oemReason = "it is the mounted configuration source this policy was read from"
	protected := map[string]string{
		sdkConstants.OEMLabel:      oemReason,
		sdkConstants.StateLabel:    "it holds the root image of the system that is booting",
		sdkConstants.RecoveryLabel: "it holds the recovery system",
		sdkConstants.EfiLabel:      "the firmware reads it to start the boot",
	}
	if oemLabel := internalUtils.GetOemLabel(); oemLabel != "" {
		protected[oemLabel] = oemReason
	}

	for _, label := range pending {
		if reason, ok := protected[label]; ok {
			return label, reason, true
		}
	}
	return "", "", false
}

// pendingEncryptionLabels splits the configured labels into the ones that
// still need encrypting. A label carried by a LUKS container (directly,
// through its _LUKS outer name on post kairos-io/kairos#4403 installs, or
// visible only to blkid on pre kairos-sdk#822 installs whose container ghw
// does not surface) is already encrypted and skipped; a label carried by a
// plaintext filesystem is pending; a label carried by neither is an error,
// because the configuration asks for a partition this machine does not have.
//
// The answer feeds a luksFormat decision, so it is not taken from a single
// udev snapshot: udev settles first, and a scan that still fails is retried
// before the boot is halted over it.
func pendingEncryptionLabels(labels []string) ([]string, error) {
	if err := udevSettleFn(); err != nil {
		return nil, fmt.Errorf("waiting for udev to settle: %w", err)
	}

	var pending []string
	var err error
	for attempt := 0; attempt < encryptPendingLookupAttempts; attempt++ {
		if attempt > 0 {
			internalUtils.KLog.Logger.Warn().Int("attempt", attempt).Err(err).
				Msg("pending-encryption lookup failed, retrying")
			time.Sleep(time.Duration(attempt) * encryptPendingRetryBase)
		}
		if pending, err = classifyPendingLabels(labels); err == nil {
			return pending, nil
		}
	}
	return nil, err
}

// classifyPendingLabels is one classification pass over one ghw scan.
func classifyPendingLabels(labels []string) ([]string, error) {
	disks, err := lookup.ScanBlockDevices()
	if err != nil {
		return nil, err
	}

	var pending []string
	for _, label := range labels {
		encrypted, err := labelIsEncrypted(disks, label)
		if err != nil {
			return nil, err
		}
		if encrypted {
			internalUtils.KLog.Logger.Debug().Str("label", label).Msg("partition already encrypted; skipping")
			continue
		}
		pending = append(pending, label)
	}
	return pending, nil
}

// labelIsEncrypted reports whether the partition carrying the label is
// already a LUKS container. ghw's filesystem type comes from the udev
// database; when it is missing the device itself is asked through blkid, and
// when neither can say, the answer is an error: with a luksFormat riding on
// it, "probably plaintext" is not an answer.
func labelIsEncrypted(disks []*partitions.Disk, label string) (bool, error) {
	if _, err := lookup.FindLUKSContainerOnDisks(disks, label); err == nil {
		return true, nil
	}

	part, err := lookup.FindMapperOnDisks(disks, label)
	if err != nil {
		// Pre kairos-sdk#822 installs carry no filesystem label at all and
		// only blkid's PARTLABEL view finds their LUKS container.
		if part, err = blkidLookupFn(label); err != nil {
			return false, fmt.Errorf("partition %s is configured for encryption but was not found", label)
		}
	}

	fs := part.FS
	if fs == "" || fs == ghw.UNKNOWN {
		fs, _ = filesystemProbeFn(part.Path)
	}
	switch fs {
	case sdkConstants.LUKSFs:
		return true, nil
	case "", ghw.UNKNOWN:
		return false, fmt.Errorf("the filesystem on partition %s (%s) could not be determined; refusing to treat it as plaintext", label, part.Path)
	}
	return false, nil
}
