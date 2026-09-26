package action

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/kairos-io/kairos/v4/sdk/kcrypt"
	"github.com/kairos-io/kairos/v4/sdk/kcrypt/lookup"
	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
)

// Package variables so the tests can stub the device probes, the encryptor
// (which needs a TPM or a KMS), the mount lookup and the confirmation prompt
// away. Same pattern as the encrypt-pending step in immucore.
var (
	kcryptScanDisksFn   = lookup.ScanBlockDevices
	kcryptBlkidLookupFn = lookup.FindByBlkid
	kcryptFsProbeFn     = lookup.FilesystemType
	kcryptUdevSettleFn  = func(cfg *sdkConfig.Config) error {
		return kcrypt.UdevAdmSettle(&cfg.Logger, kcryptEncryptSettleTimeout)
	}
	kcryptEncryptFn = func(cfg *sdkConfig.Config, labels []string) error {
		encryptor, err := kcrypt.GetEncryptorFromConfig(cfg.Logger, &cfg.Collector)
		if err != nil {
			return err
		}
		return encryptor.Encrypt(labels)
	}
	kcryptUnlockFn = func(cfg *sdkConfig.Config, labels []string) error {
		encryptor, err := kcrypt.GetEncryptorFromConfig(cfg.Logger, &cfg.Collector)
		if err != nil {
			return err
		}
		return encryptor.Unlock(labels)
	}
	kcryptMountpointsFn = mountpointsForLabel
	kcryptConfirmFn     = askForConfirmation
	procCmdlinePath     = "/proc/cmdline"
)

const kcryptEncryptSettleTimeout = 15 * time.Second

// KcryptEncrypt is `kairos-agent kcrypt encrypt LABEL...`: it encrypts the
// given plaintext partitions in place, through the same
// kcrypt.GetEncryptor().Encrypt() path the install hook and immucore's
// encrypt-pending step use (kairos-io/kairos#4556). It is meant for
// operators working from recovery, and for scripting the e2e tests.
//
// Encrypting a partition destroys its contents, so the command is defensive
// at every step: partitions the running system depends on (OEM, state,
// recovery, EFI) are refused through the same sdk guard the boot time step
// uses; a partition that is already a LUKS container is skipped, so the
// command is idempotent; a label that cannot be found, or whose filesystem
// cannot be determined, is an error rather than a guess; a mounted partition
// is refused rather than silently unmounted; and unless skipConfirmation is
// set, the operator has to type out their consent. After encrypting, the
// result is verified on a fresh scan before success is reported.
//
// The partitions are left locked. The next boot unlocks them through the
// normal path, or `kairos-agent kcrypt unlock-all` does it in place.
func KcryptEncrypt(cfg *sdkConfig.Config, labels []string, skipConfirmation bool) error {
	labels = normalizeLabels(labels)
	if len(labels) == 0 {
		return fmt.Errorf("no partition labels given")
	}

	// The same refusal list the boot time step enforces, from the same sdk
	// code, so the two cannot drift: encrypting OEM, state, recovery or EFI
	// from a running system would destroy that system. OEM additionally
	// needs the backup and restore dance only the install hook performs.
	policy := kcrypt.EncryptOnBootPolicy{Enabled: true, Partitions: labels}
	if err := policy.RejectSystemPartitions(oemLabelFromCmdline()); err != nil {
		return err
	}

	pending, err := stillPlaintextLabels(cfg, labels)
	if err != nil {
		return err
	}
	for _, label := range labels {
		if !slices.Contains(pending, label) {
			cfg.Logger.Infof("partition %s is already a LUKS container; skipping", label)
			fmt.Printf("Partition %s is already encrypted; skipping.\n", label)
		}
	}
	if len(pending) == 0 {
		fmt.Println("Nothing to encrypt.")
		return nil
	}

	// Refuse a mounted partition instead of unmounting it behind the
	// operator's back: whatever mounted it (the running system, a manual
	// mount in recovery) is using it, and encryption destroys it.
	for _, label := range pending {
		mountpoints, err := kcryptMountpointsFn(label)
		if err != nil {
			return fmt.Errorf("checking whether %s is mounted: %w", label, err)
		}
		if len(mountpoints) > 0 {
			return fmt.Errorf("partition %s is mounted at %s; unmount it first", label, strings.Join(mountpoints, ", "))
		}
	}

	if !skipConfirmation {
		fmt.Printf("\nWARNING: encrypting a partition DESTROYS ALL DATA on it.\n")
		fmt.Printf("WARNING: this action cannot be undone.\n\n")
		fmt.Printf("Partitions to encrypt: %s\n\n", strings.Join(pending, ", "))
		if !kcryptConfirmFn() {
			fmt.Println("Encryption cancelled.")
			return nil
		}
	}

	cfg.Logger.Logger.Info().Strs("partitions", pending).Msg("encrypting partitions")
	if err := kcryptEncryptFn(cfg, pending); err != nil {
		return fmt.Errorf("encrypting partitions: %w", err)
	}

	// Trust, but verify: a fresh scan has to agree that every partition is a
	// LUKS container now, or the operator gets an error instead of a false
	// success over a partition in an unknown state.
	stillPending, err := stillPlaintextLabels(cfg, pending)
	if err != nil {
		return fmt.Errorf("verifying the encryption result: %w", err)
	}
	if len(stillPending) > 0 {
		return fmt.Errorf("partition %s does not verify as a LUKS container after encryption; inspect it before retrying", strings.Join(stillPending, ", "))
	}

	fmt.Printf("Encrypted: %s\n", strings.Join(pending, ", "))
	fmt.Println("The partitions are locked. They unlock on the next boot, or run 'kairos-agent kcrypt unlock-all'.")
	return nil
}

