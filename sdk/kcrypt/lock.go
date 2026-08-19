package kcrypt

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/gofrs/uuid"
	sdkLogger "github.com/kairos-io/kairos-sdk/types/logger"
	"github.com/kairos-io/kairos-sdk/utils"
)

const udevTimeout = 30 * time.Second

// encryptWithLocalTPMPassphrase encrypts a partition using a passphrase stored in TPM NV memory.
// This bypasses the plugin bus and directly uses kairos-sdk TPM functions.
// Used for non-UKI local encryption (without remote KMS).
func encryptWithLocalTPMPassphrase(label string, nvIndex, cIndex, tpmDevice string, logger sdkLogger.KairosLogger, argsCreate ...string) (string, error) {
	logger.Logger.Info().Str("partition", label).Msg("Encrypting with local TPM NV passphrase")

	// Get or create passphrase from TPM NV memory
	passphrase, err := getOrCreateLocalTPMPassphrase(nvIndex, cIndex, tpmDevice)
	if err != nil {
		return "", fmt.Errorf("failed to get/create local TPM passphrase: %w", err)
	}

	logger.Logger.Info().
		Str("partition", label).
		Int("passphrase_length", len(passphrase)).
		Msg("Retrieved passphrase from local TPM NV memory")

	return luksifyWithPassphrase(label, passphrase, logger, argsCreate...)
}

// luksifyWithPassphrase encrypts a partition with an explicit passphrase (no plugin involved).
func luksifyWithPassphrase(label string, passphrase string, logger sdkLogger.KairosLogger, argsCreate ...string) (string, error) {
	// Only settle (no trigger) - we haven't made device changes yet, just need pending events to complete
	logger.Logger.Info().Msg("Waiting for udevadm to settle before finding partition")
	if err := UdevAdmSettle(&logger, udevTimeout); err != nil {
		return "", err
	}

	logger.Logger.Info().Str("label", label).Msg("Finding partition")
	info, err := findPartitionByLabel(label)
	if err != nil {
		logger.Err(err).Msg("find partition")
		return "", err
	}

	mapper := partitionMapperPath(info)
	device := info.Path

	extraArgs := []string{"--uuid", uuid.NewV5(uuid.NamespaceURL, label).String()}
	extraArgs = append(extraArgs, "--label", label)
	extraArgs = append(extraArgs, argsCreate...)

	logger.Logger.Info().Str("device", device).Msg("Checking if device is mounted")
	if err := unmountIfMounted(device, logger); err != nil {
		logger.Err(err).Msg("unmount device")
		return "", err
	}

	logger.Logger.Info().Str("device", device).Msg("Creating LUKS container")
	if err := createLuks(device, passphrase, extraArgs...); err != nil {
		logger.Err(err).Msg("create luks")
		return "", err
	}

	logger.Logger.Info().Str("device", device).Str("label", label).Msg("Formatting LUKS container")
	err = formatLuks(device, info.Name, mapper, label, passphrase, logger)
	if err != nil {
		logger.Err(err).Msg("format luks")
		return "", err
	}

	logger.Logger.Info().Str("label", label).Msg("Partition encryption completed")
	return fmt.Sprintf("%s:%s:%s", info.FilesystemLabel, info.Name, info.UUID), nil
}

// unmountIfMounted checks if a device is mounted and unmounts it if needed.
// This is necessary because cryptsetup cannot format a mounted partition.
func unmountIfMounted(device string, logger sdkLogger.KairosLogger) error {
	// Read /proc/mounts to check if the device is mounted
	// mount entries look like: /dev/sda6 / ext4 rw,relatime 0 0.
	f, err := os.Open("/proc/mounts")
	if err != nil {
		return fmt.Errorf("failed to open /proc/mounts: %w", err)
	}
	defer f.Close()

	var mountPoint string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		// fields[0] is device, fields[1] is mount point
		if len(fields) >= 2 && fields[0] == device {
			mountPoint = fields[1]
			break
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading /proc/mounts: %w", err)
	}

	// If device is not mounted, nothing to do
	if mountPoint == "" {
		return nil
	}

	logger.Logger.Debug().Str("device", device).Str("mountpoint", mountPoint).Msg("Device is mounted, unmounting before encryption")
	// Unmount using syscall.Unmount with flags=0 (standard unmount)
	if err := syscall.Unmount(mountPoint, 0); err != nil {
		return fmt.Errorf("failed to unmount %s from %s: %w", device, mountPoint, err)
	}

	logger.Logger.Debug().Str("device", device).Msg("Successfully unmounted device")
	return nil
}

func createLuks(dev, password string, cryptsetupArgs ...string) error {
	args := []string{"luksFormat", "--type", "luks2", "--iter-time", "5", "-q", dev}
	args = append(args, cryptsetupArgs...)
	cmd := exec.Command("cryptsetup", args...)
	cmd.Stdin = strings.NewReader(password)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err != nil {
		return err
	}

	return nil
}

