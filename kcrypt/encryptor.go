package kcrypt

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gofrs/uuid"
	"github.com/kairos-io/kairos-sdk/collector"
	"github.com/kairos-io/kairos-sdk/ghw"
	"github.com/kairos-io/kairos-sdk/kcrypt/bus"
	"github.com/kairos-io/kairos-sdk/state"
	sdkLogger "github.com/kairos-io/kairos-sdk/types/logger"
	"github.com/kairos-io/kairos-sdk/types/partitions"
	"github.com/kairos-io/kairos-sdk/utils"
	"github.com/mudler/go-pluggable"
)

// PartitionEncryptor defines the interface for encrypting and decrypting partitions.
type PartitionEncryptor interface {
	Encrypt(partitions []string) error
	Unlock(partitions []string) error
	Name() string
	Validate() error
}

// RemoteKMSEncryptor encrypts partitions using a remote KMS (kcrypt-challenger).
type RemoteKMSEncryptor struct {
	logger       sdkLogger.KairosLogger
	kcryptConfig *bus.KcryptConfig
}

func (e *RemoteKMSEncryptor) Encrypt(partitions []string) error {
	e.logger.Logger.Info().Str("method", e.Name()).Strs("partitions", partitions).Msg("Encrypting partitions")

	for _, partition := range partitions {
		e.logger.Logger.Info().Str("partition", partition).Msg("Encrypting partition")

		_, err := e.luksify(partition)
		if err != nil {
			return fmt.Errorf("failed to encrypt partition %s: %w", partition, err)
		}

		e.logger.Logger.Info().Str("partition", partition).Msg("Successfully encrypted partition")
	}

	return nil
}

func (e *RemoteKMSEncryptor) Unlock(partitions []string) error {
	e.logger.Logger.Info().Str("method", e.Name()).Strs("partitions", partitions).Msg("Unlocking encrypted partitions")

	// Unlock each partition and wait for it to be ready
	for _, partitionLabel := range partitions {
		if err := e.unlockPartition(partitionLabel); err != nil {
			return fmt.Errorf("failed to unlock partition %s: %w", partitionLabel, err)
		}
	}

	e.logger.Logger.Info().Msg("All partitions unlocked successfully")
	return nil
}

func (e *RemoteKMSEncryptor) unlockPartition(partitionLabel string) error {
	var lastErr error

	for attempt := 0; attempt < 10; attempt++ {
		if attempt > 0 {
			e.logger.Logger.Info().
				Str("partition", partitionLabel).
				Int("attempt", attempt).
				Msg("Retrying unlock")
			time.Sleep(time.Duration(attempt) * time.Second)
		}

		// Find partition information
		info, err := findPartitionByLabel(partitionLabel)
		if err != nil {
			lastErr = err
			e.logger.Logger.Debug().
				Str("partition", partitionLabel).
				Err(err).
				Msg("Failed to find partition, will retry")
			continue
		}

		// If partition is already unlocked, we're done
		if !partitionLocked(info) {
			return nil
		}

		e.logger.Logger.Debug().Msg("partition name: " + info.Name)

		// Get passphrase from remote KMS
		pass, err := e.getPasswordFromChallenger(info)
		if err != nil {
			lastErr = fmt.Errorf("failed to get password: %w", err)
			e.logger.Logger.Warn().
				Str("partition", partitionLabel).
				Err(lastErr).
				Msg("Failed to get password, will retry")
			continue
		}

		err = luksUnlock(info.Path, info.Name, pass, &e.logger)
		if err != nil {
			lastErr = fmt.Errorf("unlock failed: %w", err)
			e.logger.Logger.Warn().
				Str("partition", partitionLabel).
				Err(lastErr).
				Msg("Unlock failed, will retry")
			continue
		}

		// Verify the partition is now visible
		checkPath, _ := utils.SH(fmt.Sprintf("blkid -L %s", partitionLabel))
		if strings.TrimSpace(checkPath) != "" {
			e.logger.Logger.Info().
				Str("partition", partitionLabel).
				Msg("Partition unlocked and verified")
			return nil
		}

		lastErr = fmt.Errorf("partition unlocked but not visible")
	}

	return fmt.Errorf("failed after 10 attempts: %w", lastErr)
}

func (e *RemoteKMSEncryptor) Name() string {
	return "Remote KMS"
}

