package agent

import (
	"os"

	"github.com/kairos-io/kairos/internal/kairos"

	"gopkg.in/yaml.v2"
)

type BrandingText struct {
	InteractiveInstall string `yaml:"interactive-install"`
	Install            string `yaml:"install"`
	Reset              string `yaml:"reset"`
	Recovery           string `yaml:"recovery"`
}
type WebUI struct {
	Disable       bool   `yaml:"disable"`
	ListenAddress string `yaml:"listen_address"`
}

func (w WebUI) HasAddress() bool {
	return w.ListenAddress != ""
}

type Config struct {
	Fast     bool         `yaml:"fast,omitempty"`
	WebUI    WebUI        `yaml:"webui"`
	Branding BrandingText `yaml:"branding"`
}

func LoadConfig(path ...string) (*Config, error) {
	if len(path) == 0 {
		path = append(path, "/etc/kairos/agent.yaml", "/etc/elemental/config.yaml")
	}

	cfg := &Config{}

	for _, p := range path {
		f, err := os.ReadFile(p)
		if err == nil {
			yaml.Unmarshal(f, cfg) //nolint:errcheck
		}
	}

	if cfg.Branding.InteractiveInstall == "" {
		f, err := os.ReadFile(kairos.BrandingFile("interactive_install_text"))
		if err == nil {
			cfg.Branding.InteractiveInstall = string(f)
		}
	}

	if cfg.Branding.Install == "" {
		f, err := os.ReadFile(kairos.BrandingFile("install_text"))
		if err == nil {
			cfg.Branding.Install = string(f)
		}
	}

	if cfg.Branding.Recovery == "" {
		f, err := os.ReadFile(kairos.BrandingFile("recovery_text"))
		if err == nil {
			cfg.Branding.Recovery = string(f)
		}
	}

	if cfg.Branding.Reset == "" {
		f, err := os.ReadFile(kairos.BrandingFile("reset_text"))
		if err == nil {
			cfg.Branding.Reset = string(f)
		}
	}

	return cfg, nil
}
