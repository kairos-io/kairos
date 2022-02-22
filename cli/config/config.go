package config

import (
	"errors"
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

type K3s struct {
	Env         map[string]string `yaml:"env,omitempty"`
	ReplaceEnv  bool              `yaml:"replace_env,omitempty"`
	ReplaceArgs bool              `yaml:"replace_args,omitempty"`
	Args        []string          `yaml:"args,omitempty"`
}

type Config struct {
	C3OS             *C3OS             `yaml:"c3os,omitempty"`
	K3sAgent         K3s               `yaml:"k3s-agent,omitempty"`
	K3s              K3s               `yaml:"k3s,omitempty"`
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

func ReplaceToken(dir, token string) (err error) {
	c := &Config{}
	files, err := listFiles(dir)
	if err != nil {
		return err
	}
	var configFile string
	perms := os.ModePerm
	for _, f := range files {
		b, err := ioutil.ReadFile(f)
		if err == nil {
			yaml.Unmarshal(b, c)
			if c.C3OS != nil {
				configFile = f
				c.cloudFileContent = string(b)
				i, err := os.Stat(f)
				if err == nil {
					perms = i.Mode()
				}
				break
			}
		}
	}

	if c.C3OS == nil {
		return errors.New("no config file found")
	}

	content := map[interface{}]interface{}{}

	if err := yaml.Unmarshal([]byte(c.String()), &content); err != nil {
		return err
	}

	section, exists := content["c3os"]
	if !exists {
		return errors.New("no c3os section in config file")
	}

	dd, err := yaml.Marshal(section)
	if err != nil {
		return err
	}

	piece := map[string]interface{}{}

	if err := yaml.Unmarshal(dd, &piece); err != nil {
		return err
	}

	piece["network_token"] = token
	content["c3os"] = piece

	d, err := yaml.Marshal(content)
	if err != nil {
		return err
	}

	return ioutil.WriteFile(configFile, d, perms)
}