func (e *RemoteKMSEncryptor) Validate() error {
	// No special validation needed for remote KMS
	// The actual connection will be validated when encrypting
	return nil
}

// luksify Take a part label, and recreates it with LUKS. IT OVERWRITES DATA!.
// On success, it returns a machine parseable string with the partition information
// (label:name:uuid) so that it can be stored by the caller for later use.
// This is because the label of the encrypted partition is not accessible unless
// the partition is decrypted first and the uuid changed after encryption so
// any stored information needs to be updated (by the caller).
func (e *RemoteKMSEncryptor) luksify(label string, argsCreate ...string) (string, error) {
	var pass string

	// Only settle (no trigger) - we haven't made device changes yet, just need pending events to complete
	e.logger.Logger.Info().Msg("Waiting for udevadm to settle before finding partition")
	if err := UdevAdmSettle(&e.logger, udevTimeout); err != nil {
		return "", err
	}

	e.logger.Logger.Info().Str("label", label).Msg("Finding partition")
	info, err := findPartitionByLabel(label)
	if err != nil {
		e.logger.Err(err).Msg("find partition")
		return "", err
	}

	e.logger.Logger.Info().Str("partition", label).Msg("Getting password from kcrypt-challenger")
	pass, err = e.getPasswordFromChallenger(info)
	if err != nil {
		e.logger.Err(err).Msg("get password")
		return "", err
	}

	// Log that we received a passphrase (without revealing it)
	e.logger.Logger.Info().
		Str("partition", label).
		Int("passphrase_length", len(pass)).
		Msg("ENCRYPTION: Received passphrase from kcrypt-challenger")

	mapper := partitionMapperPath(info)
	device := info.Path

	extraArgs := []string{"--uuid", uuid.NewV5(uuid.NamespaceURL, label).String()}
	extraArgs = append(extraArgs, "--label", label)
	extraArgs = append(extraArgs, argsCreate...)

	// Unmount the device if it's mounted before attempting to encrypt it
	e.logger.Logger.Info().Str("device", device).Msg("Checking if device is mounted")
	if err := unmountIfMounted(device, e.logger); err != nil {
		e.logger.Err(err).Msg("unmount device")
		return "", err
	}

	e.logger.Logger.Info().Str("device", device).Msg("Creating LUKS container")
	if err := createLuks(device, pass, extraArgs...); err != nil {
		e.logger.Err(err).Msg("create luks")
		return "", err
	}

	e.logger.Logger.Info().Str("device", device).Str("label", label).Msg("Formatting LUKS container")
	err = formatLuks(device, info.Name, mapper, label, pass, e.logger)
	if err != nil {
		e.logger.Err(err).Msg("format luks")
		return "", err
	}

	e.logger.Logger.Info().Str("label", label).Msg("Partition encryption completed")
	return fmt.Sprintf("%s:%s:%s", info.FilesystemLabel, info.Name, info.UUID), nil
}

// getPasswordFromChallenger gets the password for a types.Partition using KcryptConfig.
// It constructs the DiscoveryPasswordPayload internally for communication with the plugin.
func (e *RemoteKMSEncryptor) getPasswordFromChallenger(b *partitions.Partition) (password string, err error) {
	// Get a logger for debugging
	log := sdkLogger.NewKairosLogger("kcrypt-getPassword", "info", false)
	defer log.Close()

	log.Logger.Info().
		Str("partition_name", b.Name).
		Str("partition_label", b.FilesystemLabel).
		Str("partition_uuid", b.UUID).
		Msg("Requesting password for partition")

	bus.Reload()

	bus.Manager.Response(bus.EventDiscoveryPassword, func(_ *pluggable.Plugin, r *pluggable.EventResponse) {
		password = r.Data
		if r.Errored() {
			err = fmt.Errorf("failed discovery: %s", r.Error)
			log.Logger.Error().Err(err).Msg("Plugin returned error")
		} else {
			log.Logger.Info().
				Int("password_length", len(password)).
				Str("partition", b.Name).
				Msg("DECRYPTION: Received password from plugin")
		}
	})

	// Use kcryptConfig from the encryptor, scanning if not provided
	kcryptConfig := e.kcryptConfig
	if kcryptConfig == nil {
		kcryptConfig = ScanKcryptConfig(e.logger)
	}

	// Construct DiscoveryPasswordPayload from KcryptConfig + partition
	// This is where we create the payload that will be sent to kcrypt-challenger
	var payload bus.DiscoveryPasswordPayload
	payload.Partition = b
	if kcryptConfig != nil {
		payload.ChallengerServer = kcryptConfig.ChallengerServer
		payload.MDNS = kcryptConfig.MDNS
		log.Logger.Info().
			Str("challenger_server", payload.ChallengerServer).
			Msg("Using provided kcrypt config")
	} else {
		log.Logger.Info().Msg("No kcrypt config provided, using local encryption")
	}

	_, err = bus.Manager.Publish(bus.EventDiscoveryPassword, payload)
	if err != nil {
		log.Logger.Error().Err(err).Msg("Failed to publish event to bus")
		return password, err
	}

	if password == "" {
		log.Logger.Error().Msg("Received empty password from plugin")
		return password, fmt.Errorf("received empty password")
	}

	log.Logger.Info().Msg("Password retrieval successful")
	return
}

