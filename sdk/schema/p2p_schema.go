package schema

import (
	jsonschemago "github.com/swaggest/jsonschema-go"
)

// P2PSchema represents the P2P block in the Kairos configuration. It is used to enables and configure the p2p full-mesh functionalities.
type P2PSchema struct {
	_            struct{} `title:"Kairos Schema: P2P block" description:"The p2p block enables the p2p full-mesh functionalities."`
	Role         string   `json:"role,omitempty" default:"" enum:"[\"\",\"master\",\"worker\",\"none\"]" description:"Pin this node to a role. Leave unset (or \"none\") to take the role from the p2p ledger."`
	NetworkID    string   `json:"network_id,omitempty" description:"User defined network-id. Can be used to have multiple clusters in the same network"`
	DNS          bool     `json:"dns,omitempty" description:"Enable embedded DNS See also: https://mudler.github.io/edgevpn/docs/concepts/overview/dns/"`
	DHTEnabled   bool     `json:"dht_enabled,omitempty" default:"true" description:"Use the public DHT to discover nodes. Turning it off makes co-ordination discover nodes only in the local network"`
	DisableDHT   bool     `json:"disable_dht,omitempty" description:"Deprecated spelling of p2p.dht_enabled, with the opposite meaning. Still read, and refused only when p2p.dht_enabled contradicts it"`
	LogLevel     string   `json:"loglevel,omitempty" default:"debug" enum:"[\"trace\",\"debug\",\"info\",\"warn\",\"error\",\"fatal\"]" description:"Log level of the p2p provider"`
	MinimumNodes int      `json:"minimum_nodes,omitempty" minimum:"0" description:"Number of nodes the network waits for before assigning roles"`
	DynamicRoles bool     `json:"dynamic_roles,omitempty" description:"Re-assign roles as nodes join and leave, instead of pinning them on the first election"`
	P2PNetworkExtended
	VPN `json:"vpn,omitempty"`
}

// KubeVIPSchema represents the kubevip block in the Kairos configuration. It sets the Elastic IP used in KubeVIP. Only valid with p2p.
type KubeVIPSchema struct {
	_           struct{} `title:"Kairos Schema: KubeVIP block" description:"Sets the Elastic IP used in KubeVIP. Only valid with p2p"`
	EIP         string   `json:"eip,omitempty" example:"192.168.1.110"`
	ManifestURL string   `json:"manifest_url,omitempty" description:"Specify a manifest URL for KubeVIP." default:""`
	Enabled     bool     `json:"enabled,omitempty" description:"Enables KubeVIP"`
	Enable      bool     `json:"enable,omitempty" description:"Deprecated spelling of kubevip.enabled. Still read, and refused only when kubevip.enabled is set to a different value"`
	Interface   string   `json:"interface,omitempty" description:"Specifies a KubeVIP Interface" example:"ens18"`
	StaticPod   bool     `json:"static_pod,omitempty" description:"Deploy KubeVIP as a static pod instead of a daemonset"`
	Version     string   `json:"version,omitempty" description:"Tag of the KubeVIP image to deploy" example:"v0.8.0"`
	Image       string   `json:"image,omitempty" description:"Repository of the KubeVIP image to deploy" example:"ghcr.io/kube-vip/kube-vip"`
}

// P2PNetworkExtended is a meta structure to hold the different rules for managing the P2P network, which are not compatible between each other.
type P2PNetworkExtended struct {
}

// P2PAutoDisabled is used to validate that when p2p.auto is disabled, then neither p2p.auto.ha not p2p.network_token can be set.
// The deprecated `enable` spelling is optional here and pinned to the same
// value, so that a config carrying both spellings in agreement validates, and
// one carrying them in disagreement does not. That mirrors what the provider
// does with the pair.
type P2PAutoDisabled struct {
	NetworkToken string `json:"network_token,omitempty" const:"" required:"true"`
	Auto         struct {
		Enabled bool `json:"enabled" const:"false" required:"true"`
		Enable  bool `json:"enable,omitempty" const:"false"`
		Ha      struct {
			Enabled bool `json:"enabled" const:"false"`
			Enable  bool `json:"enable,omitempty" const:"false"`
		} `json:"ha"`
	} `json:"auto"`
}

// P2PAutoDisabledDeprecated is P2PAutoDisabled for the deprecated `enable`
// spelling. It is a branch of its own rather than a second optional key on
// P2PAutoDisabled because that branch requires the key: one struct accepting
// either spelling would have to require neither, which would make a bare
// `p2p: {role: worker}` validate. `enabled` is optional here for the same
// reason `enable` is optional on P2PAutoDisabled.
type P2PAutoDisabledDeprecated struct {
	NetworkToken string `json:"network_token,omitempty" const:"" required:"true"`
	Auto         struct {
		Enable  bool `json:"enable" const:"false" required:"true"`
		Enabled bool `json:"enabled,omitempty" const:"false"`
		Ha      struct {
			Enable  bool `json:"enable" const:"false"`
			Enabled bool `json:"enabled,omitempty" const:"false"`
		} `json:"ha"`
	} `json:"auto"`
}

// P2PAutoEnabled is used to validate that when p2p.auto is set, p2p.network_token has to be set.
// Both `enabled` and the deprecated `enable` are accepted here, and neither is
// required, because p2p.network_token on its own already enables auto.
type P2PAutoEnabled struct {
	NetworkToken string `json:"network_token" required:"true" minLength:"1" description:"network_token is the shared secret used by the nodes to co-ordinate with p2p"`
	Auto         struct {
		Enabled bool `json:"enabled,omitempty" const:"true"`
		Enable  bool `json:"enable,omitempty" const:"true"`
		Ha      struct {
			Enabled     bool   `json:"enabled,omitempty" const:"true"`
			Enable      bool   `json:"enable,omitempty" const:"true"`
			ExternalDB  string `json:"external_db,omitempty" description:"Datastore endpoint used instead of the embedded one. Only supported with k3s."`
			MasterNodes int    `json:"master_nodes,omitempty" minimum:"1" description:"Number of HA additional master nodes. A master node is always required for creating the cluster and is implied."`
		} `json:"ha"`
	} `json:"auto,omitempty"`
}

var _ jsonschemago.AnyOfExposer = P2PNetworkExtended{}

// JSONSchemaAnyOf lists the valid p2p network rules. At least one of them has to
// validate for the whole schema to be valid.
//
// This is `anyOf` rather than `oneOf` because the two disabled branches overlap
// on purpose: a config that spells the key both ways, in agreement, matches both
// of them, and `oneOf` would reject it. P2PAutoEnabled cannot overlap with
// either, since it requires a non-empty p2p.network_token and both disabled
// branches pin it to the empty string.
func (P2PNetworkExtended) JSONSchemaAnyOf() []interface{} {
	return []interface{}{
		P2PAutoEnabled{}, P2PAutoDisabled{}, P2PAutoDisabledDeprecated{},
	}
}

// VPN represents the vpn block in the Kairos configuration.
type VPN struct {
	Create bool                   `json:"create,omitempty" default:"true"`
	Use    bool                   `json:"use,omitempty" default:"true"`
	Envs   map[string]interface{} `json:"env,omitempty"`
}
