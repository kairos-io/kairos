package config

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	retry "github.com/avast/retry-go"
	"github.com/kairos-io/kairos/pkg/machine"
	"github.com/kairos-io/kairos/sdk/bundles"
	yip "github.com/mudler/yip/pkg/schema"

	"gopkg.in/yaml.v2"
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
}

type Config struct {
	Install *Install `yaml:"install,omitempty"`
	//cloudFileContent string
	originalData       map[string]interface{}
	location           string
	header             string
	ConfigURL          string            `yaml:"config_url,omitempty"`
	Options            map[string]string `yaml:"options,omitempty"`
	FailOnBundleErrors bool              `yaml:"fail_on_bundles_errors,omitempty"`
	Bundles            Bundles           `yaml:"bundles,omitempty"`
}

type Bundles []Bundle

type Bundle struct {
	Repository string `yaml:"repository,omitempty"`
	Rootfs     string `yaml:"rootfs_path,omitempty"`
	DB         string `yaml:"db_path,omitempty"`

	Targets []string `yaml:"targets,omitempty"`
}

func HasHeader(userdata, head string) (bool, string) {
	header := strings.SplitN(userdata, "\n", 2)[0]

	// Trim trailing whitespaces
	header = strings.TrimRightFunc(header, unicode.IsSpace)

	if head != "" {
		return head == header, header
	}
	return (header == "#cloud-config") || (header == "#kairos-config") || (header == "#node-config"), header
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
			res = append(res, opts)
		}
	}
	return
}

func (c Config) Unmarshal(o interface{}) error {
	return yaml.Unmarshal([]byte(c.String()), o)
}

func (c Config) Location() string {
	return c.location
}

func (c Config) Data() map[string]interface{} {
	return c.originalData
}

func (c Config) String() string {
	if len(c.originalData) == 0 {
		dat, err := yaml.Marshal(c)
		if err == nil {
			return string(dat)
		}
	}

	dat, _ := yaml.Marshal(c.originalData)
	if c.header != "" {
		return AddHeader(c.header, string(dat))
	}
	return string(dat)
}

func (c Config) IsValid() bool {
	return c.Install != nil ||
		c.ConfigURL != "" ||
		len(c.Bundles) != 0
}

func Scan(opts ...Option) (c *Config, err error) {

	o := &Options{}

	if err := o.Apply(opts...); err != nil {
		return nil, err
	}

	dir := o.ScanDir

	c = &Config{}
	files := []string{}
	for _, d := range dir {
		if f, err := listFiles(d); err == nil {
			files = append(files, f...)
		}
	}

	configFound := false
	lastYamlFileFound := ""

	// Scanning happens as best-effort, therefore unmarshalling skips errors here.
	for _, f := range files {
		if fileSize(f) > 1.0 {
			//fmt.Println("warning: Skipping file ", f, "as exceeds 1 MB in size")
			continue
		}
		b, err := ioutil.ReadFile(f)
		if err == nil {
			// best effort. skip lint checks
			yaml.Unmarshal(b, c) //nolint:errcheck
			if exists, header := HasHeader(string(b), ""); c.IsValid() || exists {
				c.location = f
				yaml.Unmarshal(b, &c.originalData) //nolint:errcheck
				configFound = true
				if exists {
					c.header = header
				}
				break
			}

			// record back the only yaml file found (if any)
			if strings.HasSuffix(strings.ToLower(f), "yaml") || strings.HasSuffix(strings.ToLower(f), "yml") {
				lastYamlFileFound = f
			}
		}
	}

	// use last recorded if no config is found valid
	if !configFound && lastYamlFileFound != "" {
		b, err := ioutil.ReadFile(lastYamlFileFound)
		if err == nil {
			yaml.Unmarshal(b, c) //nolint:errcheck
			c.location = lastYamlFileFound
			yaml.Unmarshal(b, &c.originalData) //nolint:errcheck
		}
	}

	if o.MergeBootCMDLine {
		d, err := machine.DotToYAML(o.BootCMDLineFile)
		if err == nil { // best-effort
			yaml.Unmarshal(d, c) //nolint:errcheck
			// Merge back to originalData only config which are part of the config structure
			// This avoid garbage as unrelated bootargs to be merged in.
			dat, err := yaml.Marshal(c)
			if err == nil {
				yaml.Unmarshal(dat, &c.originalData) //nolint:errcheck
			}
		}
	}

	if c.ConfigURL != "" {
		var body []byte

		err := retry.Do(
			func() error {
				resp, err := http.Get(c.ConfigURL)
				if err != nil {
					return err
				}
				defer resp.Body.Close()

				body, err = ioutil.ReadAll(resp.Body)
				if err != nil {
					return err
				}

				return nil
			},
		)

		if err != nil {
			return c, fmt.Errorf("could not merge configs: %w", err)
		}

		yaml.Unmarshal(body, c)               //nolint:errcheck
		yaml.Unmarshal(body, &c.originalData) //nolint:errcheck
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

	bytes := stat.Size()
	kilobytes := (bytes / 1024)
	megabytes := (float64)(kilobytes / 1024) // cast to type float64

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
