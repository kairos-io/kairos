package action

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/kairos-io/kairos-sdk/kcrypt"
	sdkConfig "github.com/kairos-io/kairos-sdk/types/config"
	"github.com/kairos-io/tpm-helpers"
)

// resolveNVIndexAndDevice resolves the NV index and TPM device from flags, config, or defaults.
// Priority: explicit flag > config > default
func resolveNVIndexAndDevice(cfg *sdkConfig.Config, nvIndex, tpmDevice string) (targetIndex, targetTPMDevice string) {
	// Determine NV index
	if nvIndex != "" {
		targetIndex = nvIndex
	} else {
		// Get kcrypt config from the embedded collector.Config
		var ok bool
		var kcryptConfig map[string]interface{}
		if kcryptConfig, ok = cfg.Collector.Values["kcrypt"].(map[string]interface{}); ok {
			targetIndex, _ = kcryptConfig["nv_index"].(string)
		}
	}
	if targetIndex == "" { // If still empty, use default
		targetIndex = kcrypt.DefaultLocalPassphraseNVIndex
	}

	// Determine TPM device
	if tpmDevice != "" {
		targetTPMDevice = tpmDevice
	} else {
		// Get kcrypt config from the embedded collector.Config
		var ok bool
		var kcryptConfig map[string]interface{}
		if kcryptConfig, ok = cfg.Collector.Values["kcrypt"].(map[string]interface{}); ok {
			targetTPMDevice, _ = kcryptConfig["tpm_device"].(string)
		}
	}

	return targetIndex, targetTPMDevice
}

// resolveCIndex resolves the C index (certificate index) from flags, config, or defaults.
// Priority: explicit flag > config
func resolveCIndex(cfg *sdkConfig.Config, cIndex string) string {
	// If explicitly provided via flag, use it
	if cIndex != "" {
		return cIndex
	}

	// Otherwise, try to get from config
	var kcryptConfig map[string]interface{}
	var ok bool
	if kcryptConfig, ok = cfg.Collector.Values["kcrypt"].(map[string]interface{}); !ok {
		return ""
	}

	var challengerConfig map[string]interface{}
	if challengerConfig, ok = kcryptConfig["challenger"].(map[string]interface{}); !ok {
		return ""
	}

	configCIndex, _ := challengerConfig["c_index"].(string)
	return configCIndex
}

// KcryptReadNV reads and decrypts the value from a TPM NV index.
// If the value is encrypted (as is the case for local passphrases), it will be decrypted
// using the C index from flags or config before outputting.
func KcryptReadNV(cfg *sdkConfig.Config, nvIndex, tpmDevice, cIndex string) error {
	logger := cfg.Logger

	targetIndex, targetTPMDevice := resolveNVIndexAndDevice(cfg, nvIndex, tpmDevice)
	resolvedCIndex := resolveCIndex(cfg, cIndex)

	logger.Debugf("Reading TPM NV index: %s", targetIndex)
	if targetTPMDevice != "" {
		logger.Debugf("Using TPM device: %s", targetTPMDevice)
	}
	if resolvedCIndex != "" {
		logger.Debugf("Using C index for decryption: %s", resolvedCIndex)
	} else {
		logger.Debugf("No C index found in config or flags - will output raw value")
	}

	// Build TPM options for reading
	readOpts := []tpm.TPMOption{tpm.WithIndex(targetIndex)}
	if targetTPMDevice != "" {
		readOpts = append(readOpts, tpm.WithDevice(targetTPMDevice))
	}

	// Read the encrypted blob from the NV index
	logger.Debugf("Reading NV index %s", targetIndex)
	encryptedBlob, err := tpm.ReadBlob(readOpts...)
	if err != nil {
		return fmt.Errorf("failed to read NV index %s: %w", targetIndex, err)
	}

	// Try to decrypt the blob
	// If C index is provided, use it; otherwise try without (matching how encryption was done)
	decryptOpts := []tpm.TPMOption{}
	if resolvedCIndex != "" {
		decryptOpts = append(decryptOpts, tpm.WithIndex(resolvedCIndex))
		logger.Debugf("Attempting to decrypt blob using C index %s", resolvedCIndex)
	} else {
		logger.Debugf("No C index specified - attempting to decrypt without C index (matching encryption behavior)")
	}
	if targetTPMDevice != "" {
		decryptOpts = append(decryptOpts, tpm.WithDevice(targetTPMDevice))
	}

	decryptedValue, err := tpm.DecryptBlob(encryptedBlob, decryptOpts...)
	if err != nil {
		// If decryption fails, it might be that:
		// 1. The data is not encrypted (raw data)
		// 2. Wrong C index was used
		// 3. The encryption used a different method
		if resolvedCIndex != "" {
			logger.Logger.Warn().Err(err).Msgf("Failed to decrypt NV index %s using C index %s", targetIndex, resolvedCIndex)
			fmt.Fprintf(os.Stderr, "Warning: Decryption failed with C index %s. The data may not be encrypted, or a different C index may be needed.\n", resolvedCIndex)
			fmt.Fprintf(os.Stderr, "Outputting raw value:\n")
		} else {
			logger.Logger.Warn().Err(err).Msgf("Failed to decrypt NV index %s without C index", targetIndex)
			fmt.Fprintf(os.Stderr, "Warning: Decryption failed. The data may not be encrypted, or a C index may be required.\n")
			fmt.Fprintf(os.Stderr, "Try using --c-index flag if the data was encrypted with a C index.\n")
			fmt.Fprintf(os.Stderr, "Outputting raw value:\n")
		}
		fmt.Print(string(encryptedBlob))
		return nil
	}

	// Successfully decrypted - output the passphrase
	fmt.Print(string(decryptedValue))
	return nil
}