var seededRand = rand.New(rand.NewSource(time.Now().UnixNano()))

func getRandomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[seededRand.Intn(len(charset))]
	}
	return string(b)
}

// luksifyMeasurements takes a label and a list if public-keys and pcrs to bind and uses the measurements.
// in the current node to encrypt the partition with those and bind those to the given pcrs
// this expects systemd 255 as it needs the SRK public key that systemd extracts
// Sets a random password, enrolls the policy, unlocks and formats the partition, closes it and tfinally removes the random password from it
// Note that there is a diff between the publicKeyPcrs and normal Pcrs
// The former links to a policy type that allows anything signed by that policy to unlcok the partitions so its
// really useful for binding to PCR11 which is the UKI measurements in order to be able to upgrade the system and still be able
// to unlock the partitions.
// The later binds to a SINGLE measurement, so if that changes, it will not unlock anything.
// This is useful for things like PCR7 which measures the secureboot state and certificates if you dont expect those to change during
// the whole lifetime of a machine
// It can also be used to bind to things like the firmware code or efi drivers that we dont expect to change
// default for publicKeyPcrs is 11
// default for pcrs is nothing, so it doesn't bind as we want to expand things like DBX and be able to blacklist certs and such.
func luksifyMeasurements(label string, publicKeyPcrs []string, pcrs []string, logger sdkLogger.KairosLogger, argsCreate ...string) error {
	// Only settle (no trigger) - we haven't made device changes yet, just need pending events to complete
	if err := UdevAdmSettle(&logger, udevTimeout); err != nil {
		return err
	}

	info, err := findPartitionByLabel(label)
	if err != nil {
		return err
	}

	// On TPM locking we generate a random password that will only be used here then discarded.
	// only unlocking method will be PCR values
	pass := getRandomString(32)
	mapper := partitionMapperPath(info)
	device := info.Path

	extraArgs := []string{"--uuid", uuid.NewV5(uuid.NamespaceURL, label).String()}
	extraArgs = append(extraArgs, "--label", label)
	extraArgs = append(extraArgs, argsCreate...)

	// Unmount the device if it's mounted before attempting to encrypt it
	if err := unmountIfMounted(device, logger); err != nil {
		logger.Err(err).Msg("unmount device")
		return err
	}

	if err := createLuks(device, pass, extraArgs...); err != nil {
		return err
	}

	if len(publicKeyPcrs) == 0 {
		publicKeyPcrs = []string{"11"}
	}

	syscall.Sync()

	_ = os.Setenv("SYSTEMD_LOG_LEVEL", "debug")
	defer os.Unsetenv("SYSTEMD_LOG_LEVEL")

	// Enroll PCR policy as a keyslot
	// We pass the current signature of the booted system to confirm that we would be able to unlock with the current booted system
	// That checks the policy against the signatures and fails if a UKI with those signatures wont be able to unlock the device
	// Files are generated by systemd automatically and are extracted from the UKI binary directly
	// public pem cert -> .pcrpkey section fo the elf file
	// signatures -> .pcrsig section of the elf file
	args := []string{
		"--tpm2-public-key=/run/systemd/tpm2-pcr-public-key.pem",
		fmt.Sprintf("--tpm2-public-key-pcrs=%s", strings.Join(publicKeyPcrs, "+")),
		fmt.Sprintf("--tpm2-pcrs=%s", strings.Join(pcrs, "+")),
		"--tpm2-signature=/run/systemd/tpm2-pcr-signature.json",
		"--tpm2-device-key=/run/systemd/tpm2-srk-public-key.tpm2b_public",
		device}
	logger.Logger.Debug().Str("args", strings.Join(args, " ")).Msg("running command")
	cmd := exec.Command("systemd-cryptenroll", args...)
	cmd.Env = append(cmd.Env, fmt.Sprintf("PASSWORD=%s", pass), "SYSTEMD_LOG_LEVEL=debug") // cannot pass it via stdin
	// Store the output into a buffer to log it in case we need it
	// debug output goes to stderr for some reason?
	stdOut := bytes.Buffer{}
	cmd.Stdout = &stdOut
	cmd.Stderr = &stdOut
	err = cmd.Run()
	if err != nil {
		logger.Logger.Debug().Str("output", stdOut.String()).Msg("debug from cryptenroll")
		logger.Err(err).Msg("Enrolling measurements")
		return err
	}

	logger.Logger.Debug().Str("output", stdOut.String()).Msg("debug from cryptenroll")

	err = formatLuks(device, info.Name, mapper, label, pass, logger)
	if err != nil {
		logger.Err(err).Msg("format luks")
		return err
	}

	// Delete password slot from luks device
	out, err := utils.SH(fmt.Sprintf("systemd-cryptenroll --wipe-slot=password %s", device))
	if err != nil {
		logger.Err(err).Str("out", out).Msg("Removing password")
		return err
	}
	return nil
}

