// Package configcollector can be used to merge configuration from different
// sources into one YAML.
package collector

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/avast/retry-go"
	"github.com/google/shlex"
	"github.com/imdario/mergo"
	"github.com/kairos-io/kairos/sdk/unstructured"
	"gopkg.in/yaml.v1"
)

const DefaultHeader = "#cloud-config"

var ValidFileHeaders = []string{
	"#cloud-config",
	"#kairos-config",
	"#node-config",
}

type Options struct {
	ScanDir          []string
	BootCMDLineFile  string
	MergeBootCMDLine bool
	NoLogs           bool
}

type Option func(o *Options) error

func (o *Options) Apply(opts ...Option) error {
	for _, oo := range opts {
		if err := oo(o); err != nil {
			return err
		}
	}
	return nil
}

// SoftErr prints a warning if err is no nil and NoLogs is not true.
// It's use to wrap the same handling happening in multiple places.
//
// TODO: Switch to a standard logging library (e.g. verbose, silent mode etc)
func (o *Options) SoftErr(err error) {
	if !o.NoLogs && err != nil {
		fmt.Printf("WARNING: %s\n", err.Error())
	}
}

// We don't allow yamls that are plain arrays because is has no use in Kairos
// and there is no way to merge an array yaml with a "map" yaml.
type Config map[string]interface{}

// MergeConfigURL looks for the "config_url" key and if it's found
// it downloads the remote config and merges it with the current one.
// If the remote config also has config_url defined, it is also fetched
// recursively until a remote config no longer defines a config_url.
// NOTE: The "config_url" value of the final result is the value of the last
// config file in the chain because we replace values when we merge.
func (c *Config) MergeConfigURL() error {
	// If there is no config_url, just return (do nothing)
	configURL := c.ConfigURL()
	if configURL == "" {
		return nil
	}

	// fetch the remote config
	remoteConfig, err := fetchRemoteConfig(configURL)
	if err != nil {
		return err
	}

	// recursively fetch remote configs
	if err := remoteConfig.MergeConfigURL(); err != nil {
		return err
	}

	// merge remoteConfig back to "c"
	return c.MergeConfig(remoteConfig)
}

// MergeConfig merges the config passed as parameter back to the receiver Config.
func (c *Config) MergeConfig(newConfig *Config) error {
	return mergo.Merge(c, newConfig, func(c *mergo.Config) { c.Overwrite = true })
}

func Scan(opts ...Option) (*Config, error) {
	result := &Config{}
	o := &Options{}
	if err := o.Apply(opts...); err != nil {
		return result, err
	}

	result = parseFiles(o.ScanDir, o.NoLogs)

	if o.MergeBootCMDLine {
		d, err := DotToYAML(o.BootCMDLineFile)
		o.SoftErr(fmt.Errorf("parsing cmdline: %w", err))
		if err == nil { // best-effort
			var newYaml Config
			err = yaml.Unmarshal(d, &newYaml)
			o.SoftErr(fmt.Errorf("parsing cmdline as yaml: %w", err))

			err := mergo.Merge(result, newYaml)
			o.SoftErr(fmt.Errorf("merging config: %w", err))
		}
	}

	// TODO: This is too late, maybe a config_url has already been overwritten
	// by previous merges.
	// For every new "source", we should iterate and merge remote configs.
	// NOTE: It should not replace the original config_url in `result`, only
	// the rest of the config should be merged.
	result.MergeConfigURL()

	return result, nil
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

// parseFiles merges all config back in one structure.
func parseFiles(dir []string, nologs bool) *Config {
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

			var newYaml Config
			err = yaml.Unmarshal(b, &newYaml)
			if err != nil && !nologs {
				fmt.Printf("warning: failed to parse config:\n%s\n", err.Error())
			}
			if err := mergo.Merge(&c, newYaml); err != nil {
				fmt.Printf("warning: failed to merge config:\n%s\n", err.Error())
			}
		} else {
			if !nologs {
				fmt.Printf("warning: skipping %s (extension).\n", f)
			}
		}
	}

	return c
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

// DotToYAML reads options from the kernel cmdline and returns an
// equivalent YAML.
func DotToYAML(file string) ([]byte, error) {
	if file == "" {
		file = "/proc/cmdline"
	}
	dat, err := os.ReadFile(file)
	if err != nil {
		return []byte{}, err
	}

	v := stringToMap(string(dat))

	// TODO: Stick to map[string]interface{}? No need to call ToYAML.
	return unstructured.ToYAML(v)
}

func stringToMap(s string) map[string]interface{} {
	v := map[string]interface{}{}

	splitted, _ := shlex.Split(s)
	for _, item := range splitted {
		parts := strings.SplitN(item, "=", 2)
		value := "true"
		if len(parts) > 1 {
			value = strings.Trim(parts[1], `"`)
		}
		key := strings.Trim(parts[0], `"`)
		v[key] = value
	}

	return v
}

// ConfigURL returns the value of config_url if set or empty string otherwise.
func (c Config) ConfigURL() string {
	if val, hasKey := c["config_url"]; hasKey {
		if s, isString := val.(string); isString {
			return s
		}
	}

	return ""
}

func fetchRemoteConfig(url string) (*Config, error) {
	var body []byte
	result := &Config{}

	err := retry.Do(
		func() error {
			resp, err := http.Get(url)
			if err != nil {
				return err
			}
			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("unexpected status: %d", resp.StatusCode)
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
		return result, fmt.Errorf("could not fetch remote config: %w", err)
	}

	if err := yaml.Unmarshal(body, result); err != nil {
		return result, fmt.Errorf("could not unmarshal remote config to an object: %w", err)
	}

	// TODO: Filter by header. Ignore if header is not a "valid" one
	// Look in "ValidFileHeaders"
	// if exists, header := HasHeader(string(body), ""); exists {
	// 	result.Header = header
	// }

	return result, nil
}

func HasHeader(userdata, head string) (bool, string) {
	header := strings.SplitN(userdata, "\n", 2)[0]

	// Trim trailing whitespaces
	header = strings.TrimRightFunc(header, unicode.IsSpace)

	if head != "" {
		return head == header, header
	}
	return (header == DefaultHeader) || (header == "#kairos-config") || (header == "#node-config"), header
}
