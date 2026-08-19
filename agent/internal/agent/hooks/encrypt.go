package hook

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/kairos-io/kairos-agent/v2/pkg/constants"
	internalutils "github.com/kairos-io/kairos-agent/v2/pkg/utils"
	fsutils "github.com/kairos-io/kairos-agent/v2/pkg/utils/fs"
	"github.com/kairos-io/kairos-sdk/kcrypt"
	"github.com/kairos-io/kairos-sdk/machine"
	sdkConfig "github.com/kairos-io/kairos-sdk/types/config"
	"github.com/kairos-io/kairos-sdk/utils"
)

// Encrypt is the unified encryption method that works for both UKI and non-UKI modes
func Encrypt(c sdkConfig.Config) error {
	c.Logger.Logger.Info().Msg("Starting unified encryption flow")

	partitions := determinePartitionsToEncrypt(c)
	if len(partitions) == 0 {
		c.Logger.Logger.Info().Msg("No partitions to encrypt")
		return nil
	}
	c.Logger.Logger.Info().Strs("partitions", partitions).Msg("Partitions to encrypt")

	// Get the appropriate encryptor based on configuration
	// IMPORTANT: Do this BEFORE unmounting partitions so the encryptor can scan
	// for kcrypt configuration in /run/cos/oem
	encryptor, err := kcrypt.GetEncryptor(c.Logger)
	if err != nil {
		return fmt.Errorf("failed to get encryptor: %w", err)
	}
	c.Logger.Logger.Info().Str("method", encryptor.Name()).Msg("Using encryption method")

	// Backup OEM if it's in the list (before unmounting!)
	var oemBackupPath string
	var cleanupBackup func()
	needsOEMBackup := slices.Contains(partitions, constants.OEMLabel)
	if needsOEMBackup {
		var err error
		oemBackupPath, cleanupBackup, err = backupOEMIfNeeded(c)
		if err != nil {
			return fmt.Errorf("failed to backup OEM: %w", err)
		}
		defer cleanupBackup()
	}

	// Prepare partitions (unmount them)
	if err := preparePartitionsForEncryption(c, partitions); err != nil {
		return fmt.Errorf("failed to prepare partitions: %w", err)
	}

	if err := encryptor.Encrypt(partitions); err != nil {
		return fmt.Errorf("failed to encrypt partitions: %w", err)
	}

	// The Unlock method will wait for partitions to be ready before returning
	if err := encryptor.Unlock(partitions); err != nil {
		// Lock partitions on failure (cleanup)
		lockPartitions(c.Logger)
		return fmt.Errorf("failed to unlock partitions: %w", err)
	}

	// Restore OEM if needed
	if needsOEMBackup {
		if err := restoreOEM(c, oemBackupPath); err != nil {
			return fmt.Errorf("failed to restore OEM: %w", err)
		}
	}

	c.Logger.Logger.Info().Msg("Finished unified encryption flow")
	return nil
}

// Helper methods for unified encryption flow

// determinePartitionsToEncrypt returns the list of partitions to encrypt based on mode
func determinePartitionsToEncrypt(c sdkConfig.Config) []string {
	// If user has specified partitions, respect their preference
	if len(c.Install.Encrypt) > 0 {
		return c.Install.Encrypt
	}

	// No user-specified partitions
	if internalutils.IsUki() {
		// UKI mode: encrypt OEM and PERSISTENT by default
		return []string{constants.OEMLabel, constants.PersistentLabel}
	}

	// Non-UKI mode with no user-specified partitions: don't encrypt anything
	return []string{}
}

// preparePartitionsForEncryption unmounts all partitions that will be encrypted
func preparePartitionsForEncryption(c sdkConfig.Config, partitions []string) error {
	for _, p := range partitions {
		c.Logger.Logger.Info().Str("partition", p).Msg("Preparing to encrypt partition")

		// Unmount the partition before encrypting it
		// Find the device path for this partition label
		devPath, err := utils.SH(fmt.Sprintf("blkid -L %s", p))
		if err != nil {
			c.Logger.Logger.Warn().Str("label", p).Err(err).Msg("Could not find device for label")
		} else {
			devPath = strings.TrimSpace(devPath)
			c.Logger.Logger.Info().Str("device", devPath).Str("label", p).Msg("Found device for label")

			// Find all mount points for this device and unmount them
			mountPoints, _ := utils.SH(fmt.Sprintf("findmnt -n -o TARGET -S %s", devPath))
			if mountPoints != "" {
				for _, mp := range strings.Split(strings.TrimSpace(mountPoints), "\n") {
					if mp != "" {
						c.Logger.Logger.Info().Str("device", devPath).Str("mountpoint", mp).Msg("Unmounting partition")
						if err := machine.Umount(mp); err != nil {
							c.Logger.Logger.Warn().Str("mountpoint", mp).Err(err).Msg("Could not unmount")
						}
					}
				}
			}
		}
	}
	return nil
}

