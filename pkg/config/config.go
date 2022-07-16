package config

import (
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	retry "github.com/avast/retry-go"
	"github.com/c3os-io/c3os/internal/machine"
	yip "github.com/mudler/yip/pkg/schema"

	"gopkg.in/yaml.v2"
)

type Install struct {
	Auto     bool   `yaml:"auto,omitempty"`
	Reboot   bool   `yaml:"reboot,omitempty"`
	Device   string `yaml:"device,omitempty"`
	Poweroff bool   `yaml:"poweroff,omitempty"`
}

type Config struct {
	Install *Install `yaml:"install,omitempty"`
	//cloudFileContent string
	originalData       map[string]interface{}
	location           string
	header             string
	ConfigURL          string            `yaml:"config_url,omitempty"`
	Options            map[string]string `yaml:"options,omitempty"`
	IgnoreBundleErrors bool              `yaml:"ignore_bundles_errors,omitempty"`
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
	return (header == "#cloud-config") || (header == "#c3os-config") || (header == "#node-config"), header
}

func (b Bundles) Options() (res [][]machine.BundleOption) {
	for _, bundle := range b {
		for _, t := range bundle.Targets {
			opts := []machine.BundleOption{machine.WithRepository(bundle.Repository), machine.WithTarget(t)}
			if bundle.Rootfs != "" {
				opts = append(opts, machine.WithRootFS(bundle.Rootfs))
			}
			if bundle.DB != "" {
				opts = append(opts, machine.WithDBPath(bundle.DB))
			}
			res = append(res, opts)
		}
	}
	return
}

func (c Config) Unmarshal(o interface{}) error {
	return yaml.Unmarshal([]byte(c.String()), o)
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
	for _, f := range files {
		if fileSize(f) > 1.0 {
			//fmt.Println("warning: Skipping file ", f, "as exceeds 1 MB in size")
			continue
		}
		b, err := ioutil.ReadFile(f)
		if err == nil {
			yaml.Unmarshal(b, c)
			if exists, header := HasHeader(string(b), ""); c.IsValid() || exists {
				c.location = f
				yaml.Unmarshal(b, &c.originalData)
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
			yaml.Unmarshal(b, c)
			c.location = lastYamlFileFound
			yaml.Unmarshal(b, &c.originalData)
		}
	}

	if o.MergeBootCMDLine {
		d, err := machine.DotToYAML(o.BootCMDLineFile)
		if err == nil { // best-effort
			yaml.Unmarshal(d, c)
			// Merge back to originalData only config which are part of the config structure
			// This avoid garbage as unrelated bootargs to be merged in.
			dat, err := yaml.Marshal(c)
			if err == nil {
				yaml.Unmarshal(dat, &c.originalData)
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

		yaml.Unmarshal(body, c)
		yaml.Unmarshal(body, &c.originalData)
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
	c, err := Scan(Directories(dir...))
	if err != nil {
		return fmt.Errorf("no config file found: %w", err)
	}

	header := "#node-config"

	if hasHeader, head := HasHeader(c.String(), ""); hasHeader {
		header = head
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

	fi, err := os.Stat(c.location)
	if err != nil {
		return err
	}

	return ioutil.WriteFile(c.location, []byte(AddHeader(header, string(d))), fi.Mode().Perm())
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