// stillPlaintextLabels settles udev, scans the block devices once and
// returns the subset of labels that are not LUKS containers yet, in input
// order. Every destructive decision in this package (the encrypt
// subcommand's classification and its post-encrypt verification, the reset
// path's re-encryption) goes through this one sequence, so none of them
// reads a stale udev view and none of them guesses: a label that cannot be
// found, or whose filesystem cannot be determined, is an error rather than
// "probably plaintext".
func stillPlaintextLabels(cfg *sdkConfig.Config, labels []string) ([]string, error) {
	if err := kcryptUdevSettleFn(cfg); err != nil {
		return nil, fmt.Errorf("waiting for udev to settle: %w", err)
	}
	disks, err := kcryptScanDisksFn()
	if err != nil {
		return nil, err
	}

	var pending []string
	for _, label := range labels {
		encrypted, err := lookup.LabelIsEncrypted(disks, label, kcryptBlkidLookupFn, kcryptFsProbeFn)
		if err != nil {
			return nil, err
		}
		if !encrypted {
			pending = append(pending, label)
		}
	}
	return pending, nil
}

// normalizeLabels trims the given labels and drops empties and duplicates,
// preserving order.
func normalizeLabels(labels []string) []string {
	var out []string
	for _, label := range labels {
		label = strings.TrimSpace(label)
		if label == "" || slices.Contains(out, label) {
			continue
		}
		out = append(out, label)
	}
	return out
}

// oemLabelFromCmdline returns the effective OEM label when a cmdline
// override renames it, so the system-partition guard also refuses a renamed
// OEM. Same stanzas and precedence as immucore's GetOemLabel: rd.cos.oemlabel
// is honored, rd.immucore.oemlabel wins over it. An unreadable cmdline
// yields the empty string; the constant OEM label is refused regardless.
func oemLabelFromCmdline() string {
	cmdline, err := os.ReadFile(procCmdlinePath)
	if err != nil {
		return ""
	}

	var cosLabel, immucoreLabel string
	for _, field := range strings.Fields(string(cmdline)) {
		if value, found := strings.CutPrefix(field, "rd.cos.oemlabel="); found && cosLabel == "" {
			cosLabel = value
		}
		if value, found := strings.CutPrefix(field, "rd.immucore.oemlabel="); found && immucoreLabel == "" {
			immucoreLabel = value
		}
	}
	if immucoreLabel != "" {
		return immucoreLabel
	}
	return cosLabel
}

// mountpointsForLabel returns the mountpoints of the device carrying the
// given filesystem label, empty when it is not mounted. The device comes
// from the sdk lookup rather than /dev/disk/by-label, staying off the udev
// last-writer-wins ambiguity (kairos-io/kairos#4403), and findmnt is exec'd
// directly rather than through a shell: labels arrive from the command
// line, often via scripts, and the sdk's blkid helper documents why
// interpolating them into `sh -c` is not acceptable.
func mountpointsForLabel(label string) ([]string, error) {
	part, err := lookup.FindByLabel(label)
	if err != nil {
		// The caller already classified the label as an existing plaintext
		// partition; a miss here means it has no by-label view (pre
		// kairos-sdk#822), which also means nothing mounted it by label.
		return nil, nil
	}
	device := part.Path
	if device == "" {
		device = filepath.Join("/dev", part.Name)
	}

	// findmnt exits nonzero when the device is simply not mounted, which is
	// the common case and not an error.
	out, err := exec.Command("findmnt", "-n", "-o", "TARGET", "-S", device).Output()
	if err != nil {
		return nil, nil
	}
	var mountpoints []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			mountpoints = append(mountpoints, line)
		}
	}
	return mountpoints, nil
}

// askForConfirmation requires the operator to type 'yes', matching the
// cleanupnv prompt.
func askForConfirmation() bool {
	fmt.Printf("Are you sure you want to continue? (type 'yes' to confirm): ")
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan()
	return strings.TrimSpace(strings.ToLower(scanner.Text())) == "yes"
}
