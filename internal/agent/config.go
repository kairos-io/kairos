package agent

import (
	"io/ioutil"

	"github.com/c3os-io/c3os/internal/c3os"
	"gopkg.in/yaml.v2"
)

type BrandingText struct {
	Install  string `yaml:"install"`
	Reset    string `yaml:"reset"`
	Recovery string `yaml:"recovery"`
}

type Config struct {
	Branding BrandingText `yaml:"branding"`
}

func LoadConfig(path ...string) (*Config, error) {
	if len(path) == 0 {
		path = append(path, "/etc/c3os/agent.yaml", "/etc/elemental/config.yaml")
	}

	cfg := &Config{}

	for _, p := range path {
		f, err := ioutil.ReadFile(p)
		if err == nil {
			yaml.Unmarshal(f, cfg)
		}
	}

	if cfg.Branding.Install == "" {
		f, err := ioutil.ReadFile(c3os.BrandingFile("install_text"))
		if err == nil {
			cfg.Branding.Install = string(f)
		}
	}

	if cfg.Branding.Recovery == "" {
		f, err := ioutil.ReadFile(c3os.BrandingFile("recovery_text"))
		if err == nil {
			cfg.Branding.Recovery = string(f)
		}
	}

	if cfg.Branding.Reset == "" {
		f, err := ioutil.ReadFile(c3os.BrandingFile("reset_text"))
		if err == nil {
			cfg.Branding.Recovery = string(f)
		}
	}

	return cfg, nil
}