// TPMWithPCREncryptor encrypts partitions using TPM with PCR policy (UKI mode).
type TPMWithPCREncryptor struct {
	logger         sdkLogger.KairosLogger
	bindPublicPCRs []string
	bindPCRs       []string
}

func (e *TPMWithPCREncryptor) Encrypt(partitions []string) error {
	e.logger.Logger.Info().Str("method", e.Name()).Strs("partitions", partitions).Msg("Encrypting partitions")

	for _, partition := range partitions {
		e.logger.Logger.Info().Str("partition", partition).Msg("Encrypting partition")

		err := luksifyMeasurements(partition, e.bindPublicPCRs, e.bindPCRs, e.logger)
		if err != nil {
			return fmt.Errorf("failed to encrypt partition %s: %w", partition, err)
		}

		e.logger.Logger.Info().Str("partition", partition).Msg("Successfully encrypted partition")
	}

	return nil
}

func (e *TPMWithPCREncryptor) Unlock(partitions []string) error {
	e.logger.Logger.Info().Str("method", e.Name()).Strs("partitions", partitions).Msg("Unlocking encrypted partitions")

	// TPM with PCR uses TPM-based unlock with systemd
	_ = os.Setenv("SYSTEMD_LOG_LEVEL", "debug")
	defer os.Unsetenv("SYSTEMD_LOG_LEVEL")

	// Unlock each partition and wait for it to be ready
	for _, partitionLabel := range partitions {
		if err := e.unlockPartition(partitionLabel); err != nil {
			return fmt.Errorf("failed to unlock partition %s: %w", partitionLabel, err)
		}
	}

	e.logger.Logger.Info().Msg("All partitions unlocked successfully")
	return nil
}

func (e *TPMWithPCREncryptor) unlockPartition(partitionLabel string) error {
	var lastErr error

	for attempt := 0; attempt < 10; attempt++ {
		if attempt > 0 {
			e.logger.Logger.Info().
				Str("partition", partitionLabel).
				Int("attempt", attempt).
				Msg("Retrying unlock")
			time.Sleep(time.Duration(attempt) * time.Second)
		}

		// Find partition information
		info, err := findPartitionByLabel(partitionLabel)
		if err != nil {
			lastErr = err
			e.logger.Logger.Debug().
				Str("partition", partitionLabel).
				Err(err).
				Msg("Failed to find partition, will retry")
			continue
		}

		// If partition is already unlocked, we're done
		if !partitionLocked(info) {
			return nil
		}

		// Attempt to unlock with TPM
		out, err := utils.SH(fmt.Sprintf("/usr/lib/systemd/systemd-cryptsetup attach %s %s - tpm2-device=auto", info.Name, info.Path))
		if err != nil {
			lastErr = fmt.Errorf("TPM unlock failed: %w (output: %s)", err, out)
			e.logger.Logger.Warn().
				Str("partition", partitionLabel).
				Err(lastErr).
				Msg("TPM unlock failed, will retry")
			continue
		}

		// Verify the partition is now visible
		checkPath, _ := utils.SH(fmt.Sprintf("blkid -L %s", partitionLabel))
		if strings.TrimSpace(checkPath) != "" {
			e.logger.Logger.Info().
				Str("partition", partitionLabel).
				Msg("Partition unlocked and verified")
			return nil
		}

		lastErr = fmt.Errorf("partition unlocked but not visible")
	}

	return fmt.Errorf("failed after 10 attempts: %w", lastErr)
}

