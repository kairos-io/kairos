package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/kairos-io/kairos-sdk/bundles"
	"github.com/kairos-io/kairos/v2/pkg/config/collector"
	schema "github.com/kairos-io/kairos/v2/pkg/config/schemas"
	yip "github.com/mudler/yip/pkg/schema"

	"gopkg.in/yaml.v3"
)

const (
	DefaultWebUIListenAddress = ":8080"
	FilePrefix                = "file://"
)

type Install struct {
	Auto                   bool              `yaml:"auto,omitempty"`
	Reboot                 bool              `yaml:"reboot,omitempty"`
	Device                 string            `yaml:"device,omitempty"`
	Poweroff               bool              `yaml:"poweroff,omitempty"`
	GrubOptions            map[string]string `yaml:"grub_options,omitempty"`
	Bundles                Bundles           `yaml:"bundles,omitempty"`
	Encrypt                []string          `yaml:"encrypted_partitions,omitempty"`
	SkipEncryptCopyPlugins bool              `yaml:"skip_copy_kcrypt_plugin,omitempty"`
	Env                    []string          `yaml:"env,omitempty"`
	Image                  string            `yaml:"image,omitempty"`
	EphemeralMounts        []string          `yaml:"ephemeral_mounts,omitempty"`
	BindMounts             []string          `yaml:"bind_mounts,omitempty"`
}

type Config struct {
	Install          *Install `yaml:"install,omitempty"`
	collector.Config `yaml:"-"`
	// TODO: Remove this too?
	ConfigURL          string            `yaml:"config_url,omitempty"`
	Options            map[string]string `yaml:"options,omitempty"`
	FailOnBundleErrors bool              `yaml:"fail_on_bundles_errors,omitempty"`
	Bundles            Bundles           `yaml:"bundles,omitempty"`
	GrubOptions        map[string]string `yaml:"grub_options,omitempty"`
	Env                []string          `yaml:"env,omitempty"`
}

type Bundles []Bundle

type Bundle struct {
	Repository string   `yaml:"repository,omitempty"`
	Rootfs     string   `yaml:"rootfs_path,omitempty"`
	DB         string   `yaml:"db_path,omitempty"`
	LocalFile  bool     `yaml:"local_file,omitempty"`
	Targets    []string `yaml:"targets,omitempty"`
}

const DefaultHeader = "#cloud-config"

func HasHeader(userdata, head string) (bool, string) {
	header := strings.SplitN(userdata, "\n", 2)[0]

	// Trim trailing whitespaces
	header = strings.TrimRightFunc(header, unicode.IsSpace)

	if head != "" {
		return head == header, header
	}
	return (header == DefaultHeader) || (header == "#kairos-config") || (header == "#node-config"), header
}

func (b Bundles) Options() (res [][]bundles.BundleOption) {
	for _, bundle := range b {
		for _, t := range bundle.Targets {
			opts := []bundles.BundleOption{bundles.WithRepository(bundle.Repository), bundles.WithTarget(t)}
			if bundle.Rootfs != "" {
				opts = append(opts, bundles.WithRootFS(bundle.Rootfs))
			}
			if bundle.DB != "" {
				opts = append(opts, bundles.WithDBPath(bundle.DB))
			}
			if bundle.LocalFile {
				opts = append(opts, bundles.WithLocalFile(true))
			}
			res = append(res, opts)
		}
	}
	return
}

// HasConfigURL returns true if ConfigURL has been set and false if it's empty.
func (c Config) HasConfigURL() bool {
	return c.ConfigURL != ""
}

// FilterKeys is used to pass to any other pkg which might want to see which part of the config matches the Kairos config.
func FilterKeys(d []byte) ([]byte, error) {
	cmdLineFilter := Config{}
	err := yaml.Unmarshal(d, &cmdLineFilter)
	if err != nil {
		return []byte{}, err
	}

	out, err := yaml.Marshal(cmdLineFilter)
	if err != nil {
		return []byte{}, err
	}

	return out, nil
}

func Scan(opts ...collector.Option) (c *Config, err error) {
	result := &Config{}

	o := &collector.Options{}
	if err := o.Apply(opts...); err != nil {
		return result, err
	}

	genericConfig, err := collector.Scan(o, FilterKeys)
	if err != nil {
		return result, err

	}
	result.Config = *genericConfig
	configStr, err := genericConfig.String()
	if err != nil {
		return result, err
	}

	err = yaml.Unmarshal([]byte(configStr), result)
	if err != nil {
		return result, err
	}

	kc, err := schema.NewConfigFromYAML(configStr, schema.RootSchema{})
	if err != nil {
		if !o.NoLogs && !o.StrictValidation {
			fmt.Printf("WARNING: %s\n", err.Error())
		}

		if o.StrictValidation {
			return result, fmt.Errorf("ERROR: %s", err.Error())
		}
	}

	if !kc.IsValid() {
		if !o.NoLogs && !o.StrictValidation {
			fmt.Printf("WARNING: %s\n", kc.ValidationError.Error())
		}

		if o.StrictValidation {
			return result, fmt.Errorf("ERROR: %s", kc.ValidationError.Error())
		}
	}

	return result, nil
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
	return os.WriteFile(filepath.Join("usr", "local", "cloud-config", fmt.Sprintf("100_%s.yaml", name)), dnsYAML, 0700)
}

func FromString(s string, o interface{}) error {
	return yaml.Unmarshal([]byte(s), o)
}

func MergeYAML(objs ...interface{}) ([]byte, error) {
	content := [][]byte{}
	for _, o := range objs {
		dat, err := yaml.Marshal(o)
		if err != nil {
			return []byte{}, err
		}
		content = append(content, dat)
	}

	finalData := make(map[string]interface{})

	for _, c := range content {
		if err := yaml.Unmarshal(c, &finalData); err != nil {
			return []byte{}, err
		}
	}

	return yaml.Marshal(finalData)
}

func AddHeader(header, data string) string {
	return fmt.Sprintf("%s\n%s", header, data)
}