// KcryptCheckNV checks if data exists in a TPM NV index.
// Returns an error if the index doesn't exist or is empty.
func KcryptCheckNV(cfg *sdkConfig.Config, nvIndex, tpmDevice string) error {
	logger := cfg.Logger

	targetIndex, targetTPMDevice := resolveNVIndexAndDevice(cfg, nvIndex, tpmDevice)

	logger.Debugf("Checking TPM NV index: %s", targetIndex)
	if targetTPMDevice != "" {
		logger.Debugf("Using TPM device: %s", targetTPMDevice)
	}

	// Build TPM options
	opts := []tpm.TPMOption{tpm.WithIndex(targetIndex)}
	if targetTPMDevice != "" {
		opts = append(opts, tpm.WithDevice(targetTPMDevice))
	}

	// Try to read from the index to see if it exists
	logger.Debugf("Checking if NV index %s exists", targetIndex)
	_, err := tpm.ReadBlob(opts...)
	if err != nil {
		return fmt.Errorf("NV index %s does not exist or is empty: %w", targetIndex, err)
	}

	fmt.Printf("NV index %s contains data\n", targetIndex)
	return nil
}

// KcryptCleanup cleans up TPM NV memory by undefining specific NV indices.
// This is used to clean up legacy local passphrase storage (now handled by kairos-sdk)
// or any other TPM NV indices that need to be removed.
func KcryptCleanup(cfg *sdkConfig.Config, nvIndex, tpmDevice string, skipConfirmation bool) error {
	logger := cfg.Logger

	targetIndex, targetTPMDevice := resolveNVIndexAndDevice(cfg, nvIndex, tpmDevice)

	logger.Debugf("Cleaning up TPM NV index: %s", targetIndex)
	if targetTPMDevice != "" {
		logger.Debugf("Using TPM device: %s", targetTPMDevice)
	}

	// Check if the NV index exists first
	opts := []tpm.TPMOption{tpm.WithIndex(targetIndex)}
	if targetTPMDevice != "" {
		opts = append(opts, tpm.WithDevice(targetTPMDevice))
	}

	// Try to read from the index to see if it exists
	logger.Debugf("Checking if NV index %s exists", targetIndex)
	_, err := tpm.ReadBlob(opts...)
	if err != nil {
		// If we can't read it, it might not exist or be empty
		logger.Debugf("NV index %s appears to be empty or non-existent: %v", targetIndex, err)
		fmt.Printf("NV index %s appears to be empty or does not exist\n", targetIndex)
		return nil
	}

	// Confirmation prompt with warning
	if !skipConfirmation {
		fmt.Printf("\n⚠️  WARNING: You are about to delete TPM NV index %s\n", targetIndex)
		fmt.Printf("⚠️  If this index contains your disk encryption passphrase, your encrypted disk will become UNBOOTABLE!\n")
		fmt.Printf("⚠️  This action CANNOT be undone.\n\n")
		fmt.Printf("Are you sure you want to continue? (type 'yes' to confirm): ")

		scanner := bufio.NewScanner(os.Stdin)
		scanner.Scan()
		response := strings.TrimSpace(strings.ToLower(scanner.Text()))

		if response != "yes" {
			fmt.Printf("Cleanup cancelled.\n")
			return nil
		}
	}

	// Use native Go TPM library to undefine the NV space
	logger.Debugf("Using native TPM library to undefine NV index")
	fmt.Printf("Cleaning up TPM NV index %s...\n", targetIndex)

	err = tpm.UndefineBlob(opts...)
	if err != nil {
		return fmt.Errorf("failed to undefine NV index %s: %w", targetIndex, err)
	}

	fmt.Printf("Successfully cleaned up NV index %s\n", targetIndex)
	logger.Debugf("Successfully undefined NV index %s", targetIndex)
	return nil
}
