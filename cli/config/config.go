package config

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	yip "github.com/mudler/yip/pkg/schema"

	"gopkg.in/yaml.v2"
)

type C3OS struct {
	NetworkToken string `yaml:"network_token,omitempty"`
	Offline      bool   `yaml:"offline,omitempty"`
	Reboot       bool   `yaml:"reboot,omitempty"`
	Device       string `yaml:"device,omitempty"`
	Poweroff     bool   `yaml:"poweroff,omitempty"`
	Role         string `yaml:"role,omitempty"`
	NetworkID    string `yaml:"network_id,omitempty"`
	DNS          bool   `yaml:"dns,omitempty"`
	LogLevel     string `yaml:"loglevel,omitempty"`
}

type K3s struct {
	Env         map[string]string `yaml:"env,omitempty"`
	ReplaceEnv  bool              `yaml:"replace_env,omitempty"`
	ReplaceArgs bool              `yaml:"replace_args,omitempty"`
	Args        []string          `yaml:"args,omitempty"`
	Enabled     bool              `yaml:"enabled,omitempty"`
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

func Scan(dir ...string) (c *Config, err error) {
	c = &Config{}
	files := []string{}
	for _, d := range dir {
		if f, err := listFiles(d); err == nil {
			files = append(files, f...)
		}
	}

	for _, f := range files {
		if fileSize(f) > 1.0 {
			fmt.Println("warning: Skipping file ", f, "as exceeds 1 MB in size")
			continue
		}
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

func fileSize(f string) float64 {
	file, err := os.Open(f)
	if err != nil {
		return 0
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		return 0
	}

	var bytes int64
	bytes = stat.Size()

	var kilobytes int64
	kilobytes = (bytes / 1024)

	var megabytes float64
	megabytes = (float64)(kilobytes / 1024) // cast to type float64
	return megabytes
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

func ReplaceToken(dir []string, token string) (err error) {
	c := &Config{}
	files := []string{}
	for _, d := range dir {
		if f, err := listFiles(d); err == nil {
			files = append(files, f...)
		}
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

type Stage string

const (
	NetworkStage Stage = "network"
)

func (n Stage) String() string {
	return string(n)
}

func SaveCloudConfig(name Stage, yc yip.YipConfig) error {
	dnsYAML, err := yaml.Marshal(yc)
	if err != nil {
		return err
	}
	return ioutil.WriteFile(filepath.Join("usr", "local", "cloud-config", fmt.Sprintf("100_%s.yaml", name)), dnsYAML, 0700)
}
