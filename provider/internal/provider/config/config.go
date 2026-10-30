package config

import (
	"fmt"

	"github.com/kube-vip/kube-vip/pkg/kubevip"
	"gopkg.in/yaml.v3"
)

const (
	K3sDistro = "k3s"
	K0sDistro = "k0s"

	// RoleNone is the value the Kairos schema used to advertise as the
	// default for p2p.role. No role handler was ever registered for it, so
	// it is treated as "no hardcoded role".
	RoleNone = "none"
)

// DeprecatedKey names a key spelling that is still accepted, and the spelling
// that replaces it, so a caller can tell the user what to change.
type DeprecatedKey struct {
	Key         string
	Replacement string
}

// resolveEnabled folds the deprecated `enable` spelling into the canonical
// `enabled` one. The two blocks that already shipped `enabled`, k3s and k0s,
// are what makes `enable` the odd one out. See kairos-io/kairos#1016.
//
// Both spellings at once is accepted while they agree. `enabled` was ignored
// in these blocks before this change, so a config that writes both already
// boots a cluster today, and the collector produces one whenever it merges an
// older file with a newer one. Only a disagreement is refused: there the value
// picked decides whether the cluster comes up, and the config does not say
// which the author meant.
func resolveEnabled(block string, enabled, deprecated *bool) (*bool, error) {
	if enabled != nil && deprecated != nil && *enabled != *deprecated {
		return nil, fmt.Errorf(
			"%[1]s.enabled is %[2]t and the deprecated %[1]s.enable is %[3]t; they disagree, so remove %[1]s.enable and keep %[1]s.enabled",
			block, *enabled, *deprecated)
	}
	if enabled != nil {
		return enabled, nil
	}

	return deprecated, nil
}

// resolveDHTEnabled folds the deprecated negative `disable_dht` spelling into
// the positive `dht_enabled` one. The block it sits in reads `dns` and
// `dynamic_roles` positively, so `disable_dht` was the one key an author had
// to read backwards. See kairos-io/kairos#1016.
//
// The two are refused only when they contradict each other, the same rule
// resolveEnabled applies, except that agreement here means opposite values:
// `disable_dht: true` says the same thing as `dht_enabled: false`.
func resolveDHTEnabled(enabled, deprecatedDisable *bool) (*bool, error) {
	if deprecatedDisable == nil {
		return enabled, nil
	}

	fromDeprecated := !*deprecatedDisable
	if enabled != nil && *enabled != fromDeprecated {
		return nil, fmt.Errorf(
			"p2p.dht_enabled is %t and the deprecated p2p.disable_dht is %t; they disagree, so remove p2p.disable_dht and keep p2p.dht_enabled",
			*enabled, *deprecatedDisable)
	}
	if enabled != nil {
		return enabled, nil
	}

	return &fromDeprecated, nil
}

type P2P struct {
	NetworkToken string `yaml:"network_token,omitempty"`
	NetworkID    string `yaml:"network_id,omitempty"`
	Role         string `yaml:"role,omitempty"`
	DNS          bool   `yaml:"dns,omitempty"`
	LogLevel     string `yaml:"loglevel,omitempty"`
	VPN          VPN    `yaml:"vpn,omitempty"`

	MinimumNodes int `yaml:"minimum_nodes,omitempty"`
	// DHTEnabled turns on the public DHT the nodes use to find each other.
	// Unset means on, which is what writing neither key has always done.
	DHTEnabled *bool `yaml:"dht_enabled,omitempty"`
	// DeprecatedDisableDHT carries the `disable_dht` spelling, which says the
	// opposite. UnmarshalYAML folds it into DHTEnabled, so every reader has to
	// use IsDHTEnabled; this field only records that the deprecated spelling
	// is the one the user wrote.
	DeprecatedDisableDHT *bool `yaml:"disable_dht,omitempty"`
	Auto                 Auto  `yaml:"auto,omitempty"`

	DynamicRoles bool `yaml:"dynamic_roles,omitempty"`
}

// UnmarshalYAML accepts `dht_enabled` and the deprecated `disable_dht`.
func (p *P2P) UnmarshalYAML(value *yaml.Node) error {
	type plain P2P
	var decoded plain
	if err := value.Decode(&decoded); err != nil {
		return err
	}
	*p = P2P(decoded)

	enabled, err := resolveDHTEnabled(p.DHTEnabled, p.DeprecatedDisableDHT)
	if err != nil {
		return err
	}
	p.DHTEnabled = enabled

	return nil
}

