package kcrypt

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/anatol/luks.go"
	"github.com/kairos-io/kairos/v4/sdk/collector"
	"github.com/kairos-io/kairos/v4/sdk/constants"
	"github.com/kairos-io/kairos/v4/sdk/ghw"
	"github.com/kairos-io/kairos/v4/sdk/retry"
	sdkLogger "github.com/kairos-io/kairos/v4/sdk/types/logger"
	"github.com/kairos-io/kairos/v4/sdk/types/partitions"
	"github.com/kairos-io/kairos/v4/sdk/utils"
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
	maxRetries := 3

	defer func() {
		if dev != nil {
			_ = dev.Close() // might be already closed. Just to be sure.
		}
	}()

	attempt := 0
	unlockErr := retry.Do(func() error {
		if attempt > 0 {
			// Just settle, no trigger - we're waiting for pending events before retrying
			if err := UdevAdmSettle(logger, 10*time.Second); err != nil {
				if logger != nil {
					logger.Logger.Warn().
						Int("attempt", attempt+1).
						Err(err).Msg("Failed to settle")
				}
			}
		}
		attempt++

		var err error
		dev, err = luks.Open(device)
		if err != nil {
			if logger != nil {
				logger.Logger.Warn().
					Int("attempt", attempt).
					Int("max_retries", maxRetries).
					Str("device", device).
					Err(err).
					Msg("Failed to open device")
			}
			return err
		}

		// Try to unlock
		err = dev.Unlock(0, []byte(password), mapper)
		if err != nil {
			_ = dev.Close() // Close on error so that the next retry opens it again
			if logger != nil {
				logger.Logger.Warn().
					Int("attempt", attempt).
					Int("max_retries", maxRetries).
					Str("device", device).
					Err(err).
					Msg("Failed to unlock device")
			}
			return err
		}

		// Success! Close the device handle immediately to release the file descriptor
		_ = dev.Close()
		if logger != nil {
			logger.Logger.Debug().Str("device", device).Msg("Successfully unlocked")
		}
		return nil
	}, retry.Config{Attempts: uint(maxRetries), Delay: retry.Linear(1*time.Second, 0)})

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
			if p.FS == constants.LUKSFs {
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
// both UKI and non-UKI modes by using GetEncryptorFromConfig() which
// automatically detects the appropriate encryption method.
//
// When that fails (no TPM, a cleared TPM, an unreachable key server) and the
// configuration sets kcrypt.passphrase_fallback, it asks for a passphrase on
// the console for every partition that is still locked before it gives up.
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

	logger.Logger.Info().Strs("partitions", partitions).Msg("Found encrypted partitions to unlock")

	collectorConfig, scanErr := scanUnlockConfig(logger)
	if scanErr != nil {
		// Same as GetEncryptor: a failed scan falls back to the no-config
		// default rather than failing the unlock.
		logger.Debugf("UnlockAllEncryptedPartitions: %v", scanErr)
		collectorConfig = nil
	}

	err = unlockWithEncryptor(logger, collectorConfig, partitions)
	if err == nil {
		logger.Logger.Info().Msg("Successfully unlocked all encrypted partitions")
		return nil
	}

	timeout := passphraseFallbackTimeout(KcryptConfigFromCollector(collectorConfig, logger))
	if timeout == 0 {
		return err
	}

	logger.Logger.Warn().Err(err).Msg("Automatic unlock failed, asking for a passphrase")
	for _, label := range partitions {
		if promptErr := unlockByPrompt(label, timeout, logger); promptErr != nil {
			logger.Logger.Err(promptErr).Str("partition", label).Msg("Passphrase unlock failed")
			return fmt.Errorf("%w; passphrase fallback: %w", err, promptErr)
		}
	}

	logger.Logger.Info().Msg("Unlocked all encrypted partitions with a passphrase")
	return nil
}

// unlockWithEncryptor picks the encryptor for the configuration and unlocks
// the partitions with it.
func unlockWithEncryptor(logger sdkLogger.KairosLogger, collectorConfig *collector.Config, partitions []string) error {
	encryptor, err := getEncryptorFromConfig(logger, collectorConfig)
	if err != nil {
		logger.Logger.Err(err).Msg("Failed to get encryptor")
		return err
	}

	logger.Logger.Info().Str("method", encryptor.Name()).Msg("Using encryption method for unlock")

	// The encryptor knows how to unlock based on its type (TPM+PCR, Remote KMS, Local TPM, Passphrase)
	if err := encryptor.Unlock(partitions); err != nil {
		logger.Logger.Err(err).Msg("Failed to unlock partitions")
		return err
	}
	return nil
}

// getEncryptorFromConfig and scanUnlockConfig are seams for the tests.
var (
	getEncryptorFromConfig = GetEncryptorFromConfig
	scanUnlockConfig       = func(logger sdkLogger.KairosLogger) (*collector.Config, error) {
		return ScanCollectorConfig(logger)
	}
)
