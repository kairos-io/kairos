// Package configcollector can be used to merge configuration from different
// sources into one YAML.
package collector

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"
	"unicode"

	"golang.org/x/exp/slices"

	"github.com/kairos-io/kairos-sdk/machine"

	"github.com/avast/retry-go"
	"github.com/itchyny/gojq"
	"gopkg.in/yaml.v3"
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

func (c *Config) toMap() (map[string]interface{}, error) {
	var result map[string]interface{}
	data, err := yaml.Marshal(c)
	if err != nil {
		return result, err
	}

	err = yaml.Unmarshal(data, &result)
	return result, err
}

func (c *Config) applyMap(i interface{}) error {
	data, err := yaml.Marshal(i)
	if err != nil {
		return err
	}

	err = yaml.Unmarshal(data, c)
	return err
}

// MergeConfig merges the config passed as parameter back to the receiver Config.
func (c *Config) MergeConfig(newConfig *Config) error {
	var err error

	// convert the two configs into maps
	aMap, err := c.toMap()
	if err != nil {
		return err
	}
	bMap, err := newConfig.toMap()
	if err != nil {
		return err
	}

	// deep merge the two maps
	cMap, err := DeepMerge(aMap, bMap)
	if err != nil {
		return err
	}

	// apply the result of the deepmerge into the base config
	return c.applyMap(cMap)
}

func deepMergeSlices(sliceA, sliceB []interface{}) ([]interface{}, error) {
	// We use the first item in the slice to determine if there are maps present.
	// Do we need to do the same for other types?
	firstItem := sliceA[0]
	if reflect.ValueOf(firstItem).Kind() == reflect.Map {
		temp := make(map[string]interface{})

		// first we put in temp all the keys present in a, and assign them their existing values
		for _, item := range sliceA {
			for k, v := range item.(map[string]interface{}) {
				temp[k] = v
			}
		}

		// then we go through b to merge each of its keys
		for _, item := range sliceB {
			for k, v := range item.(map[string]interface{}) {
				current, ok := temp[k]
				if ok {
					// if the key exists, we deep merge it
					dm, err := DeepMerge(current, v)
					if err != nil {
						return []interface{}{}, fmt.Errorf("cannot merge %s with %s", current, v)
					}
					temp[k] = dm
				} else {
					// otherwise we just set it
					temp[k] = v
				}
			}
		}

		return []interface{}{temp}, nil
	}

	// for simple slices
	for _, v := range sliceB {
		i := slices.Index(sliceA, v)
		if i < 0 {
			sliceA = append(sliceA, v)
		}
	}

	return sliceA, nil
}

func deepMergeMaps(a, b map[string]interface{}) (map[string]interface{}, error) {
	// go through all items in b and merge them to a
	for k, v := range b {
		current, ok := a[k]
		if ok {
			// when the key is already set, we don't know what type it has, so we deep merge them in case they are maps
			// or slices
			res, err := DeepMerge(current, v)
			if err != nil {
				return a, err
			}
			a[k] = res
		} else {
			a[k] = v
		}
	}

	return a, nil
}

// DeepMerge takes two data structures and merges them together deeply. The results can vary depending on how the
// arguments are passed since structure B will always overwrite what's on A.
func DeepMerge(a, b interface{}) (interface{}, error) {
	if a == nil && b != nil {
		return b, nil
	}

	typeA := reflect.TypeOf(a)
	typeB := reflect.TypeOf(b)

	// We don't support merging different data structures
	if typeA.Kind() != typeB.Kind() {
		return map[string]interface{}{}, fmt.Errorf("cannot merge %s with %s", typeA.String(), typeB.String())
	}

	if typeA.Kind() == reflect.Slice {
		return deepMergeSlices(a.([]interface{}), b.([]interface{}))
	}

	if typeA.Kind() == reflect.Map {
		return deepMergeMaps(a.(map[string]interface{}), b.(map[string]interface{}))
	}

	// for any other type, b should take precedence
	return b, nil
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

func Scan(o *Options, filter func(d []byte) ([]byte, error)) (*Config, error) {
	configs := Configs{}

	configs = append(configs, parseFiles(o.ScanDir, o.NoLogs)...)

	if o.MergeBootCMDLine {
		cConfig, err := ParseCmdLine(o.BootCMDLineFile, filter)
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
func ParseCmdLine(file string, filter func(d []byte) ([]byte, error)) (*Config, error) {
	result := Config{}
	dotToYAML, err := machine.DotToYAML(file)
	if err != nil {
		return &result, err
	}

	filteredYAML, err := filter(dotToYAML)
	if err != nil {
		return &result, err
	}

	err = yaml.Unmarshal(filteredYAML, &result)
	if err != nil {
		return &result, err
	}

	return &result, nil
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
		fmt.Printf("WARNING: Couldn't fetch config_url: %s", err)
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
	var dat1 map[string]interface{}

	yamlStr, err := c.String()
	if err != nil {
		panic(err)
	}
	// Marshall it so it removes the first line which cannot be parsed
	err = yaml.Unmarshal([]byte(yamlStr), &dat1)
	if err != nil {
		panic(err)
	}
	// Transform it to json so its parsed correctly by gojq
	b, err := json.Marshal(dat1)
	if err != nil {
		panic(err)
	}
	if err := json.Unmarshal(b, &dat); err != nil {
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
