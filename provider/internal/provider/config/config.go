package config

import (
	"fmt"

	"github.com/kube-vip/kube-vip/pkg/kubevip"
	"gopkg.in/yaml.v3"
)

const (
	K3sDistro = "k3s"
	K0sDistro = "k0s"
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
// Both spellings at once is refused rather than resolved: their values can
// disagree, and picking one silently decides whether a cluster comes up.
func resolveEnabled(block string, enabled, deprecated *bool) (*bool, error) {
	if enabled != nil && deprecated != nil {
		return nil, fmt.Errorf(
			"%[1]s.enabled and %[1]s.enable are both set; %[1]s.enable is deprecated, keep only %[1]s.enabled",
			block)
	}
	if enabled != nil {
		return enabled, nil
	}

	return deprecated, nil
}

type P2P struct {
	NetworkToken string `yaml:"network_token,omitempty"`
	NetworkID    string `yaml:"network_id,omitempty"`
	Role         string `yaml:"role,omitempty"`
	DNS          bool   `yaml:"dns,omitempty"`
	LogLevel     string `yaml:"loglevel,omitempty"`
	VPN          VPN    `yaml:"vpn,omitempty"`

	MinimumNodes int  `yaml:"minimum_nodes,omitempty"`
	DisableDHT   bool `yaml:"disable_dht,omitempty"`
	Auto         Auto `yaml:"auto,omitempty"`

	DynamicRoles bool `yaml:"dynamic_roles,omitempty"`
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
	Interface   string `yaml:"interface,omitempty"`
	Enabled     *bool  `yaml:"enabled,omitempty"`
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