func (e *TPMWithPCREncryptor) Name() string {
	return "TPM with PCR policy"
}

func (e *TPMWithPCREncryptor) Validate() error {
	// Validate systemd version (need >= 252 for systemd-cryptenroll)
	if err := validateSystemdVersion(e.logger, 252); err != nil {
		return err
	}

	// Validate TPM 2.0 device exists
	if err := validateTPMDevice(e.logger); err != nil {
		return err
	}

	return nil
}

// LocalTPMNVEncryptor encrypts partitions using local TPM NV passphrase storage.
type LocalTPMNVEncryptor struct {
	logger       sdkLogger.KairosLogger
	kcryptConfig *bus.KcryptConfig
}

func (e *LocalTPMNVEncryptor) Encrypt(partitions []string) error {
	e.logger.Logger.Info().Str("method", e.Name()).Strs("partitions", partitions).Msg("Encrypting partitions")

	// Extract TPM configuration
	nvIndex := DefaultLocalPassphraseNVIndex
	cIndex := ""
	tpmDevice := ""

	if e.kcryptConfig != nil {
		if e.kcryptConfig.NVIndex != "" {
			nvIndex = e.kcryptConfig.NVIndex
		}
		cIndex = e.kcryptConfig.CIndex
		tpmDevice = e.kcryptConfig.TPMDevice
	}

	for _, partition := range partitions {
		e.logger.Logger.Info().Str("partition", partition).Msg("Encrypting partition")

		_, err := encryptWithLocalTPMPassphrase(partition, nvIndex, cIndex, tpmDevice, e.logger)
		if err != nil {
			return fmt.Errorf("failed to encrypt partition %s: %w", partition, err)
		}

		e.logger.Logger.Info().Str("partition", partition).Msg("Successfully encrypted partition")
	}

	return nil
}

func (e *LocalTPMNVEncryptor) Unlock(partitions []string) error {
	e.logger.Logger.Info().Str("method", e.Name()).Strs("partitions", partitions).Msg("Unlocking encrypted partitions")

	// Unlock each partition and wait for it to be ready
	for _, partitionLabel := range partitions {
		if err := e.unlockPartition(partitionLabel); err != nil {
			return fmt.Errorf("failed to unlock partition %s: %w", partitionLabel, err)
		}
	}

	e.logger.Logger.Info().Msg("All partitions unlocked successfully")
	return nil
}

func (e *LocalTPMNVEncryptor) unlockPartition(partitionLabel string) error {
	var lastErr error

	for attempt := 0; attempt < 10; attempt++ {
		if attempt > 0 {
			e.logger.Logger.Info().
				Str("partition", partitionLabel).
				Int("attempt", attempt).
				Msg("Retrying unlock")
			time.Sleep(time.Duration(attempt) * time.Second)
		}

		// Find partition information
		info, err := findPartitionByLabel(partitionLabel)
		if err != nil {
			lastErr = err
			e.logger.Logger.Debug().
				Str("partition", partitionLabel).
				Err(err).
				Msg("Failed to find partition, will retry")
			continue
		}

		// If partition is already unlocked, we're done
		if !partitionLocked(info) {
			return nil
		}

		// Extract TPM configuration
		nvIndex := DefaultLocalPassphraseNVIndex
		cIndex := ""
		tpmDevice := ""
		if e.kcryptConfig != nil {
			if e.kcryptConfig.NVIndex != "" {
				nvIndex = e.kcryptConfig.NVIndex
			}
			cIndex = e.kcryptConfig.CIndex
			tpmDevice = e.kcryptConfig.TPMDevice
		}

		// Get passphrase from local TPM NV memory (not from remote)
		passphrase, err := getOrCreateLocalTPMPassphrase(nvIndex, cIndex, tpmDevice)
		if err != nil {
			lastErr = fmt.Errorf("failed to get passphrase from local TPM: %w", err)
			e.logger.Logger.Warn().
				Str("partition", partitionLabel).
				Err(lastErr).
				Msg("Failed to get local TPM passphrase, will retry")
			continue
		}

		// Unlock directly with the local passphrase
		err = luksUnlock(info.Path, info.Name, passphrase, &e.logger)
		if err != nil {
			lastErr = fmt.Errorf("unlock failed: %w", err)
			e.logger.Logger.Warn().
				Str("partition", partitionLabel).
				Err(lastErr).
				Msg("Unlock failed, will retry")
			continue
		}

		// Verify the partition is now visible
		checkPath, _ := utils.SH(fmt.Sprintf("blkid -L %s", partitionLabel))
		if strings.TrimSpace(checkPath) != "" {
			e.logger.Logger.Info().
				Str("partition", partitionLabel).
				Msg("Partition unlocked and verified")
			return nil
		}

		lastErr = fmt.Errorf("partition unlocked but not visible")
	}

	return fmt.Errorf("failed after 10 attempts: %w", lastErr)
}

