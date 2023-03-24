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
	"time"
	"unicode"

	"github.com/avast/retry-go"
	"github.com/google/shlex"
	"github.com/imdario/mergo"
	"github.com/itchyny/gojq"
	"github.com/kairos-io/kairos-sdk/unstructured"
	"gopkg.in/yaml.v1"
)

const DefaultHeader = "#cloud-config"

var ValidFileHeaders = []string{
	"#cloud-config",
	"#kairos-config",
	"#node-config",
}

type Configs []*Config

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

// String returns a string which is a Yaml representation of the Config.
func (c *Config) String() (string, error) {
	data, err := yaml.Marshal(c)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%s\n\n%s", DefaultHeader, string(data)), nil
}

func (cs Configs) Merge() (*Config, error) {
	result := &Config{}

	for _, c := range cs {
		if err := c.MergeConfigURL(); err != nil {
			return result, err
		}

		if err := result.MergeConfig(c); err != nil {
			return result, err
		}
	}

	return result, nil
}

func Scan(o *Options) (*Config, error) {
	configs := Configs{}

	configs = append(configs, parseFiles(o.ScanDir, o.NoLogs)...)

	if o.MergeBootCMDLine {
		cConfig, err := ParseCmdLine(o.BootCMDLineFile)
		o.SoftErr("parsing cmdline", err)
		if err == nil { // best-effort
			configs = append(configs, cConfig)
		}
	}

	return configs.Merge()
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

// parseFiles returns a list of Configs parsed from files.
func parseFiles(dir []string, nologs bool) Configs {
	result := Configs{}
	files := allFiles(dir)
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

			if !HasValidHeader(string(b)) {
				if !nologs {
					fmt.Printf("warning: skipping %s because it has no valid header\n", f)
				}
				continue
			}

			var newConfig Config
			err = yaml.Unmarshal(b, &newConfig)
			if err != nil && !nologs {
				fmt.Printf("warning: failed to parse config:\n%s\n", err.Error())
			}
			result = append(result, &newConfig)
		} else {
			if !nologs {
				fmt.Printf("warning: skipping %s (extension).\n", f)
			}
		}
	}

	return result
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

// ParseCmdLine reads options from the kernel cmdline and returns the equivalent
// Config.
func ParseCmdLine(file string) (*Config, error) {
	result := &Config{}

	if file == "" {
		file = "/proc/cmdline"
	}
	dat, err := os.ReadFile(file)
	if err != nil {
		return result, err
	}

	d, err := unstructured.ToYAML(stringToConfig(string(dat)))
	if err != nil {
		return result, err
	}
	err = yaml.Unmarshal(d, &result)

	return result, err
}

func stringToConfig(s string) Config {
	v := Config{}

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
		}, retry.Delay(time.Second), retry.Attempts(3),
	)

	if err != nil {
		// TODO: improve logging
		fmt.Println("could not fetch remote config: %w", err)
		return result, nil
	}

	if !HasValidHeader(string(body)) {
		// TODO: Print a warning when we implement proper logging
		fmt.Println("No valid header in remote config: %w", err)
		return result, nil
	}

	if err := yaml.Unmarshal(body, result); err != nil {
		return result, fmt.Errorf("could not unmarshal remote config to an object: %w", err)
	}

	return result, nil
}

func HasValidHeader(data string) bool {
	header := strings.SplitN(data, "\n", 2)[0]

	// Trim trailing whitespaces
	header = strings.TrimRightFunc(header, unicode.IsSpace)

	// NOTE: we also allow "legacy" headers. Should only allow #cloud-config at
	// some point.
	return (header == DefaultHeader) || (header == "#kairos-config") || (header == "#node-config")
}

func (c Config) Query(s string) (res string, err error) {
	s = fmt.Sprintf(".%s", s)

	var dat map[string]interface{}

	yamlStr, err := c.String()
	if err != nil {
		panic(err)
	}
	if err := yaml.Unmarshal([]byte(yamlStr), &dat); err != nil {
		panic(err)
	}

	query, err := gojq.Parse(s)
	if err != nil {
		return res, err
	}

	iter := query.Run(dat) // or query.RunWithContext
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

// TODO check if doing the right thing ... also checking remote files and cmdline.
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
