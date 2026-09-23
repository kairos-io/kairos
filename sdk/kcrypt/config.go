package kcrypt

import (
	"fmt"
	"strings"

	"github.com/kairos-io/kairos/v4/sdk/collector"
	"github.com/kairos-io/kairos/v4/sdk/kcrypt/bus"
	sdkLogger "github.com/kairos-io/kairos/v4/sdk/types/logger"
)

// DefaultConfigDirs are the default directories to scan for Kairos configuration.
var DefaultConfigDirs = []string{"/oem", "/sysroot/oem", "/run/cos/oem"}

// ScanCollectorConfig scans the Kairos configuration in the given directories
// (or DefaultConfigDirs), merged with the kernel cmdline, and returns the raw
// collector config. Every kcrypt config consumer (ScanKcryptConfig,
// ScanEncryptOnBootPolicy, GetEncryptor) goes through this one scan so their
// view of the options cannot drift apart. A nil config with a nil error means
// no configuration exists; an error means the scan itself failed and the
// caller cannot know what the configuration says.
func ScanCollectorConfig(logger sdkLogger.KairosLogger, dirs ...string) (*collector.Config, error) {
	if len(dirs) == 0 {
		dirs = DefaultConfigDirs
	}

	logger.Debugf("ScanCollectorConfig: scanning directories: %v", dirs)

	o := &collector.Options{NoLogs: true, MergeBootCMDLine: true}
	if err := o.Apply(collector.Directories(dirs...)); err != nil {
		return nil, fmt.Errorf("applying collector options: %w", err)
	}

	collectorConfig, err := collector.Scan(o, func(d []byte) ([]byte, error) {
		return d, nil
	})
	if err != nil {
		return nil, fmt.Errorf("scanning configuration: %w", err)
	}

	return collectorConfig, nil
}

// ScanKcryptConfig scans for Kairos configuration in the given directories (or defaults),
// merges with cmdline, and extracts the kcrypt configuration.
// Returns nil if no kcrypt config is found.
func ScanKcryptConfig(logger sdkLogger.KairosLogger, dirs ...string) *bus.KcryptConfig {
	collectorConfig, err := ScanCollectorConfig(logger, dirs...)
	if err != nil {
		logger.Debugf("ScanKcryptConfig: %v", err)
		return nil
	}

	if collectorConfig == nil {
		logger.Debugf("ScanKcryptConfig: collector returned nil config")
		return nil
	}

	logger.Debugf("ScanKcryptConfig: collector found config with %d keys", len(collectorConfig.Values))

	result := extractKcryptConfigFromCollector(*collectorConfig, logger)
	logger.Debugf("ScanKcryptConfig: extracted kcrypt config =%s", result)

	return result
}

// EncryptOnBootPolicy is the boot time encryption policy read on every boot:
// whether partitions listed in install.encrypted_partitions that are still
// plaintext should be encrypted before they are mounted
// (kcrypt.encrypt_on_boot, default false). Both keys are non secret policy,
// so a golden image or template can carry them while the LUKS material is
// created on the first boot of each clone, against that clone's own TPM.
// See kairos-io/kairos#4556.
type EncryptOnBootPolicy struct {
	Enabled    bool
	Partitions []string
}

// ScanEncryptOnBootPolicy scans the Kairos configuration in the given
// directories (or DefaultConfigDirs), merged with the kernel cmdline, and
// extracts the boot time encryption policy. It returns an error when the
// configuration could not be read at all: the caller cannot tell an absent
// opt-in from an unreadable one, so a failed scan must never pass for a
// disabled policy.
func ScanEncryptOnBootPolicy(logger sdkLogger.KairosLogger, dirs ...string) (EncryptOnBootPolicy, error) {
	collectorConfig, err := ScanCollectorConfig(logger, dirs...)
	if err != nil {
		return EncryptOnBootPolicy{}, err
	}

	return EncryptOnBootPolicyFromConfig(collectorConfig, logger), nil
}

// EncryptOnBootPolicyFromConfig extracts the boot time encryption policy from
// an already-scanned collector config, so a caller that needs both the policy
// and the rest of the configuration (immucore's encrypt-pending step, which
// also hands the config to GetEncryptorFromConfig) scans once. A nil config
// means no configuration exists: a disabled policy.
func EncryptOnBootPolicyFromConfig(collectorConfig *collector.Config, log sdkLogger.KairosLogger) EncryptOnBootPolicy {
	if collectorConfig == nil {
		return EncryptOnBootPolicy{}
	}

	return extractEncryptOnBootPolicy(*collectorConfig, log)
}

// extractEncryptOnBootPolicy extracts the boot time encryption policy from a
// collector.Config. The flag lives under kcrypt so it sits next to the rest
// of the encryption configuration; the partition list is the same
// install.encrypted_partitions the install time hook consumes.
func extractEncryptOnBootPolicy(collectorConfig collector.Config, log sdkLogger.KairosLogger) EncryptOnBootPolicy {
	policy := EncryptOnBootPolicy{}

	if collectorConfig.Values == nil {
		log.Debugf("extractEncryptOnBootPolicy: no values found")
		return policy
	}

	if kcryptMap, ok := asConfigValues(collectorConfig.Values["kcrypt"]); ok {
		policy.Enabled = enabledFlag(kcryptMap["encrypt_on_boot"])
	}

	if installMap, ok := asConfigValues(collectorConfig.Values["install"]); ok {
		switch v := installMap["encrypted_partitions"].(type) {
		case []string:
			policy.Partitions = v
		case []interface{}:
			for _, item := range v {
				if str, ok := item.(string); ok {
					policy.Partitions = append(policy.Partitions, str)
				}
			}
		case string:
			// The cmdline route delivers the list as one comma separated
			// string (install.encrypted_partitions=COS_PERSISTENT,MYAPP_DATA).
			for _, item := range strings.Split(v, ",") {
				if item = strings.TrimSpace(item); item != "" {
					policy.Partitions = append(policy.Partitions, item)
				}
			}
		}
	}

	log.Debugf("extractEncryptOnBootPolicy: enabled=%t partitions=%v", policy.Enabled, policy.Partitions)
	return policy
}