func (e *LocalTPMNVEncryptor) Name() string {
	return "Local TPM NV passphrase"
}

func (e *LocalTPMNVEncryptor) Validate() error {
	// Validate TPM 2.0 device exists
	return validateTPMDevice(e.logger)
}

// GetEncryptor returns the appropriate encryptor based on system configuration.
// It automatically:
// 1. Scans for kcrypt configuration (challenger server, mdns, etc.)
// 2. Detects UKI mode
// 3. Extracts PCR bindings from config
// 4. Returns the appropriate encryptor implementation
//
// Decision logic:
// 1. If challenger_server configured OR mdns enabled -> Remote KMS
// 2. Else if UKI mode -> TPM + PCR policy (requires systemd >= 252 and TPM 2.0)
// 3. Else (non-UKI, no remote) -> Local TPM NV passphrase.
func GetEncryptor(logger sdkLogger.KairosLogger) (PartitionEncryptor, error) {
	kcryptConfig := ScanKcryptConfig(logger)

	isUKI := detectUKIMode(logger)

	var bindPCRs, bindPublicPCRs []string
	if isUKI {
		collectorConfig := scanCollectorConfig(logger)
		if collectorConfig != nil {
			bindPCRs, bindPublicPCRs = extractPCRBindingsFromCollector(*collectorConfig, logger)
		}
	}

	useRemoteKMS := kcryptConfig != nil && (kcryptConfig.ChallengerServer != "" || kcryptConfig.MDNS)

	var encryptor PartitionEncryptor

	if useRemoteKMS {
		logger.Logger.Info().
			Str("challenger_server", kcryptConfig.ChallengerServer).
			Bool("mdns", kcryptConfig.MDNS).
			Msg("Using remote KMS for encryption")
		encryptor = &RemoteKMSEncryptor{
			logger:       logger,
			kcryptConfig: kcryptConfig,
		}
	} else if isUKI {
		logger.Logger.Info().Msg("Using TPM with PCR policy for encryption (UKI mode)")
		encryptor = &TPMWithPCREncryptor{
			logger:         logger,
			bindPublicPCRs: bindPublicPCRs,
			bindPCRs:       bindPCRs,
		}
	} else {
		logger.Logger.Info().Msg("Using local TPM NV passphrase for encryption")
		encryptor = &LocalTPMNVEncryptor{
			logger:       logger,
			kcryptConfig: kcryptConfig,
		}
	}

	if err := encryptor.Validate(); err != nil {
		return nil, fmt.Errorf("encryptor validation failed: %w", err)
	}

	return encryptor, nil
}

// scanCollectorConfig scans for configuration and returns the collector config.
func scanCollectorConfig(logger sdkLogger.KairosLogger) *collector.Config {
	o := &collector.Options{NoLogs: true, MergeBootCMDLine: true}
	if err := o.Apply(collector.Directories(DefaultConfigDirs...)); err != nil {
		logger.Debugf("scanCollectorConfig: error applying collector options: %v", err)
		return nil
	}

	collectorConfig, err := collector.Scan(o, func(d []byte) ([]byte, error) {
		return d, nil
	})
	if err != nil {
		logger.Debugf("scanCollectorConfig: error scanning for config: %v", err)
		return nil
	}

	return collectorConfig
}

// detectUKIMode detects if the system is running in UKI mode
// This checks for the presence of UKI-specific indicators.
func detectUKIMode(logger sdkLogger.KairosLogger) bool {
	cmdline, err := os.ReadFile("/proc/cmdline")
	if err != nil {
		logger.Logger.Debug().Err(err).Msg("Error reading /proc/cmdline file " + err.Error())
		return false
	}

	return state.DetectUKIboot(string(cmdline))
}

// partitionLocked returns true if the partition is currently locked (encrypted and not unlocked).
func partitionLocked(p *partitions.Partition) bool {
	if p == nil {
		return false
	}
	return !utils.Exists(partitionMapperPath(p))
}

