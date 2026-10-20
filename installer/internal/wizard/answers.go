package wizard

import sdkExtensions "github.com/kairos-io/kairos/v4/sdk/types/extensions"

// Answers is everything the steps collected. It is plain data, so the web UI
// can keep it in the browser between calls and the server holds no session.
type Answers struct {
	Disk         string                   `json:"disk,omitempty"`
	Username     string                   `json:"username,omitempty"`
	PasswordHash string                   `json:"password_hash,omitempty"`
	SSHKeys      []string                 `json:"ssh_keys,omitempty"`
	Hostname     string                   `json:"hostname,omitempty"`
	Timezone     string                   `json:"timezone,omitempty"`
	Keymap       string                   `json:"keymap,omitempty"`
	Extensions   sdkExtensions.Extensions `json:"extensions,omitempty"`
	// Provider holds the provider plugins' fields, nested by their YAML
	// section, and is merged at the top level of the cloud-config.
	Provider     map[string]any `json:"provider,omitempty"`
	FinishAction string         `json:"finish_action,omitempty"`
	// Source is the installer's --source. It is never read from a browser.
	Source string `json:"-"`
}