// backupOEMIfNeeded backs up the OEM partition contents before encryption
func backupOEMIfNeeded(c sdkConfig.Config) (backupPath string, cleanup func(), err error) {
	c.Logger.Logger.Info().Msg("Backing up OEM partition before encryption")

	// Check if OEM is already mounted
	_, err = utils.SH(fmt.Sprintf("findmnt %s", constants.OEMDir))
	oemAlreadyMounted := (err == nil)

	// Mount OEM partition if not already mounted
	if !oemAlreadyMounted {
		c.Logger.Logger.Info().Msg("Mounting OEM partition for backup")
		err = machine.Mount(constants.OEMLabel, constants.OEMDir)
		if err != nil {
			return "", nil, fmt.Errorf("failed to mount OEM for backup: %w", err)
		}
	} else {
		c.Logger.Logger.Info().Msg("OEM already mounted, using existing mount")
	}

	// Create temporary directory for backup
	tmpDir, err := fsutils.TempDir(c.Fs, "", "oem-backup-xxxx")
	if err != nil {
		if !oemAlreadyMounted {
			machine.Umount(constants.OEMDir) //nolint:errcheck
		}
		return "", nil, fmt.Errorf("failed to create temp dir for OEM backup: %w", err)
	}

	// Sync OEM data to temp directory
	err = internalutils.SyncData(c.Logger, c.Runner, c.Fs, constants.OEMDir, tmpDir, []string{}...)
	if err != nil {
		c.Fs.RemoveAll(tmpDir) //nolint:errcheck
		if !oemAlreadyMounted {
			machine.Umount(constants.OEMDir) //nolint:errcheck
		}
		return "", nil, fmt.Errorf("failed to sync OEM data: %w", err)
	}

	// Unmount OEM (it will be unmounted again by preparePartitionsForEncryption, but that's ok)
	err = machine.Umount(constants.OEMDir) //nolint:errcheck
	if err != nil {
		c.Fs.RemoveAll(tmpDir) //nolint:errcheck
		return "", nil, fmt.Errorf("failed to unmount OEM after backup: %w", err)
	}

	// Return cleanup function that removes the temp directory
	cleanup = func() {
		c.Logger.Logger.Info().Str("path", tmpDir).Msg("Cleaning up OEM backup")
		c.Fs.RemoveAll(tmpDir) //nolint:errcheck
	}

	c.Logger.Logger.Info().Str("backup_path", tmpDir).Msg("OEM backup completed")
	return tmpDir, cleanup, nil
}

