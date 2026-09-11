// Package collector can be used to merge configuration from different
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

	"github.com/kairos-io/kairos/v4/sdk/machine"
	"github.com/kairos-io/kairos/v4/sdk/retry"

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

type ConfigValues map[string]interface{}

// We don't allow yamls that are plain arrays because is has no use in Kairos
// and there is no way to merge an array yaml with a "map" yaml.
type Config struct {
	Sources []string
	Values  ConfigValues
}

// MergeConfigURL looks for the "config_url" key and if it's found
// it downloads the remote config and merges it with the current one.
// If the remote config also has config_url defined, it is also fetched
// recursively until a remote config no longer defines a config_url.
// NOTE: The "config_url" value of the final result is the value of the last
// config file in the chain because we replace values when we merge.
// NOTE: a remote body without one of ValidFileHeaders, and a fetch that fails
// after its retries, are both silently dropped: fetchRemoteConfig returns an
// empty config and a nil error. Both are marked TODO there.
func (c *Config) MergeConfigURL() error {
	// If there is no config_url, just return (do nothing)
	configURL := c.ConfigURL()
	if configURL == "" {
		return nil
	}

	// Render {{ .Values.* }} template markers in the URL before the fetch so
	// a static config_url can carry per-machine identifiers. Recursion below
	// re-invokes MergeConfigURL on each downloaded hop, so a fetched config
	// that itself declares a templated config_url gets rendered next pass.
	rendered, err := RenderConfigURL(configURL)
	if err != nil {
		return fmt.Errorf("rendering config_url template: %w", err)
	}

	// fetch the remote config
	remoteConfig, err := fetchRemoteConfig(rendered)
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

func (c *Config) valuesCopy() (ConfigValues, error) {
	var result ConfigValues
	data, err := yaml.Marshal(c.Values)
	if err != nil {
		return result, err
	}

	err = yaml.Unmarshal(data, &result)

	return result, err
}

// MergeConfig merges the config passed as parameter back to the receiver Config.
func (c *Config) MergeConfig(newConfig *Config) error {
	var err error

	aMap, err := c.valuesCopy()
	if err != nil {
		return err
	}
	bMap, err := newConfig.valuesCopy()
	if err != nil {
		return err
	}

	// TODO: Consider removing the `name:` key because in the end we end up with the
	// value from the last config merged. Ideally we should display the name in the "sources"
	// comment next to the file but doing it here is not possible because the configs
	// passed, could already be results of various merged thus we don't know which of
	// the "sources" should take the "name" next to it.
	//
	// if _, exists := bMap.Values["name"]; exists {
	// 	delete(bMap.Values, "name")
	// }

	// deep merge the two maps
	mergedValues, err := DeepMerge(aMap, bMap)
	if err != nil {
		return err
	}
	finalConfig := Config{}
	finalConfig.Sources = append(c.Sources, newConfig.Sources...)
	finalConfig.Values = mergedValues.(ConfigValues)

	*c = finalConfig

	return nil
}

func mergeSlices(sliceA, sliceB []interface{}) ([]interface{}, error) {
	// return sliceB if sliceA is empty
	if len(sliceA) == 0 {
		return sliceB, nil
	}
	// We use the first item in the slice to determine if there are maps present.
	firstItem := sliceA[0]
	// If the first item is a map, we concatenate both slices
	if reflect.ValueOf(firstItem).Kind() == reflect.Map {
		union := append(sliceA, sliceB...)

		return union, nil
	}

	// For any other type, we check if the every item in sliceB is already present in sliceA and if not, we add it.
	// Implementation for 1.20:
	// for _, v := range sliceB {
	// 	i := slices.Index(sliceA, v)
	// 	if i < 0 {
	// 		sliceA = append(sliceA, v)
	// 	}
	// }
	// This implementation is needed because Go 1.19 does not implement compare for {}interface. Once
	// FIPS can be upgraded to 1.20, we should be able to use the code above instead.
	for _, vB := range sliceB {
		found := false
		for _, vA := range sliceA {
			if vA == vB {
				found = true
			}
		}

		if !found {
			sliceA = append(sliceA, vB)
		}
	}

	return sliceA, nil
}

func deepMergeMaps(a, b ConfigValues) (ConfigValues, error) {
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

	// if b is null value, return null-value of whatever a currently is
	if b == nil {
		if typeA.Kind() == reflect.Slice {
			return reflect.MakeSlice(typeA, 0, 0).Interface(), nil
		} else if typeA.Kind() == reflect.Map {
			return reflect.MakeMap(typeA).Interface(), nil
		}
		return reflect.Zero(typeA).Interface(), nil
	}

	// We don't support merging different data structures
	if typeA.Kind() != typeB.Kind() {
		return ConfigValues{}, fmt.Errorf("cannot merge %s with %s", typeA.String(), typeB.String())
	}

	if typeA.Kind() == reflect.Slice {
		return mergeSlices(a.([]interface{}), b.([]interface{}))
	}

	if typeA.Kind() == reflect.Map {
		return deepMergeMaps(a.(ConfigValues), b.(ConfigValues))
	}

	// for any other type, b should take precedence
	return b, nil
}

// String returns a string which is a Yaml representation of the Config.
func (c *Config) String() (string, error) {
	sourcesComment := ""
	config := *c
	if len(config.Sources) > 0 {
		sourcesComment = "# Sources:\n"
		for _, s := range config.Sources {
			sourcesComment += fmt.Sprintf("# - %s\n", s)
		}
		sourcesComment += "\n"
	}

	data, err := yaml.Marshal(config.Values)
	if err != nil {
		return "", fmt.Errorf("marshalling the config to a string: %s", err)
	}

	return fmt.Sprintf("%s\n\n%s%s", DefaultHeader, sourcesComment, string(data)), nil
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
	configs = append(configs, parseReaders(o.Readers, o.NoLogs)...)

	if o.MergeBootCMDLine {
		cConfig, err := ParseCmdLine(o.BootCMDLineFile, filter)
		o.SoftErr("parsing cmdline", err)
		if err == nil { // best-effort
			configs = append(configs, cConfig)
		}
	}

	mergedConfig, err := configs.Merge()
	if err != nil {
		return mergedConfig, err
	}

	if o.Overwrites != "" {
		yaml.Unmarshal([]byte(o.Overwrites), &mergedConfig.Values) //nolint:errcheck
	}

	return mergedConfig, nil
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

// maxConfigFileSize is the size at which a candidate config file is skipped
// rather than parsed. It is the threshold the collector has enforced since
// the check was written: the original code compared truncating integer
// megabytes against 1.0, so it skipped a file only from 2MiB up, despite
// saying "1MB". Stating it in bytes makes the number honest without changing
// which files load; moving the threshold itself is a separate decision.
const maxConfigFileSize = 2 * 1024 * 1024

// parseFiles returns a list of Configs parsed from files.
func parseFiles(dir []string, nologs bool) Configs {
	result := Configs{}
	files := allFiles(dir)
	for _, f := range files {
		// Check the extension before the size. A scanned directory holds far
		// more than configs (kairos-io/kairos#2064: EFI binaries, grub
		// modules, kernels), and reporting those as oversized configs buries
		// the one report that matters below.
		//
		// Skip them silently. A non-yaml file in a scanned directory is never
		// actionable for the user, so a warning per file is pure noise: an
		// upgrade on live media printed hundreds of them, one for every grub
		// module and EFI binary under /run/initramfs/live.
		if ext := filepath.Ext(f); ext != ".yml" && ext != ".yaml" {
			continue
		}

		// A file the user named like a config and put in a scanned directory,
		// dropped for its size, is silent data loss: the machine comes up with
		// none of those settings and nothing on the console says why. Report
		// it whatever nologs says (kairos-io/kairos#1275). It goes to stderr
		// so `kairos-agent config` and `config get` stay pipeable.
		if size, err := fileSize(f); err == nil && size >= maxConfigFileSize {
			fmt.Fprintf(os.Stderr,
				"warning: skipping %s: it is %d bytes and the limit for a single config file is %d bytes, so none of its settings were applied. Split it up, or serve it with config_url.\n",
				f, size, maxConfigFileSize)
			continue
		}

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
		err = yaml.Unmarshal(b, &newConfig.Values)
		if err != nil && !nologs {
			fmt.Printf("warning: failed to parse config:\n%s\n", err.Error())
		}
		newConfig.Sources = []string{f}

		result = append(result, &newConfig)
	}

	return result
}

// parseReaders returns a list of Configs parsed from Reader interfaces
// We assume as this has been passed explicitly to the collector that the
// checks for it being a config is already done, so no header checks here.
func parseReaders(readers []io.Reader, nologs bool) Configs {
	result := Configs{}
	for _, R := range readers {
		var newConfig Config
		read, err := io.ReadAll(R)
		if err != nil {
			if !nologs {
				fmt.Printf("Error reading config: %s", err.Error())
			}
			continue
		}
		err = yaml.Unmarshal(read, &newConfig.Values)
		if err != nil {
			err = json.Unmarshal(read, &newConfig.Values)
			if err != nil {
				if !nologs {
					fmt.Printf("Error unmarshalling config(error: %s): %s", err.Error(), string(read))
				}
				continue
			}
		}
		newConfig.Sources = []string{"reader"}
		result = append(result, &newConfig)
	}

	return result
}

// fileSize returns the size of f in bytes. An error means the caller could not
// learn the size, which is not the same as the file being empty: it must fall
// through and let the read report the real problem, rather than treat an
// unreadable file as one that fits.
func fileSize(f string) (int64, error) {
	stat, err := os.Stat(f)
	if err != nil {
		return 0, err
	}

	return stat.Size(), nil
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

// ParseCmdLine reads the kernel cmdline (defaults to /proc/cmdline when
// file is empty) and returns a single Config that merges two token
// families in one pass:
//
//   - Kairos-owned prefixes (kairos.config=, kairos.config_url=,
//     cos.setup=) go through machine.KairosCmdlineYAMLFromString, which
//     supports dot notation with numeric segments for list indices, a
//     dedicated URL entrypoint (kairos.config_url=URL) and the legacy
//     cos.setup= alias. Values here bypass the caller-supplied filter
//     because they have dedicated semantics, not a schema-driven shape.
//   - Every other KEY=VALUE token is fed to machine.DotStringToYAML
//     (which skips the Kairos-owned prefixes) and then handed to the
//     caller-supplied filter so unrelated kernel args are dropped.
//
// Both partial YAMLs are unmarshalled and deep-merged; Kairos-owned
// values win on collision so a kairos.config= stanza cannot be shadowed
// by a stray generic token with the same key. Call MergeConfigURL on
// the result to recursively pull any remote config referenced through
// config_url.
func ParseCmdLine(file string, filter func(d []byte) ([]byte, error)) (*Config, error) {
	result := &Config{Sources: []string{"cmdline"}, Values: ConfigValues{}}

	if file == "" {
		file = "/proc/cmdline"
	}
	dat, err := os.ReadFile(file)
	if err != nil {
		return result, err
	}
	s := string(dat)

	genericYAML, err := machine.DotStringToYAML(s)
	if err != nil {
		return result, err
	}
	if len(genericYAML) > 0 {
		filteredYAML, err := filter(genericYAML)
		if err != nil {
			return result, err
		}
		if err := yaml.Unmarshal(filteredYAML, &result.Values); err != nil {
			return result, err
		}
	}

	kairosYAML, err := machine.KairosCmdlineYAMLFromString(s)
	if err != nil {
		return result, err
	}
	if len(kairosYAML) > 0 {
		kairosVals := ConfigValues{}
		if err := yaml.Unmarshal(kairosYAML, &kairosVals); err != nil {
			return result, err
		}
		if err := result.MergeConfig(&Config{Values: kairosVals}); err != nil {
			return result, err
		}
	}

	return result, nil
}

// ConfigURL returns the value of config_url if set or empty string otherwise.
func (c Config) ConfigURL() string {
	if val, hasKey := c.Values["config_url"]; hasKey {
		if s, isString := val.(string); isString {
			return s
		}
	}

	return ""
}

func fetchRemoteConfig(url string) (*Config, error) {
	var body []byte
	result := &Config{}

	// Per-attempt HTTP timeout. http.DefaultClient's zero-value Timeout
	// means a stalled TCP connect (no route to the target during the fs
	// cloud-init stage, which runs before network-online.target) can
	// hang for the kernel's SYN-retry window -- ~2 minutes per attempt,
	// multiplied by the retry count below -- and drag cos-setup-fs.service
	// with it. cos-setup-fs is ordered Before=sysinit.target, so hanging
	// it blocks every later target including multi-user.target, and sshd
	// never comes up. A bounded per-request timeout keeps the whole
	// retry loop below a minute of wall-clock even in the worst case.
	client := &http.Client{Timeout: 15 * time.Second}
	err := retry.Do(
		func() error {
			resp, err := client.Get(url)
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
		retry.Config{Attempts: 3, Delay: retry.Fixed(time.Second)},
	)

	if err != nil {
		// TODO: This keeps the old behaviour but IMHO we should return an error here
		return result, nil
	}

	if !HasValidHeader(string(body)) {
		// TODO: This keeps the old behaviour but IMHO we should return an error here
		return result, nil
	}

	if err := yaml.Unmarshal(body, &result.Values); err != nil {
		return result, fmt.Errorf("could not unmarshal remote config to an object: %w", err)
	}

	result.Sources = []string{url}

	return result, nil
}

func HasValidHeader(data string) bool {
	// Get the first 10 lines
	headers := strings.SplitN(data, "\n", 10)

	// iterate over them as there could be comments or the jinja template info:
	// https://cloudinit.readthedocs.io/en/latest/explanation/instancedata.html#example-cloud-config-with-instance-data

	for _, line := range headers {
		// Trim trailing whitespaces
		header := strings.TrimRightFunc(line, unicode.IsSpace)
		// If it starts with a hash check it, in case its a huge line, we dont want to waste time
		if strings.HasPrefix(header, "#") {
			// NOTE: we also allow "legacy" headers. Should only allow #cloud-config at
			// some point.
			if (header == DefaultHeader) || (header == "#kairos-config") || (header == "#node-config") {
				return true
			}
		}
	}

	return false
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
	// Adding some jq options to the query so the output does not include "null" if the value is empty
	// This is not a json parse feature but a string one, so we should return normal values not json specific ones
	query, err := gojq.Parse(s + " | if ( . | type) == \"null\" then empty else . end")
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
