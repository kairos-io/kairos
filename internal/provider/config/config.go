package config

type C3OS struct {
	NetworkToken string `yaml:"network_token,omitempty"`
	NetworkID    string `yaml:"network_id,omitempty"`
	Role         string `yaml:"role,omitempty"`
	DNS          bool   `yaml:"dns,omitempty"`
	LogLevel     string `yaml:"loglevel,omitempty"`
}

type Config struct {
	C3OS     *C3OS             `yaml:"c3os,omitempty"`
	K3sAgent K3s               `yaml:"k3s-agent,omitempty"`
	K3s      K3s               `yaml:"k3s,omitempty"`
	VPN      map[string]string `yaml:"vpn,omitempty"`
}

type K3s struct {
	Env         map[string]string `yaml:"env,omitempty"`
	ReplaceEnv  bool              `yaml:"replace_env,omitempty"`
	ReplaceArgs bool              `yaml:"replace_args,omitempty"`
	Args        []string          `yaml:"args,omitempty"`
	Enabled     bool              `yaml:"enabled,omitempty"`
}
