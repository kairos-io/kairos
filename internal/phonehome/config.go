package phonehome

import (
	"encoding/json"
	"fmt"
	"time"

	sdkConfig "github.com/kairos-io/kairos-sdk/types/config"
	"gopkg.in/yaml.v3"
)

const (
	DefaultHeartbeatInterval = 30 * time.Second
	DefaultReconnectBackoff  = 5 * time.Second
	MaxReconnectBackoff      = 60 * time.Second
	// DefaultCredentialsPath is the on-disk location of the node's saved
	// credentials. It is a filesystem path, not an embedded secret.
	DefaultCredentialsPath = "/usr/local/.kairos/phonehome-credentials.yaml" //nosec G101 -- path, not credential

	// ServiceName / ServicePath are shared between the agent package (which
	// installs the unit) and this package's Uninstall (which tears it down).
	// Keeping them here avoids an internal/phonehome -> internal/agent import
	// cycle when Uninstall needs to touch the same paths.
	ServiceName = "kairos-agent-phonehome"
	ServicePath = "/etc/systemd/system/kairos-agent-phonehome.service"

	// Cloud-config files written during phone-home installation that Uninstall
	// removes. The first is baked by AuroraBoot (either in an artifact image's
	// datasource or by the install script); the second is written on demand
	// by the apply-cloud-config command handler.
	CloudConfigPath       = "/oem/phonehome.yaml"
	RemoteCloudConfigPath = "/oem/99_phonehome_remote.yaml"
)

// Config holds the phone-home configuration, typically read from cloud-config.
type Config struct {
	URL               string            `yaml:"url"`
	RegistrationToken string            `yaml:"registration_token"`
	Group             string            `yaml:"group"`
	Labels            map[string]string `yaml:"labels"`
	HeartbeatInterval time.Duration     `yaml:"heartbeat_interval"`
	ReconnectBackoff  time.Duration     `yaml:"reconnect_backoff"`
	// AllowedCommands is the list of remote commands the node will execute.
	// When nil, defaults to DefaultAllowedCommands (upgrade/reboot only).
	// Destructive commands (exec, reset, apply-cloud-config) must be opted-in
	// explicitly because a DNS hijack / rogue server can reach this endpoint.
	AllowedCommands []string `yaml:"allowed_commands"`
}

// DefaultAllowedCommands is the conservative set of commands a phone-home node
// will execute when the user has not specified allowed_commands in cloud-config.
// It intentionally excludes exec, reset and apply-cloud-config.
//
// `unregister` is included: it is self-destruct of the management link, not a
// privilege escalation — the worst a rogue server can do with it is terminate
// its own connection to the node. Having it on by default means operators can
// cleanly decommission nodes without having to first push a cloud-config
// update opting in to the teardown command.
var DefaultAllowedCommands = []string{"upgrade", "upgrade-recovery", "reboot", "unregister"}

// IsAllowed reports whether the given command name is permitted by this config.
// Matching is exact. A nil AllowedCommands falls back to DefaultAllowedCommands;
// an explicitly empty slice means "deny everything".
func (c *Config) IsAllowed(cmd string) bool {
	list := c.AllowedCommands
	if list == nil {
		list = DefaultAllowedCommands
	}
	for _, a := range list {
		if a == cmd {
			return true
		}
	}
	return false
}

// cloudConfigEnvelope is the shape of the phonehome: section inside a kairos
// cloud-config. We parse the merged Collector output (not individual files) so
// all the usual Kairos config sources and precedence rules apply.
type cloudConfigEnvelope struct {
	Phonehome Config `yaml:"phonehome"`
}

// LoadFromCollector extracts the phonehome configuration from an already-scanned
// Kairos config. It returns (cfg, true, nil) if a phonehome.url is present,
// (nil, false, nil) if the section is missing or empty, and (nil, false, err)
// if the merged config could not be re-serialized or parsed.
func LoadFromCollector(c *sdkConfig.Config) (*Config, bool, error) {
	if c == nil {
		return nil, false, nil
	}
	raw, err := c.Collector.String()
	if err != nil {
		return nil, false, fmt.Errorf("rendering merged cloud-config: %w", err)
	}
	var env cloudConfigEnvelope
	if err := yaml.Unmarshal([]byte(raw), &env); err != nil {
		return nil, false, fmt.Errorf("parsing phonehome section: %w", err)
	}
	if env.Phonehome.URL == "" {
		return nil, false, nil
	}
	cfg := env.Phonehome
	return &cfg, true, nil
}

// Credentials stores the node's registration result.
type Credentials struct {
	NodeID string `yaml:"node_id" json:"id"`
	// APIKey is the node's bearer token returned by the server after
	// registration. The name matches the secret-pattern detector by design —
	// this field *is* the session credential — but it is not a hardcoded value.
	APIKey string `yaml:"api_key" json:"apiKey"` //nosec G101 -- legitimate credential carrier, populated at runtime
}

// NodeAddress is one of the node's reported network addresses. It mirrors the
// Cluster API MachineAddress shape (type + address) and matches the AuroraBoot
// store.NodeAddress wire contract (kairos-io/kairos#4253). Type is one of
// InternalIP | ExternalIP | Hostname.
type NodeAddress struct {
	Type    string `json:"type"`
	Address string `json:"address"`
}

// RegisterRequest is the body sent to POST /api/v1/nodes/register.
type RegisterRequest struct {
	MachineID         string            `json:"machineID"`
	Hostname          string            `json:"hostname"`
	RegistrationToken string            `json:"registrationToken"`
	Group             string            `json:"group,omitempty"`
	Labels            map[string]string `json:"labels,omitempty"`
	OSRelease         map[string]string `json:"osRelease,omitempty"`
	Addresses         []NodeAddress     `json:"addresses,omitempty"`
	BootState         string            `json:"bootState,omitempty"`
}

// WSMessage is the envelope for all WebSocket messages.
type WSMessage struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
}

// HeartbeatData is sent by the agent periodically.
type HeartbeatData struct {
	AgentVersion string            `json:"agentVersion"`
	OSRelease    map[string]string `json:"osRelease,omitempty"`
	Labels       map[string]string `json:"labels,omitempty"`
	Addresses    []NodeAddress     `json:"addresses,omitempty"`
	BootState    string            `json:"bootState,omitempty"`
}

// CommandData is received from the management server.
type CommandData struct {
	ID      string            `json:"id"`
	Command string            `json:"command"`
	Args    map[string]string `json:"args,omitempty"`
}

// CommandStatusData is sent by the agent to report command execution result.
type CommandStatusData struct {
	ID     string `json:"id"`
	Phase  string `json:"phase"`
	Result string `json:"result,omitempty"`
}
