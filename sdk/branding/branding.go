// Package branding holds the two things that brand an installer frontend: the
// lookup for the files under /etc/kairos/branding, and the settings read from
// /etc/kairos/agent.yaml.
//
// It lives in the sdk because kairos-agent and kairos-installer both need them.
// They used to live in agent/internal, which installer/ cannot import, and the
// file lookup was duplicated in installer/internal/tui.
package branding

import (
	"crypto/subtle"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Dir is the directory an image drops its branding overrides into.
const Dir = "/etc/kairos/branding"

// DefaultAgentConfig is the file LoadConfig reads when given no path.
const DefaultAgentConfig = "/etc/kairos/agent.yaml"

// DefaultInteractiveInstallerTitle is used when the image brands no title.
const DefaultInteractiveInstallerTitle = "Kairos Interactive Installer"

// File returns the path to a branding file, whether or not it exists.
func File(name string) string {
	return filepath.Join(Dir, name)
}

// DefaultTitleInteractiveInstaller returns the branded installer title, or the
// default one when the image brands none.
func DefaultTitleInteractiveInstaller() string {
	return defaultTitle(Dir)
}

func defaultTitle(dir string) string {
	if b, err := os.ReadFile(filepath.Join(dir, "interactive_install_text")); err == nil {
		return string(b)
	}
	return DefaultInteractiveInstallerTitle
}

// Text is the set of screens an image can put its own wording on.
type Text struct {
	InteractiveInstall string `yaml:"interactive-install"`
	Install            string `yaml:"install"`
	Reset              string `yaml:"reset"`
	Recovery           string `yaml:"recovery"`
}

// WebUI brands the web installer.
type WebUI struct {
	Disable       bool   `yaml:"disable"`
	ListenAddress string `yaml:"listen_address"`
	// Token is the credential a request has to carry to reach the web UI or
	// the MCP server. Empty, which is the default, leaves both open: an
	// unbranded live ISO is expected to install the machine the user is
	// standing at, and asking that user for a secret nobody set would only
	// lock them out. An image or a cloud config sets it when the installer
	// answers on a network where not everyone may install.
	Token string `yaml:"token"`
}

// TokenParam is the query parameter a frontend accepts the token in, so a URL
// printed on the console or encoded as a QR code is enough to get in.
const TokenParam = "token"

// HasToken reports whether the image asked for a credential.
func (w WebUI) HasToken() bool {
	return w.Token != ""
}

// TokenMatches reports whether got is the token the image set. It is false for
// every got when no token was set, so a caller cannot accidentally authorize a
// request by guessing the empty string: a frontend has to ask HasToken first
// and skip the check entirely.
//
// The comparison is constant time, because the caller is comparing a secret
// against a value an unauthenticated client chose.
func (w WebUI) TokenMatches(got string) bool {
	if w.Token == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(w.Token), []byte(got)) == 1
}

// HasAddress reports whether the image pinned a listen address, as opposed to
// leaving the default one.
func (w WebUI) HasAddress() bool {
	return w.ListenAddress != ""
}

// MCP configures the Model Context Protocol endpoint the installer serves, so
// an agent can drive an install.
//
// There is no address here on purpose: the endpoint is a route on the web
// installer's own server, so where it listens is webui.listen_address and
// nothing else. Disable only decides whether the route is registered at all,
// for an image that wants the browser installer without the agent one.
type MCP struct {
	Disable bool `yaml:"disable"`
}

// Config is the content of /etc/kairos/agent.yaml.
type Config struct {
	Fast     bool  `yaml:"fast,omitempty"`
	WebUI    WebUI `yaml:"webui"`
	MCP      MCP   `yaml:"mcp"`
	Branding Text  `yaml:"branding"`
}

// LoadConfig reads the agent config, falling back to the branding files for any
// screen the config does not word itself. A missing or unreadable file is not an
// error: an unbranded image is the normal case.
func LoadConfig(paths ...string) (*Config, error) {
	return loadConfig(Dir, paths...)
}

func loadConfig(dir string, paths ...string) (*Config, error) {
	if len(paths) == 0 {
		paths = append(paths, DefaultAgentConfig)
	}

	cfg := &Config{}

	for _, p := range paths {
		f, err := os.ReadFile(p)
		if err == nil {
			yaml.Unmarshal(f, cfg) //nolint:errcheck
		}
	}

	for _, fallback := range []struct {
		field *string
		file  string
	}{
		{&cfg.Branding.InteractiveInstall, "interactive_install_text"},
		{&cfg.Branding.Install, "install_text"},
		{&cfg.Branding.Recovery, "recovery_text"},
		{&cfg.Branding.Reset, "reset_text"},
	} {
		if *fallback.field != "" {
			continue
		}
		if f, err := os.ReadFile(filepath.Join(dir, fallback.file)); err == nil {
			*fallback.field = string(f)
		}
	}

	return cfg, nil
}