// findMapperDeviceForPartition finds an unlocked dm-crypt mapper device for a partition label.
// It first finds the underlying partition device using the label, then checks if a mapper
// device exists for that partition in dmsetup.
// Returns the full path to the mapper device (e.g., /dev/mapper/vda2).
func findMapperDeviceForPartition(c sdkConfig.Config, label string) (string, error) {
	// First, find the underlying encrypted partition device by its label
	// This will return the LUKS container device (e.g., /dev/vda2)
	partitionPath, err := utils.SH(fmt.Sprintf("blkid -L %s", label))
	if err != nil {
		return "", fmt.Errorf("failed to find partition with label %s: %w", label, err)
	}
	partitionPath = strings.TrimSpace(partitionPath)
	c.Logger.Logger.Debug().Str("partition", partitionPath).Str("label", label).Msg("Found encrypted partition")

	// Extract the base device name (e.g., "vda2" from "/dev/vda2" or "/dev/mapper/vda2")
	// After encryption, blkid might return the mapper device instead of the underlying device
	baseName := strings.TrimPrefix(partitionPath, "/dev/mapper/")
	baseName = strings.TrimPrefix(baseName, "/dev/")
	mapperPath := fmt.Sprintf("/dev/mapper/%s", baseName)

	// Get list of active encrypted mapper devices to verify it's unlocked
	dmOutput, err := utils.SH("dmsetup ls --target crypt")
	if err != nil {
		return "", fmt.Errorf("failed to list dm-crypt devices: %w", err)
	}

	c.Logger.Logger.Info().
		Str("dmsetup_output", strings.TrimSpace(dmOutput)).
		Str("looking_for", baseName).
		Str("partition_path", partitionPath).
		Str("label", label).
		Msg("Checking dmsetup for mapper device")

	// Check if our mapper device is in the list of active devices
	mapperExists := false
	lines := strings.Split(strings.TrimSpace(dmOutput), "\n")
	for _, line := range lines {
		if line == "" || strings.Contains(line, "No devices found") {
			continue
		}

		// Extract mapper name (first field before whitespace)
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		mapperName := fields[0]

		c.Logger.Logger.Info().
			Str("line", line).
			Str("mapper_name", mapperName).
			Str("looking_for", baseName).
			Bool("matches", mapperName == baseName).
			Msg("Comparing mapper names")

		if mapperName == baseName {
			mapperExists = true
			c.Logger.Logger.Info().Str("mapper", mapperPath).Str("partition", baseName).Msg("Found unlocked mapper device")
			break
		}
	}

	if !mapperExists {
		return "", fmt.Errorf("partition %s (label: %s) is not unlocked - mapper device %s not found in dmsetup", partitionPath, label, baseName)
	}

	return mapperPath, nil
}

// restoreOEM restores the OEM partition contents after encryption.
// This function assumes:
// - COS_OEM partition is encrypted
// - COS_OEM has been unlocked (mapper device exists in dmsetup)
// - We need to find the mapper device path to mount and restore data
func restoreOEM(c sdkConfig.Config, backupPath string) error {
	c.Logger.Logger.Info().Str("backup_path", backupPath).Msg("Restoring OEM partition from backup")

	// Find the unlocked mapper device for COS_OEM
	// We find the underlying partition by label, then check if it's unlocked in dmsetup
	devicePath, err := findMapperDeviceForPartition(c, constants.OEMLabel)
	if err != nil {
		return fmt.Errorf("failed to find unlocked OEM mapper device: %w", err)
	}

	c.Logger.Logger.Info().Str("device", devicePath).Msg("Found unlocked OEM mapper device")

	// Wait for udev to finish processing - no trigger needed since unlock already happened
	c.Logger.Logger.Info().Str("device", devicePath).Msg("Waiting for udev to create device node")
	if err := kcrypt.UdevAdmSettle(&c.Logger, 15*time.Second); err != nil {
		return fmt.Errorf("failed settling udev: %w", err)
	}

	// Create mount point if it doesn't exist
	if err := fsutils.MkdirAll(c.Fs, constants.OEMDir, 0755); err != nil {
		return fmt.Errorf("failed to create mount point: %w", err)
	}

	// Mount the unlocked device
	c.Logger.Logger.Info().Str("device", devicePath).Str("mountpoint", constants.OEMDir).Msg("Mounting OEM partition")

	// First check what filesystem is on the device
	fsType, _ := utils.SH(fmt.Sprintf("blkid -s TYPE -o value %s", devicePath))
	c.Logger.Logger.Info().Str("device", devicePath).Str("fs_type", strings.TrimSpace(fsType)).Msg("Filesystem type on mapper device")

	mountOut, err := utils.SH(fmt.Sprintf("mount %s %s", devicePath, constants.OEMDir))
	if err != nil {
		c.Logger.Logger.Error().Str("mount_output", mountOut).Str("device", devicePath).Msg("Mount failed")
		return fmt.Errorf("failed to mount OEM for restore: %w (output: %s)", err, mountOut)
	}

	// Copy back the contents of the OEM partition that we saved before encrypting
	err = internalutils.SyncData(c.Logger, c.Runner, c.Fs, backupPath, constants.OEMDir, []string{}...)
	if err != nil {
		machine.Umount(constants.OEMDir) //nolint:errcheck
		return fmt.Errorf("failed to restore OEM data: %w", err)
	}

	// Unmount the OEM partition and leave everything unmounted
	err = machine.Umount(constants.OEMDir)
	if err != nil {
		return fmt.Errorf("failed to unmount OEM after restore: %w", err)
	}

	c.Logger.Logger.Info().Msg("OEM partition restored successfully")
	return nil
}
