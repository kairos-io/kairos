package config

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/joho/godotenv"
	"github.com/kairos-io/kairos/v4/internal/version"
	"github.com/kairos-io/kairos/v4/sdk/collector"
	"github.com/kairos-io/kairos/v4/sdk/schema"
	"github.com/kairos-io/kairos/v4/sdk/state"
	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
	"github.com/kairos-io/kairos/v4/sdk/types/logger"
	"github.com/sanity-io/litter"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

// FilterKeys is used to pass to any other pkg which might want to see which part of the config matches the Kairos config.
func FilterKeys(d []byte) ([]byte, error) {
	cmdLineFilter := sdkConfig.Config{}
	err := yaml.Unmarshal(d, &cmdLineFilter)
	if err != nil {
		return []byte{}, err
	}

	out, err := yaml.Marshal(cmdLineFilter)
	if err != nil {
		return []byte{}, err
	}

	return out, nil
}

// ScanNoLogs is a wrapper around Scan that sets the logger to null
// Also sets the NoLogs option to true by default
func ScanNoLogs(opts ...collector.Option) (c *sdkConfig.Config, err error) {
	log := logger.NewNullLogger()
	result := NewConfig(WithLogger(log))
	if result == nil {
		return nil, fmt.Errorf("failed to initialize config")
	}
	return scan(result, append(opts, collector.NoLogs)...)
}

// Scan is a wrapper around collector.Scan that sets the logger to the default Kairos logger
func Scan(opts ...collector.Option) (c *sdkConfig.Config, err error) {
	result := NewConfig()
	if result == nil {
		return nil, fmt.Errorf("failed to initialize config")
	}
	return scan(result, opts...)
}

// scan is the internal function that does the actual scanning of the configs
func scan(result *sdkConfig.Config, opts ...collector.Option) (c *sdkConfig.Config, err error) {
	// Init new config with some default options
	o := &collector.Options{}
	if err := o.Apply(opts...); err != nil {
		return result, err
	}

	genericConfig, err := collector.Scan(o, FilterKeys)
	if err != nil {
		return result, err
	}

	result.Collector = *genericConfig
	if err := validateRegistryAuthKeys(result.Collector.Values); err != nil {
		return result, err
	}
	configStr, err := genericConfig.String()
	if err != nil {
		return result, err
	}

	err = yaml.Unmarshal([]byte(configStr), result)
	if err != nil {
		return result, err
	}

	kc, err := schema.NewConfigFromYAML(configStr, schema.RootSchema{})
	if err != nil {
		if !o.NoLogs && !o.StrictValidation {
			fmt.Printf("WARNING: %s\n", err.Error())
		}

		if o.StrictValidation {
			return result, fmt.Errorf("ERROR: %s", err.Error())
		}
	}

	if kc != nil && !kc.IsValid() {
		if !o.NoLogs && !o.StrictValidation {
			fmt.Printf("WARNING: %s\n", kc.ValidationError.Error())
		}

		if o.StrictValidation {
			return result, fmt.Errorf("ERROR: %s", kc.ValidationError.Error())
		}
	}

	if kc != nil {
		warnings, err := kc.ValidateSemantics()
		if err != nil {
			// Semantic errors are always fatal (they describe configs
			// that would leave the installed system unusable) regardless
			// of --strict-validation.
			return result, fmt.Errorf("ERROR: %s", err.Error())
		}
		if !o.NoLogs {
			for _, w := range warnings {
				fmt.Printf("WARNING: %s\n", w)
			}
		}
	}

	// If we got debug enabled via cloud config, set it on viper so its available everywhere
	if result.Debug {
		viper.Set("debug", true)
	}
	// Config the logger
	if viper.GetBool("debug") {
		result.Logger.SetLevel("debug")
	}

	result.Logger.Logger.Info().Str("version", version.Version).Msg("Kairos Agent")
	result.Logger.Logger.Debug().Str("version", version.Version).Str("go", runtime.Version()).Msg("Kairos Agent")

	// Try to load the kairos version from the kairos-release file
	// Best effort, if it fails, we just ignore it
	f, err := result.Fs.Open("/etc/os-release")
	defer func() { _ = f.Close() }()
	osRelease, err := godotenv.Parse(f)
	if err == nil {
		v := osRelease["KAIROS_VERSION"]
		if v != "" {
			result.Logger.Logger.Info().Str("version", v).Msg("Kairos System")
		} else {
			// Fallback into os-release
			f, err = result.Fs.Open("/etc/os-release")
			defer func() { _ = f.Close() }()
			osRelease, err = godotenv.Parse(f)
			if err == nil {
				v = osRelease["KAIROS_VERSION"]
				if v != "" {
					result.Logger.Logger.Info().Str("version", v).Msg("Kairos System")
				}
			}
		}
	}

	// Log the boot mode
	r, err := state.NewRuntimeWithLogger(result.Logger.Logger)
	if err == nil {
		result.Logger.Logger.Info().Str("boot_mode", string(r.BootState)).Msg("Boot Mode")
	}

	// Detect if we are running on a UKI boot to also log it
	cmdline, err := result.Fs.ReadFile("/proc/cmdline")
	if err == nil {
		result.Logger.Logger.Info().Bool("result", state.DetectUKIboot(string(cmdline))).Msg("Boot in uki mode")
	}

	litter.Config.HideZeroValues = true
	result.Logger.Debugf("Loaded config: %s", RedactedConfigDump(result))

	return result, nil
}

// RedactedConfigDump returns a redacted snapshot of configuration data only.
// Runtime services and providers are excluded from the diagnostic output.
func RedactedConfigDump(result *sdkConfig.Config) string {
	if err := validateRegistryAuthKeys(result.Collector.Values); err != nil {
		return err.Error()
	}
	data, err := yaml.Marshal(map[string]interface{}{
		"config":    result,
		"collector": result.Collector.Values,
	})
	if err != nil {
		return "configuration diagnostic unavailable"
	}
	var snapshot map[string]interface{}
	if err := yaml.Unmarshal(data, &snapshot); err != nil {
		return "configuration diagnostic unavailable"
	}
	return litter.Sdump(redactConfigValues(snapshot))
}

func isSensitiveConfigKey(key string) bool {
	key = strings.ToLower(key)
	if key == "ssh_authorized_keys" {
		return false
	}
	for _, part := range []string{"auth", "passwd", "password", "token", "secret"} {
		if strings.Contains(key, part) {
			return true
		}
	}
	return false
}

func redactConfigValues(value interface{}) interface{} {
	switch typed := value.(type) {
	case collector.ConfigValues:
		out := collector.ConfigValues{}
		for key, child := range typed {
			if isSensitiveConfigKey(key) {
				out[key] = "[REDACTED]"
			} else {
				out[key] = redactConfigValues(child)
			}
		}
		return out
	case map[string]interface{}:
		out := map[string]interface{}{}
		for key, child := range typed {
			if isSensitiveConfigKey(key) {
				out[key] = "[REDACTED]"
			} else {
				out[key] = redactConfigValues(child)
			}
		}
		return out
	case map[interface{}]interface{}:
		out := map[interface{}]interface{}{}
		for key, child := range typed {
			if text, ok := key.(string); ok && isSensitiveConfigKey(text) {
				out[key] = "[REDACTED]"
			} else {
				out[key] = redactConfigValues(child)
			}
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(typed))
		for i, child := range typed {
			out[i] = redactConfigValues(child)
		}
		return out
	default:
		return value
	}
}