// IsDHTEnabled reports whether the nodes should use the public DHT. The
// default is on, so only an explicit `dht_enabled: false` or
// `disable_dht: true` turns it off.
func (p P2P) IsDHTEnabled() bool {
	return p.DHTEnabled == nil || *p.DHTEnabled
}

// HardcodedRole returns the role the user pinned this node to, or the empty
// string when the node has to take its role from the p2p ledger. `none` maps
// to the empty string: it was the schema's advertised default, so it is in the
// wild, and nothing ever implemented it as a role.
func (p P2P) HardcodedRole() string {
	if p.Role == RoleNone {
		return ""
	}
	return p.Role
}

func (p P2P) IsAutoEnabled() bool {
	return p.Auto.Enabled != nil && *p.Auto.Enabled || p.NetworkToken != ""
}

type VPN struct {
	Create *bool             `yaml:"create,omitempty"`
	Use    *bool             `yaml:"use,omitempty"`
	Env    map[string]string `yaml:"env,omitempty"`
}

// If no setting is provided by the user,
// we assume that we are going to create and use the VPN
// for the network layer of our cluster.
func (p P2P) UseVPNWithKubernetes() bool {
	return p.VPNNeedsCreation() && (p.VPN.Use == nil || *p.VPN.Use)
}

func (p P2P) VPNNeedsCreation() bool {
	return p.VPN.Create == nil || *p.VPN.Create
}

type Config struct {
	P2P       *P2P    `yaml:"p2p,omitempty"`
	K3sAgent  K3s     `yaml:"k3s-agent,omitempty"`
	K3s       K3s     `yaml:"k3s,omitempty"`
	KubeVIP   KubeVIP `yaml:"kubevip,omitempty"`
	K0sWorker K0s     `yaml:"k0s-worker,omitempty"`
	K0s       K0s     `yaml:"k0s,omitempty"`
}

func (c *Config) IsP2PConfigured() bool {
	return c.P2P != nil
}

// DeprecatedKeys returns the deprecated key spellings this config was written
// with, as cloud-config paths. Decoding accepts them, so nothing else in the
// provider can tell that the user needs to change anything.
func (c *Config) DeprecatedKeys() []DeprecatedKey {
	var keys []DeprecatedKey

	if c.P2P != nil {
		if c.P2P.DeprecatedDisableDHT != nil {
			keys = append(keys, DeprecatedKey{Key: "p2p.disable_dht", Replacement: "p2p.dht_enabled"})
		}
		if c.P2P.Auto.DeprecatedEnable != nil {
			keys = append(keys, DeprecatedKey{Key: "p2p.auto.enable", Replacement: "p2p.auto.enabled"})
		}
		if c.P2P.Auto.HA.DeprecatedEnable != nil {
			keys = append(keys, DeprecatedKey{Key: "p2p.auto.ha.enable", Replacement: "p2p.auto.ha.enabled"})
		}
	}
	if c.KubeVIP.DeprecatedEnable != nil {
		keys = append(keys, DeprecatedKey{Key: "kubevip.enable", Replacement: "kubevip.enabled"})
	}

	return keys
}

func (c *Config) IsKubernetesConfigured() bool {
	return c.K3s.IsEnabled() || c.K3sAgent.IsEnabled() || c.K0s.IsEnabled() || c.K0sWorker.IsEnabled()
}

type KubeVIP struct {
	EIP         string `yaml:"eip,omitempty"`
	ManifestURL string `yaml:"manifest_url,omitempty"`
	// ManifestSHA256 is the expected sha256 digest (lowercase hex) of the
	// content at ManifestURL. ManifestURL is an operator-supplied,
	// arbitrary source with no identity Kairos can know in advance, so
	// there is nothing to pin in code the way kairos-io's own release
	// artifacts can be; this field lets the operator supply that pin
	// themselves. When set, the download is verified against it and fails
	// closed on a mismatch. When unset, the download proceeds unverified,
	// same as before this field existed.
	ManifestSHA256 string `yaml:"manifest_sha256,omitempty"`
	Interface      string `yaml:"interface,omitempty"`
	Enabled        *bool  `yaml:"enabled,omitempty"`
	// DeprecatedEnable carries the `enable` spelling. UnmarshalYAML folds it
	// into Enabled, so every reader has to use Enabled; this field only
	// records that the deprecated spelling is the one the user wrote.
	DeprecatedEnable *bool  `yaml:"enable,omitempty"`
	StaticPod        bool   `yaml:"static_pod,omitempty"`
	Version          string `yaml:"version,omitempty"`
	Image            string `yaml:"image,omitempty"`
	kubevip.Config
}

