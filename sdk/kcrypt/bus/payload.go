package bus

import (
	"github.com/kairos-io/kairos-sdk/types/partitions"
)

// KcryptConfig represents the kcrypt configuration from Kairos config merged with cmdline options.
// This is the general configuration struct used by local encryptors and throughout the SDK.
// It does NOT include partition information as it represents configuration, not runtime state.
type KcryptConfig struct {
	// Kcrypt challenger configuration (for remote KMS)
	ChallengerServer string `json:"challenger_server,omitempty"`
	MDNS             bool   `json:"mdns,omitempty"`
	// TPM configuration (for local TPM-based encryption)
	Certificate string `json:"certificate,omitempty"`
	NVIndex     string `json:"nv_index,omitempty"`
	CIndex      string `json:"c_index,omitempty"`
	TPMDevice   string `json:"tpm_device,omitempty"`
}

// DiscoveryPasswordPayload is the data sent to kcrypt-challenger plugin.
// It contains only the minimal information needed for remote KMS communication:
// - ChallengerServer: the remote server address
// - MDNS: whether to use mDNS discovery
// - Partition: the partition to unlock (runtime state)
//
// This struct is constructed ONLY by:
// 1. The remote KMS Encryptor implementation when calling the kcrypt-challenger
// 2. The kcrypt-challenger when unmarshalling received data through stdin.
type DiscoveryPasswordPayload struct {
	Partition        *partitions.Partition `json:"partition"`
	ChallengerServer string                `json:"challenger_server,omitempty"`
	MDNS             bool                  `json:"mdns,omitempty"`
}
