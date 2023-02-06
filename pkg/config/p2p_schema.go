package config

import (
	jsonschemago "github.com/swaggest/jsonschema-go"
)

type P2PSchema struct {
	_          struct{} `title:"Kairos Schema: P2P block" description:"The p2p block enables the p2p full-mesh functionalities."`
	Role       string   `json:"role,omitempty" default:"none" enum:"[\"master\",\"worker\",\"none\"]"`
	NetworkID  string   `json:"network_id,omitempty" description:"User defined network-id. Can be used to have multiple clusters in the same network"`
	DNS        bool     `json:"dns,omitempty" description:"Enable embedded DNS See also: https://mudler.github.io/edgevpn/docs/concepts/overview/dns/"`
	DisableDHT bool     `json:"disable_dht,omitempty" default:"true" description:"Disabling DHT makes co-ordination to discover nodes only in the local network"`
	P2PNetworkExtended
	Vpn `json:"vpn,omitempty"`
}

type KubeVIPSchema struct {
	_           struct{} `title:"Kairos Schema: KubeVIP block" description:"Sets the Elastic IP used in KubeVIP. Only valid with p2p"`
	EIP         string   `json:"eip,omitempty" example:"192.168.1.110"`
	ManifestURL string   `json:"manifest_url,omitempty" description:"Specify a manifest URL for KubeVIP." default:""`
	Enable      bool     `json:"enable,omitempty" description:"Enables KubeVIP"`
	Interface   bool     `json:"interface,omitempty" description:"Specifies a KubeVIP Interface" example:"ens18"`
}

type P2PNetworkExtended struct {
}

type P2PAutoDisabled struct {
	NetworkToken string `json:"network_token,omitempty" const:"" required:"true"`
	Auto         struct {
		Enable bool `json:"enable" const:"false" required:"true"`
		Ha     struct {
			Enable bool `json:"enable" const:"false"`
		} `json:"ha"`
	} `json:"auto"`
}

type P2PAutoEnabled struct {
	NetworkToken string `json:"network_token" required:"true" minLength:"1" description:"network_token is the shared secret used by the nodes to co-ordinate with p2p"`
	Auto         struct {
		Enable bool `json:"enable,omitempty" const:"true"`
		Ha     struct {
			Enable      bool `json:"enable" const:"true"`
			MasterNodes int  `json:"master_nodes,omitempty" minimum:"1" description:"Number of HA additional master nodes. A master node is always required for creating the cluster and is implied."`
		} `json:"ha"`
	} `json:"auto,omitempty"`
}

type P2PAuto struct {
	Enable bool `json:"enable,omitempty" const:"true"`
}

var _ jsonschemago.OneOfExposer = P2PNetworkExtended{}

func (P2PNetworkExtended) JSONSchemaOneOf() []interface{} {
	return []interface{}{
		P2PAutoEnabled{}, P2PAutoDisabled{},
	}
}

type Vpn struct {
	Create bool          `json:"vpn,omitempty" default:"true"`
	Use    bool          `json:"use,omitempty" default:"true"`
	Envs   []interface{} `json:"env,omitempty"`
}
