package client

import (
	"github.com/kairos-io/kairos-sdk/kcrypt"
	loggerpkg "github.com/kairos-io/kairos-sdk/types/logger"
)

type Client struct {
	Config Config
	Logger loggerpkg.KairosLogger
}

type Config struct {
	Kcrypt struct {
		Challenger struct {
			MDNS        bool   `yaml:"mdns,omitempty"`
			Server      string `yaml:"challenger_server,omitempty"`
			Certificate string `yaml:"certificate,omitempty"`
		}
		TPMDevice string `yaml:"tpm_device,omitempty"`
	}
}

// newEmptyConfig returns an empty config
// The actual config is now passed via the DiscoveryPasswordPayload JSON from the caller
func newEmptyConfig() Config {
	return Config{}
}

// LoadConfigFromCollector loads configuration from kairos-sdk collector.
// This scans the standard Kairos config directories and extracts kcrypt configuration.
func LoadConfigFromCollector(logger loggerpkg.KairosLogger) Config {
	conf := newEmptyConfig()

	kcryptConfig := kcrypt.ScanKcryptConfig(logger)
	if kcryptConfig == nil {
		logger.Debugf("No kcrypt config found via collector")
		return conf
	}

	// Populate config from collector
	if kcryptConfig.TPMDevice != "" {
		conf.Kcrypt.TPMDevice = kcryptConfig.TPMDevice
	}
	if kcryptConfig.ChallengerServer != "" {
		conf.Kcrypt.Challenger.Server = kcryptConfig.ChallengerServer
	}
	// MDNS is a bool, so check if it's true (explicitly set to true in config)
	if kcryptConfig.MDNS {
		conf.Kcrypt.Challenger.MDNS = kcryptConfig.MDNS
		logger.Debugf("Loaded MDNS from collector: %t", conf.Kcrypt.Challenger.MDNS)
	} else {
		logger.Debugf("MDNS not set in collector config (value: %t), defaulting to false", kcryptConfig.MDNS)
		conf.Kcrypt.Challenger.MDNS = false
	}
	if kcryptConfig.Certificate != "" {
		conf.Kcrypt.Challenger.Certificate = kcryptConfig.Certificate
	}

	logger.Debugf("Loaded config from collector: TPMDevice=%s, Server=%s, MDNS=%t",
		conf.Kcrypt.TPMDevice, conf.Kcrypt.Challenger.Server, conf.Kcrypt.Challenger.MDNS)

	return conf
}
