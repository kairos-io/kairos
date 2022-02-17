package config

import (
	"io/ioutil"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v2"
)

type C3OS struct {
	NetworkToken string `yaml:"network_token,omitempty"`
	Offline      bool   `yaml:"offline"`
	Reboot       bool   `yaml:"reboot"`
	Device       string `yaml:"device"`
	Poweroff     bool   `yaml:"poweroff"`
	Role         string `yaml:"role,omitempty"`
	NetworkID    string `yaml:"network_id,omitempty"`
}

type Config struct {
	C3OS             *C3OS             `yaml:"c3os,omitempty"`
	VPN              map[string]string `yaml:"vpn,omitempty"`
	cloudFileContent string
}

func (c Config) String() string {
	return c.cloudFileContent
}

func Scan(dir string) (c *Config, err error) {
	c = &Config{}
	files, err := listFiles(dir)
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		b, err := ioutil.ReadFile(f)
		if err == nil {
			yaml.Unmarshal(b, c)
			if c.C3OS != nil {
				c.cloudFileContent = string(b)
				break
			}
		}
	}
	return c, nil
}

func listFiles(dir string) ([]string, error) {
	content := []string{}

	err := filepath.Walk(dir,
		func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			content = append(content, path)

			return nil
		})

	return content, err
}