// enabledFlag interprets the encrypt_on_boot value across the shapes the
// collector can deliver it in: a YAML bool, the numbers a YAML `1` parses to,
// and the strings a kernel cmdline carries. A bare kcrypt.encrypt_on_boot
// token reaches the collector as the string "true", and spellings like
// kcrypt.encrypt_on_boot=1 or =yes arrive verbatim, so the common truthy
// forms are accepted rather than the single literal "true". Everything else,
// "false", "no" and "0" included, is disabled.
func enabledFlag(v interface{}) bool {
	switch val := v.(type) {
	case bool:
		return val
	case int:
		return val == 1
	case int64:
		return val == 1
	case float64:
		return val == 1
	case string:
		switch strings.ToLower(strings.TrimSpace(val)) {
		case "true", "1", "yes", "y", "on":
			return true
		}
	}
	return false
}

// asConfigValues unwraps a nested collector value to its map form. The
// collector hands nested blocks back as collector.ConfigValues, but a value
// that went through a plain YAML unmarshal is a map[string]interface{}, so
// accept both.
func asConfigValues(v interface{}) (collector.ConfigValues, bool) {
	switch m := v.(type) {
	case collector.ConfigValues:
		return m, true
	case map[string]interface{}:
		return collector.ConfigValues(m), true
	}
	return nil, false
}

// extractKcryptConfigFromCollector extracts kcrypt configuration from a collector.Config.
func extractKcryptConfigFromCollector(collectorConfig collector.Config, log sdkLogger.KairosLogger) *bus.KcryptConfig {
	config := &bus.KcryptConfig{}

	if collectorConfig.Values == nil {
		log.Debugf("extractKcryptConfigFromCollector: no values found")
		return config
	}

	kcryptVal, hasKcrypt := collectorConfig.Values["kcrypt"]
	if !hasKcrypt {
		log.Debugf("extractKcryptConfigFromCollector: no kcrypt key found")
		return config
	}

	kcryptMap, ok := kcryptVal.(collector.ConfigValues)
	if !ok {
		log.Debugf("extractKcryptConfigFromCollector: kcrypt value is not ConfigValues, it's %T", kcryptVal)
		return config
	}

	// Extract from challenger block if present (for remote KMS)
	challengerVal := kcryptMap["challenger"]
	if challengerMap, ok := challengerVal.(collector.ConfigValues); ok {
		if server, ok := challengerMap["challenger_server"].(string); ok {
			config.ChallengerServer = server
		}
		if mdns, ok := challengerMap["mdns"].(bool); ok {
			config.MDNS = mdns
		}
		if cert, ok := challengerMap["certificate"].(string); ok {
			config.Certificate = cert
		}
	}

	// Extract TPM fields from top-level kcrypt block (for local encryption)
	if nvIndex, ok := kcryptMap["nv_index"].(string); ok {
		config.NVIndex = nvIndex
	}
	if cIndex, ok := kcryptMap["c_index"].(string); ok {
		config.CIndex = cIndex
	}
	if tpmDevice, ok := kcryptMap["tpm_device"].(string); ok {
		config.TPMDevice = tpmDevice
	}

	return config
}

// extractPCRBindingsFromCollector extracts bind-pcrs and bind-public-pcrs from collector config
// Returns the PCR bindings, with defaults if not found.
func extractPCRBindingsFromCollector(collectorConfig collector.Config, log sdkLogger.KairosLogger) (bindPCRs []string, bindPublicPCRs []string) {
	if collectorConfig.Values == nil {
		log.Debugf("ExtractPCRBindings: no config values")
		return nil, nil
	}

	if bindPCRsVal, ok := collectorConfig.Values["bind-pcrs"]; ok {
		log.Debugf("ExtractPCRBindings: found bind-pcrs, type=%T", bindPCRsVal)
		// Handle both []string and []interface{} (from YAML unmarshaling).
		switch v := bindPCRsVal.(type) {
		case []string:
			bindPCRs = v
		case []interface{}:
			for _, item := range v {
				if str, ok := item.(string); ok {
					bindPCRs = append(bindPCRs, str)
				}
			}
		}
		log.Debugf("ExtractPCRBindings: extracted bind-pcrs=%v", bindPCRs)
	}

	if bindPublicPCRsVal, ok := collectorConfig.Values["bind-public-pcrs"]; ok {
		log.Debugf("ExtractPCRBindings: found bind-public-pcrs, type=%T", bindPublicPCRsVal)
		// Handle both []string and []interface{} (from YAML unmarshaling).
		switch v := bindPublicPCRsVal.(type) {
		case []string:
			bindPublicPCRs = v
		case []interface{}:
			for _, item := range v {
				if str, ok := item.(string); ok {
					bindPublicPCRs = append(bindPublicPCRs, str)
				}
			}
		}
		log.Debugf("ExtractPCRBindings: extracted bind-public-pcrs=%v", bindPublicPCRs)
	}

	return bindPCRs, bindPublicPCRs
}
