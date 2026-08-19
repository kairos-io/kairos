package kcrypt

import (
	"github.com/kairos-io/kairos-sdk/collector"
	"github.com/kairos-io/kairos-sdk/kcrypt/bus"
	sdkLogger "github.com/kairos-io/kairos-sdk/types/logger"
)

// DefaultConfigDirs are the default directories to scan for Kairos configuration.
var DefaultConfigDirs = []string{"/oem", "/sysroot/oem", "/run/cos/oem"}

// ScanKcryptConfig scans for Kairos configuration in the given directories (or defaults),
// merges with cmdline, and extracts the kcrypt configuration.
// Returns nil if no kcrypt config is found.
func ScanKcryptConfig(logger sdkLogger.KairosLogger, dirs ...string) *bus.KcryptConfig {
	if len(dirs) == 0 {
		dirs = DefaultConfigDirs
	}

	logger.Debugf("ScanKcryptConfig: scanning directories: %v", dirs)

	o := &collector.Options{NoLogs: true, MergeBootCMDLine: true}
	if err := o.Apply(collector.Directories(dirs...)); err != nil {
		logger.Debugf("ScanKcryptConfig: error applying collector options: %v", err)
		return nil
	}

	collectorConfig, err := collector.Scan(o, func(d []byte) ([]byte, error) {
		return d, nil
	})
	if err != nil {
		logger.Debugf("ScanKcryptConfig: error scanning for config: %v", err)
		return nil
	}

	if collectorConfig == nil {
		logger.Debugf("ScanKcryptConfig: collector returned nil config")
		return nil
	}

	logger.Debugf("ScanKcryptConfig: collector found config with %d keys", len(collectorConfig.Values))
	if len(collectorConfig.Values) > 0 {
		// Log the top-level keys
		keys := make([]string, 0, len(collectorConfig.Values))
		for k := range collectorConfig.Values {
			keys = append(keys, k)
		}
		logger.Debugf("ScanKcryptConfig: top-level keys: %v", keys)
	}
	logger.Debugf("ScanKcryptConfig: struct is: %#v", collectorConfig.Values)

	result := extractKcryptConfigFromCollector(*collectorConfig, logger)
	logger.Debugf("ScanKcryptConfig: extracted kcrypt config =%s", result)

	return result
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