// UnmarshalYAML accepts `enabled` and the deprecated `enable`.
func (k *KubeVIP) UnmarshalYAML(value *yaml.Node) error {
	type plain KubeVIP
	var decoded plain
	if err := value.Decode(&decoded); err != nil {
		return err
	}
	*k = KubeVIP(decoded)

	enabled, err := resolveEnabled("kubevip", k.Enabled, k.DeprecatedEnable)
	if err != nil {
		return err
	}
	k.Enabled = enabled

	return nil
}

func (k KubeVIP) IsEnabled() bool {
	return (k.Enabled == nil && k.EIP != "") || (k.Enabled != nil && *k.Enabled)
}

type Auto struct {
	Enabled *bool `yaml:"enabled,omitempty"`
	// DeprecatedEnable carries the `enable` spelling. See KubeVIP.
	DeprecatedEnable *bool `yaml:"enable,omitempty"`
	HA               HA    `yaml:"ha,omitempty"`
}

// UnmarshalYAML accepts `enabled` and the deprecated `enable`.
func (a *Auto) UnmarshalYAML(value *yaml.Node) error {
	type plain Auto
	var decoded plain
	if err := value.Decode(&decoded); err != nil {
		return err
	}
	*a = Auto(decoded)

	enabled, err := resolveEnabled("p2p.auto", a.Enabled, a.DeprecatedEnable)
	if err != nil {
		return err
	}
	a.Enabled = enabled

	return nil
}

func (a Auto) IsEnabled() bool {
	return a.Enabled == nil || (a.Enabled != nil && *a.Enabled)
}

func (ha HA) IsEnabled() bool {
	return (ha.Enabled != nil && *ha.Enabled) || (ha.Enabled == nil && ha.MasterNodes != nil)
}

type HA struct {
	Enabled *bool `yaml:"enabled,omitempty"`
	// DeprecatedEnable carries the `enable` spelling. See KubeVIP.
	DeprecatedEnable *bool  `yaml:"enable,omitempty"`
	ExternalDB       string `yaml:"external_db,omitempty"`
	MasterNodes      *int   `yaml:"master_nodes,omitempty"`
}

// UnmarshalYAML accepts `enabled` and the deprecated `enable`.
func (ha *HA) UnmarshalYAML(value *yaml.Node) error {
	type plain HA
	var decoded plain
	if err := value.Decode(&decoded); err != nil {
		return err
	}
	*ha = HA(decoded)

	enabled, err := resolveEnabled("p2p.auto.ha", ha.Enabled, ha.DeprecatedEnable)
	if err != nil {
		return err
	}
	ha.Enabled = enabled

	return nil
}

type K3s struct {
	Env              map[string]string `yaml:"env,omitempty"`
	ReplaceEnv       bool              `yaml:"replace_env,omitempty"`
	ReplaceArgs      bool              `yaml:"replace_args,omitempty"`
	Args             []string          `yaml:"args,omitempty"`
	Enabled          *bool             `yaml:"enabled,omitempty"`
	EmbeddedRegistry bool              `yaml:"embedded_registry,omitempty"`
}

func (k K3s) IsEnabled() bool {
	return k.Enabled != nil && *k.Enabled
}

type K0s struct {
	Env         map[string]string `yaml:"env,omitempty"`
	ReplaceEnv  bool              `yaml:"replace_env,omitempty"`
	ReplaceArgs bool              `yaml:"replace_args,omitempty"`
	Args        []string          `yaml:"args,omitempty"`
	Enabled     *bool             `yaml:"enabled,omitempty"`
}

func (k K0s) IsEnabled() bool {
	return k.Enabled != nil && *k.Enabled
}