// partitionMapperPath returns the device mapper path for the partition (e.g., /dev/mapper/sda1).
func partitionMapperPath(p *partitions.Partition) string {
	if p == nil {
		return ""
	}
	return filepath.Join("/dev", "mapper", p.Name)
}

// findPartitionByLabel finds a partition by its filesystem label and returns its information.
// It performs all the common logic needed before attempting to unlock a partition.
// Returns an error if the partition is not found. Use partitionLocked() to check if the partition is locked.
func findPartitionByLabel(partitionLabel string) (*partitions.Partition, error) {
	// Find the partition device by label
	devicePath, err := utils.SH(fmt.Sprintf("blkid -L %s", partitionLabel))
	devicePath = strings.TrimSpace(devicePath)
	legacyName := legacyPartitionName(partitionLabel)
	if (err != nil || devicePath == "") && legacyName != "" {
		devicePath, err = utils.SH(fmt.Sprintf("blkid -t PARTLABEL=%s -o device", legacyName))
		devicePath = strings.TrimSpace(devicePath)
	}

	if err != nil || devicePath == "" {
		return nil, fmt.Errorf("partition not found")
	}

	logger := sdkLogger.NewNullLogger()
	disks := ghw.GetDisks(ghw.NewPaths(""), &logger)
	if disks == nil {
		return nil, fmt.Errorf("failed to scan block devices")
	}

	var partition *partitions.Partition
	for _, disk := range disks {
		for _, p := range disk.Partitions {
			if p.FilesystemLabel == partitionLabel || (legacyName != "" && p.PartitionLabel == legacyName) {
				partition = p
				break
			}
		}
		if partition != nil {
			break
		}
	}

	if partition == nil {
		return nil, fmt.Errorf("partition not found in block devices")
	}

	// Ensure Path is set to the device path found by blkid
	// This ensures we use the actual device path even if Partition.Path wasn't set
	if partition.Path == "" {
		partition.Path = devicePath
	}
	// Ensure Name is set if it wasn't populated
	if partition.Name == "" {
		partition.Name = filepath.Base(devicePath)
	}

	return partition, nil
}

// validateSystemdVersion checks if systemd version is >= required version.
func validateSystemdVersion(logger sdkLogger.KairosLogger, minVersion int) error {
	run, err := utils.SH("systemctl --version | head -1 | awk '{ print $2}'")
	systemdVersion := strings.TrimSpace(string(run))
	if err != nil {
		logger.Errorf("could not get systemd version: %s", err)
		return fmt.Errorf("could not get systemd version: %w", err)
	}
	if systemdVersion == "" {
		return fmt.Errorf("could not get systemd version: empty output")
	}

	// Extract the numeric portion of the version string using a regular expression.
	re := regexp.MustCompile(`\d+`)
	matches := re.FindString(systemdVersion)
	if matches == "" {
		return fmt.Errorf("could not extract numeric part from systemd version: %s", systemdVersion)
	}

	systemdVersionInt, err := strconv.Atoi(matches)
	if err != nil {
		return fmt.Errorf("could not convert systemd version to int: %w", err)
	}

	if systemdVersionInt < minVersion {
		return fmt.Errorf("systemd version is %d, we need %d or higher for encrypting partitions with PCR policy", systemdVersionInt, minVersion)
	}

	logger.Logger.Info().Int("version", systemdVersionInt).Int("required", minVersion).Msg("Systemd version check passed")
	return nil
}

// validateTPMDevice checks if TPM 2.0 device exists.
func validateTPMDevice(logger sdkLogger.KairosLogger) error {
	// Check for a TPM 2.0 device as it's needed to encrypt
	// Exposed by the kernel to userspace as /dev/tpmrm0 since kernel 4.12
	// https://git.kernel.org/pub/scm/linux/kernel/git/torvalds/linux.git/commit/?id=fdc915f7f71939ad5a3dda3389b8d2d7a7c5ee66
	_, err := os.Stat("/dev/tpmrm0")
	if err != nil {
		logger.Warnf("Could not find TPM 2.0 device at /dev/tpmrm0")
		return fmt.Errorf("could not find TPM 2.0 device at /dev/tpmrm0: %w", err)
	}

	logger.Logger.Info().Msg("TPM 2.0 device found at /dev/tpmrm0")
	return nil
}
