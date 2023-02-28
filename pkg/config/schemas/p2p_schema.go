package config

import (
	jsonschemago "github.com/swaggest/jsonschema-go"
)

// P2PSchema represents the P2P block in the Kairos configuration. It is used to enables and configure the p2p full-mesh functionalities.
type P2PSchema struct {
	_          struct{} `title:"Kairos Schema: P2P block" description:"The p2p block enables the p2p full-mesh functionalities."`
	Role       string   `json:"role,omitempty" default:"none" enum:"[\"master\",\"worker\",\"none\"]" yaml:"role,omitempty"`
	NetworkID  string   `json:"network_id,omitempty" description:"User defined network-id. Can be used to have multiple clusters in the same network" yaml:"network_id,omitempty"`
	DNS        bool     `json:"dns,omitempty" description:"Enable embedded DNS See also: https://mudler.github.io/edgevpn/docs/concepts/overview/dns/" yaml:"dns,omitempty"`
	DisableDHT bool     `json:"disable_dht,omitempty" default:"true" description:"Disabling DHT makes co-ordination to discover nodes only in the local network" yaml:"disable_dht,omitempty"`
	P2PNetworkExtended
	VPN `json:"vpn,omitempty" yaml:"vpn,omitempty"`
}

// KubeVIPSchema represents the kubevip block in the Kairos configuration. It sets the Elastic IP used in KubeVIP. Only valid with p2p.
type KubeVIPSchema struct {
	_           struct{} `title:"Kairos Schema: KubeVIP block" description:"Sets the Elastic IP used in KubeVIP. Only valid with p2p"`
	EIP         string   `json:"eip,omitempty" example:"192.168.1.110" yaml:"eip,omitempty"`
	ManifestURL string   `json:"manifest_url,omitempty" description:"Specify a manifest URL for KubeVIP." default:"" yaml:"manifest_url,omitempty"`
	Enable      bool     `json:"enable,omitempty" description:"Enables KubeVIP" yaml:"enable,omitempty"`
	Interface   bool     `json:"interface,omitempty" description:"Specifies a KubeVIP Interface" example:"ens18" yaml:"interface,omitempty"`
}

// P2PNetworkExtended is a meta structure to hold the different rules for managing the P2P network, which are not compatible between each other.
type P2PNetworkExtended struct {
}

// P2PAutoDisabled is used to validate that when p2p.auto is disabled, then neither p2p.auto.ha not p2p.network_token can be set.
type P2PAutoDisabled struct {
	NetworkToken string `json:"network_token,omitempty" const:"" required:"true" yaml:"network_token,omitempty"`
	Auto         struct {
		Enable bool `json:"enable" const:"false" required:"true" yaml:"enable"`
		Ha     struct {
			Enable bool `json:"enable" const:"false" yaml:"enable"`
		} `json:"ha" yaml:"ha"`
	} `json:"auto" yaml:"auto"`
}

// P2PAutoEnabled is used to validate that when p2p.auto is set, p2p.network_token has to be set.
type P2PAutoEnabled struct {
	NetworkToken string `json:"network_token" required:"true" minLength:"1" description:"network_token is the shared secret used by the nodes to co-ordinate with p2p" yaml:"network_token"`
	Auto         struct {
		Enable bool `json:"enable,omitempty" const:"true" yaml:"enable,omitempty"`
		Ha     struct {
			Enable      bool `json:"enable" const:"true" yaml:"enable"`
			MasterNodes int  `json:"master_nodes,omitempty" minimum:"1" description:"Number of HA additional master nodes. A master node is always required for creating the cluster and is implied." yaml:"master_nodes,omitempty"`
		} `json:"ha" yaml:"ha"`
	} `json:"auto,omitempty" yaml:"auto,omitempty"`
}

var _ jsonschemago.OneOfExposer = P2PNetworkExtended{}

// JSONSchemaOneOf defines that different which are the different valid p2p network rules and states that one and only one of them needs to be validated for the entire schema to be valid.
func (P2PNetworkExtended) JSONSchemaOneOf() []interface{} {
	return []interface{}{
		P2PAutoEnabled{}, P2PAutoDisabled{},
	}
}

// VPN represents the vpn block in the Kairos configuration.
type VPN struct {
	Create bool          `json:"vpn,omitempty" default:"true" yaml:"vpn,omitempty"`
	Use    bool          `json:"use,omitempty" default:"true" yaml:"use,omitempty"`
	Envs   []interface{} `json:"env,omitempty" yaml:"env,omitempty"`
}
