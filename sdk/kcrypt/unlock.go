package kcrypt

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/anatol/luks.go"
	"github.com/kairos-io/kairos-sdk/constants"
	"github.com/kairos-io/kairos-sdk/ghw"
	sdkLogger "github.com/kairos-io/kairos-sdk/types/logger"
	"github.com/kairos-io/kairos-sdk/types/partitions"
	"github.com/kairos-io/kairos-sdk/utils"
)

func encryptedPartitionLabel(partition *partitions.Partition) string {
	if partition == nil {
		return ""
	}

	if partition.FilesystemLabel != "" && partition.FilesystemLabel != "unknown" {
		return partition.FilesystemLabel
	}

	switch partition.PartitionLabel {
	case constants.OEMPartName:
		return constants.OEMLabel
	case constants.PersistentPartName:
		return constants.PersistentLabel
	default:
		return ""
	}
}

func legacyPartitionName(filesystemLabel string) string {
	switch filesystemLabel {
	case constants.OEMLabel:
		return constants.OEMPartName
	case constants.PersistentLabel:
		return constants.PersistentPartName
	default:
		return ""
	}
}

func luksUnlock(device, mapper, password string, logger *sdkLogger.KairosLogger) error {
	// Check if device exists and is accessible
	if _, err := os.Stat(device); err != nil {
		return fmt.Errorf("device not accessible: %v", err)
	}

	// Construct mapper path
	mapperPath := filepath.Join("/dev", "mapper", mapper)

	// Check if mapper already exists
	if _, err := os.Stat(mapperPath); err == nil {
		// Already unlocked
		if logger != nil {
			logger.Logger.Debug().Str("mapper", mapperPath).Msg("Mapper already exists")
		}
		return nil
	}

	// Try to unlock with retries - the luks.go library sometimes has timing issues
	// when unlocking multiple partitions in sequence
	var dev luks.Device
	var unlockErr error
	maxRetries := 3

	defer func() {
		if dev != nil {
			_ = dev.Close() // might be already closed. Just to be sure.
		}
	}()

	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * time.Second)
			// Just settle, no trigger - we're waiting for pending events before retrying
			if err := UdevAdmSettle(logger, 10*time.Second); err != nil {
				if logger != nil {
					logger.Logger.Warn().
						Int("attempt", attempt+1).
						Err(err).Msg("Failed to settle")
				}
			}
		}

		dev, unlockErr = luks.Open(device)
		if unlockErr != nil {
			if logger != nil {
				logger.Logger.Warn().
					Int("attempt", attempt+1).
					Int("max_retries", maxRetries).
					Str("device", device).
					Err(unlockErr).
					Msg("Failed to open device")
			}

			continue
		}

		// Try to unlock
		unlockErr = dev.Unlock(0, []byte(password), mapper)
		if unlockErr != nil {
			_ = dev.Close() // Close on error so that the next retry opens it again
			if logger != nil {
				logger.Logger.Warn().
					Int("attempt", attempt+1).
					Int("max_retries", maxRetries).
					Str("device", device).
					Err(unlockErr).
					Msg("Failed to unlock device")
			}

			continue
		}

		// Success! Close the device handle immediately to release the file descriptor
		_ = dev.Close()
		if logger != nil {
			logger.Logger.Debug().Str("device", device).Msg("Successfully unlocked")
		}
		break
	}

	// If all retries failed, return the error
	if unlockErr != nil {
		return fmt.Errorf("LUKS unlock failed after %d attempts: %w", maxRetries, unlockErr)
	}

	// Trigger and settle - we just created the mapper device, udev needs to process it
	if err := UdevAdmTriggerSettle(logger, 30*time.Second); err != nil {
		if logger != nil {
			logger.Logger.Warn().Err(err).Str("device", device).Msg("UdevAdmTriggerSettle failed")
		}
	}

	// Additional wait for dmsetup to register the device
	// Sometimes the filesystem node exists but dmsetup hasn't registered it yet
	for i := 0; i < 5; i++ {
		dmOutput, dmErr := utils.SH("dmsetup ls --target crypt")
		if dmErr == nil && strings.Contains(dmOutput, mapper) {
			break
		}
		time.Sleep(1 * time.Second)
	}

	// Verify the mapper was created
	if _, err := os.Stat(mapperPath); err != nil {
		return fmt.Errorf("mapper device %s not created after unlock: %w", mapperPath, err)
	}

	return nil
}