// format luks will unlock the device, wait for it and then format it
// device is the actual /dev/X luks device
// label is the label we will set to the formatted partition
// password is the pass to unlock the device to be able to format the underlying mapper.
func formatLuks(device, name, mapper, label, pass string, logger sdkLogger.KairosLogger) error {
	l := logger.Logger.With().Str("device", device).Str("name", name).Str("mapper", mapper).Logger()
	l.Debug().Msg("unlock")
	if err := luksUnlock(device, name, pass, &logger); err != nil {
		return fmt.Errorf("unlock err: %w", err)
	}

	l.Debug().Msg("wait device")
	if err := waitDevice(mapper, 10); err != nil {
		return fmt.Errorf("waitdevice err: %w", err)
	}

	l.Debug().Msg("format")
	cmdFormat := fmt.Sprintf("mkfs.ext4 -L %s %s", label, mapper)
	out, err := utils.SH(cmdFormat)
	if err != nil {
		return fmt.Errorf("mkfs err: %w, out: %s", err, out)
	}

	// Refresh needs the password as its doing actions on the device directly
	l.Debug().Msg("discards")
	// Note: cryptsetup v2.8+ expects the device name (not the device path) for the 'refresh' command.
	// Using 'name' with v2.7 also works, hence why no fallback is needed for backward compatibility.
	cmd := exec.Command("cryptsetup", "refresh", "--persistent", "--allow-discards", name)
	cmd.Stdin = strings.NewReader(pass)
	output, err := cmd.CombinedOutput()

	if err != nil {
		return fmt.Errorf("refresh err: %w, out: %s", err, string(output))
	}

	l.Debug().Msg("close")
	out, err = utils.SH(fmt.Sprintf("cryptsetup close %s", mapper))
	if err != nil {
		return fmt.Errorf("lock err: %w, out: %s", err, out)
	}

	return nil
}

func waitDevice(device string, attempts int) error {
	for tries := 0; tries < attempts; tries++ {
		// Just settle, no trigger - we're waiting for the device to appear
		if err := UdevAdmSettle(nil, 10*time.Second); err != nil {
			return err
		}
		_, err := os.Lstat(device)
		if !os.IsNotExist(err) {
			return nil
		}
		time.Sleep(1 * time.Second)
	}
	return fmt.Errorf("no device found %s", device)
}

// UdevAdmTrigger triggers udev events for all subsystems and devices.
// This replays all udev rules and should be called after device-changing operations.
// The logger parameter can be nil for silent operation.
func UdevAdmTrigger(logger *sdkLogger.KairosLogger) error {
	if logger != nil {
		logger.Logger.Info().Msg("Triggering udev events")
	}

	// Trigger subsystems and devices (this replays all udev rules)
	cmd1 := exec.Command("udevadm", "trigger", "--action=add", "--type=subsystems")
	output1, err := cmd1.CombinedOutput()
	if err != nil {
		return fmt.Errorf("udevadm trigger subsystems failed: %v (output: %s)", err, string(output1))
	}

	cmd2 := exec.Command("udevadm", "trigger", "--action=add", "--type=devices")
	output2, err := cmd2.CombinedOutput()
	if err != nil {
		return fmt.Errorf("udevadm trigger devices failed: %v (output: %s)", err, string(output2))
	}

	return nil
}

// UdevAdmSettle waits for pending udev events to complete.
// Use this when you need to ensure udev has finished processing pending events.
// The logger parameter can be nil for silent operation.
func UdevAdmSettle(logger *sdkLogger.KairosLogger, timeout time.Duration) error {
	if logger != nil {
		logger.Logger.Info().Msg("Flushing filesystem buffers (sync)")
	}
	syscall.Sync()

	if logger != nil {
		logger.Logger.Info().Dur("timeout", timeout).Msg("Waiting for udev to settle")
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "udevadm", "settle")
	output, err := cmd.CombinedOutput()

	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("udevadm settle timed out after %s", timeout)
	}
	if err != nil {
		return fmt.Errorf("udevadm settle failed: %v (output: %s)", err, string(output))
	}

	if logger != nil {
		logger.Logger.Info().Msg("udevadm settle completed successfully")
	}

	return nil
}

// UdevAdmTriggerSettle triggers udev events and then waits for them to complete.
// Use this after device-changing operations (LUKS encrypt/unlock, partition creation).
// The logger parameter can be nil for silent operation.
func UdevAdmTriggerSettle(logger *sdkLogger.KairosLogger, timeout time.Duration) error {
	if err := UdevAdmTrigger(logger); err != nil {
		return err
	}
	return UdevAdmSettle(logger, timeout)
}
