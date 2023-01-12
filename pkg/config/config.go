package config

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	retry "github.com/avast/retry-go"
	"github.com/itchyny/gojq"
	"github.com/kairos-io/kairos/pkg/machine"
	"github.com/kairos-io/kairos/sdk/bundles"
	"github.com/kairos-io/kairos/sdk/unstructured"
	yip "github.com/mudler/yip/pkg/schema"

	"gopkg.in/yaml.v3"
)

const DefaultWebUIListenAddress = ":8080"

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
	CustomBindMounts       []string          `yaml:"additional_persistent_mounts,omitempty"`
	CustomEphemeralMounts  []string          `yaml:"additional_ephemeral_mounts,omitempty"`
}

type Config struct {
	Install *Install `yaml:"install,omitempty"`
	//cloudFileContent string
	originalData       map[string]interface{}
	header             string
	ConfigURL          string            `yaml:"config_url,omitempty"`
	Options            map[string]string `yaml:"options,omitempty"`
	FailOnBundleErrors bool              `yaml:"fail_on_bundles_errors,omitempty"`
	Bundles            Bundles           `yaml:"bundles,omitempty"`
	GrubOptions        map[string]string `yaml:"grub_options,omitempty"`
	Env                []string          `yaml:"env,omitempty"`
}

type Bundles []Bundle

type Bundle struct {
	Repository string `yaml:"repository,omitempty"`
	Rootfs     string `yaml:"rootfs_path,omitempty"`
	DB         string `yaml:"db_path,omitempty"`

	Targets []string `yaml:"targets,omitempty"`
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

func (c Config) Query(s string) (res string, err error) {
	s = fmt.Sprintf(".%s", s)
	jsondata := map[string]interface{}{}

	err = yaml.Unmarshal([]byte(c.String()), &jsondata)
	if err != nil {
		return
	}
	query, err := gojq.Parse(s)
	if err != nil {
		return res, err
	}

	iter := query.Run(jsondata) // or query.RunWithContext
	for {
		v, ok := iter.Next()
		if !ok {
			break
		}
		if err, ok := v.(error); ok {
			return res, fmt.Errorf("failed parsing, error: %w", err)
		}

		dat, err := yaml.Marshal(v)
		if err != nil {
			break
		}
		res += string(dat)
	}
	return
}

func allFiles(dir []string) []string {
	files := []string{}
	for _, d := range dir {
		if f, err := listFiles(d); err == nil {
			files = append(files, f...)
		}
	}
	return files
}

func Scan(opts ...Option) (c *Config, err error) {
	o := &Options{}
	if err := o.Apply(opts...); err != nil {
		return nil, err
	}

	c = parseConfig(o.ScanDir, o.NoLogs)

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

				body, err = io.ReadAll(resp.Body)
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

		if exists, header := HasHeader(string(body), ""); exists {
			c.header = header
		}
	}

	if c.header == "" {
		c.header = DefaultHeader
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
			if !info.IsDir() {
				content = append(content, path)
			}

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

func FindYAMLWithKey(s string, opts ...Option) ([]string, error) {
	o := &Options{}

	result := []string{}
	if err := o.Apply(opts...); err != nil {
		return result, err
	}

	files := allFiles(o.ScanDir)

	for _, f := range files {
		dat, err := os.ReadFile(f)
		if err != nil {
			fmt.Printf("warning: skipping file '%s' - %s\n", f, err.Error())
		}

		found, err := unstructured.YAMLHasKey(s, dat)
		if err != nil {
			fmt.Printf("warning: skipping file '%s' - %s\n", f, err.Error())
		}

		if found {
			result = append(result, f)
		}

	}

	return result, nil
}

// parseConfig merges all config back in one structure.
func parseConfig(dir []string, nologs bool) *Config {
	files := allFiles(dir)
	c := &Config{}
	for _, f := range files {
		if fileSize(f) > 1.0 {
			if !nologs {
				fmt.Printf("warning: skipping %s. too big (>1MB)\n", f)
			}
			continue
		}
		if strings.Contains(f, "userdata") || filepath.Ext(f) == ".yml" || filepath.Ext(f) == ".yaml" {
			b, err := os.ReadFile(f)
			if err != nil {
				if !nologs {
					fmt.Printf("warning: skipping %s. %s\n", f, err.Error())
				}
				continue
			}
			yaml.Unmarshal(b, c)               //nolint:errcheck
			yaml.Unmarshal(b, &c.originalData) //nolint:errcheck
			if exists, header := HasHeader(string(b), ""); exists {
				c.header = header
			}
		} else {
			if !nologs {
				fmt.Printf("warning: skipping %s (extension).\n", f)
			}
		}
	}

	return c
}