// findEncryptedPartitions scans the system for LUKS encrypted partitions
// and returns their filesystem labels. It uses the kairos-sdk ghw wrapper
// to scan for partitions and checks if they are LUKS encrypted by examining
// the filesystem type.
func findEncryptedPartitions(logger sdkLogger.KairosLogger) ([]string, error) {
	logger.Logger.Debug().Msg("Scanning for encrypted partitions")

	disks := ghw.GetDisks(ghw.NewPaths(""), &logger)
	if disks == nil {
		logger.Logger.Warn().Msg("Failed to scan block devices")
		return nil, fmt.Errorf("failed to scan block devices")
	}

	var partitionLabels []string
	for _, disk := range disks {
		for _, p := range disk.Partitions {
			// Check if partition is LUKS encrypted by examining the filesystem type
			// LUKS partitions have FS type "crypto_LUKS"
			if p.FS == "crypto_LUKS" {
				// Check if device is already unlocked
				// We mount it under /dev/mapper/DEVICE, so it's pretty easy to check
				mapperPath := filepath.Join("/dev", "mapper", p.Name)
				if !utils.Exists(mapperPath) {
					logger.Logger.Info().
						Str("device", p.Path).
						Str("label", p.FilesystemLabel).
						Msg("Found unmounted LUKS partition")
					if label := encryptedPartitionLabel(p); label != "" {
						partitionLabels = append(partitionLabels, label)
					}
				} else {
					logger.Logger.Info().
						Str("device", p.Path).
						Str("mapper", mapperPath).
						Msg("Device already unlocked, skipping")
				}
			}
		}
	}

	return partitionLabels, nil
}

// UnlockAllEncryptedPartitions finds all encrypted partitions and unlocks them
// using the appropriate encryptor based on system configuration.
// This is the unified logic for unlocking encrypted partitions that works for
// both UKI and non-UKI modes by using GetEncryptor() which automatically
// detects the appropriate encryption method.
func UnlockAllEncryptedPartitions(logger sdkLogger.KairosLogger) error {
	logger.Logger.Debug().Msg("Getting encryptor for unlocking")

	// Scan for all LUKS partitions and unlock them
	partitions, err := findEncryptedPartitions(logger)
	if err != nil {
		logger.Logger.Err(err).Msg("Failed to find encrypted partitions")
		return err
	}

	if len(partitions) == 0 {
		logger.Logger.Debug().Msg("No encrypted partitions found")
		return nil
	}

	// Get the appropriate encryptor based on system configuration
	// This automatically detects UKI mode, kcrypt config, etc.
	encryptor, err := GetEncryptor(logger)
	if err != nil {
		logger.Logger.Err(err).Msg("Failed to get encryptor")
		return err
	}

	logger.Logger.Info().Str("method", encryptor.Name()).Msg("Using encryption method for unlock")

	logger.Logger.Info().Strs("partitions", partitions).Msg("Found encrypted partitions to unlock")

	// Unlock all encrypted partitions using the encryptor
	// The encryptor knows how to unlock based on its type (TPM+PCR, Remote KMS, Local TPM)
	err = encryptor.Unlock(partitions)
	if err != nil {
		logger.Logger.Err(err).Msg("Failed to unlock partitions")
		return err
	}

	logger.Logger.Info().Msg("Successfully unlocked all encrypted partitions")
	return nil
}
