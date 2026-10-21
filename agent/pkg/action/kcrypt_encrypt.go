package action

import (
	"bufio"
	"fmt"
	"os"
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
	procMountsPath      = "/proc/mounts"
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
	// mount in recovery) is using it, and encryption destroys it. Checked
	// here so the operator is not asked to confirm something that will be
	// refused, and again after the confirmation below.
	if err := refuseMounted(pending); err != nil {
		return err
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

	// The prompt can wait indefinitely, and the encryptor underneath
	// silently unmounts a mounted device before formatting it, so a mount
	// made while the prompt was open must still be refused.
	if err := refuseMounted(pending); err != nil {
		return err
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

// refuseMounted returns an error when any of the labels is mounted, or when
// whether it is mounted cannot be determined.
func refuseMounted(labels []string) error {
	for _, label := range labels {
		mountpoints, err := kcryptMountpointsFn(label)
		if err != nil {
			return fmt.Errorf("checking whether %s is mounted: %w", label, err)
		}
		if len(mountpoints) > 0 {
			return fmt.Errorf("partition %s is mounted at %s; unmount it first", label, strings.Join(mountpoints, ", "))
		}
	}
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
// given filesystem label, empty only when it is positively not mounted.
//
// The answer gates a destructive write, and the encryptor underneath will
// silently unmount a mounted device before formatting it, so this check
// fails closed: a device that cannot be resolved, or a mount table that
// cannot be read, is an error, never "not mounted". The device comes from
// the sdk lookup (with the blkid fallback for pre kairos-sdk#822 installs)
// rather than /dev/disk/by-label, and the mount table is read directly
// rather than through findmnt, whose nonzero exit cannot tell "no match"
// from "could not look".
func mountpointsForLabel(label string) ([]string, error) {
	part, err := lookup.FindByLabel(label)
	if err != nil {
		return nil, fmt.Errorf("cannot resolve the device for %s to check whether it is mounted: %w", label, err)
	}
	device := part.Path
	if device == "" && part.Name != "" {
		device = filepath.Join("/dev", part.Name)
	}
	if device == "" {
		return nil, fmt.Errorf("cannot resolve the device for %s to check whether it is mounted", label)
	}
	return mountpointsForDevice(procMountsPath, device)
}

// mountpointsForDevice lists the mountpoints in the given mount table whose
// source is device. Sources that are symlinks (a /dev/disk/by-* path) are
// resolved before comparing, so a mount made through a by-label link is
// still seen. An unreadable table is an error.
func mountpointsForDevice(mountsPath, device string) ([]string, error) {
	want := device
	if resolved, err := filepath.EvalSymlinks(device); err == nil {
		want = resolved
	}

	data, err := os.ReadFile(mountsPath)
	if err != nil {
		return nil, fmt.Errorf("reading the mount table %s: %w", mountsPath, err)
	}

	var mountpoints []string
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		source := fields[0]
		if strings.HasPrefix(source, "/") {
			if resolved, err := filepath.EvalSymlinks(source); err == nil {
				source = resolved
			}
		}
		if source == want || fields[0] == device {
			// /proc/mounts escapes spaces in mountpoints as \040.
			mountpoints = append(mountpoints, strings.ReplaceAll(fields[1], `\040`, " "))
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
