package token

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"github.com/kairos-io/kairos/v4/agent/pkg/config"
	"github.com/kairos-io/kairos/v4/provider/internal/provider"
	providerConfig "github.com/kairos-io/kairos/v4/provider/internal/provider/config"
	"github.com/kairos-io/kairos/v4/provider/internal/services"
	"github.com/kairos-io/kairos/v4/sdk/collector"
	loggerpkg "github.com/kairos-io/kairos/v4/sdk/types/logger"
	"github.com/kairos-io/kairos/v4/sdk/unstructured"
	"gopkg.in/yaml.v3"
)

func RotateToken(configDir []string, newToken, apiAddress, rootDir string, restart bool) error {
	if err := ReplaceToken(configDir, newToken); err != nil {
		return err
	}

	o := &collector.Options{}
	if err := o.Apply(collector.Directories(configDir...)); err != nil {
		return err
	}
	c, err := collector.Scan(o, config.FilterKeys)

	if err != nil {
		return err
	}

	providerCfg := &providerConfig.Config{}
	a, _ := c.String()
	err = yaml.Unmarshal([]byte(a), providerCfg)
	if err != nil {
		return err
	}

	// SetupVPN reports a best-effort failure (the local DNS apply) through
	// this logger rather than stdout, so the plugin path cannot corrupt its
	// go-pluggable JSON response.
	logger := loggerpkg.NewKairosLogger("provider", "info", false)

	err = provider.SetupVPN(logger, services.EdgeVPNDefaultInstance, apiAddress, rootDir, false, providerCfg)
	if err != nil {
		return err
	}

	if restart {
		svc, err := services.EdgeVPN(services.EdgeVPNDefaultInstance, rootDir)
		if err != nil {
			return err
		}

		return svc.Restart()
	}
	return nil
}

func ReplaceToken(dir []string, token string) (err error) {
	locations, err := FindYAMLWithKey("p2p.network_token", collector.Directories(dir...))
	if err != nil {
		return err
	}
	for _, f := range locations {
		dat, err := os.ReadFile(f)
		if err != nil {
			fmt.Printf("warning: could not read %s '%s'\n", f, err.Error())
			continue
		}

		var doc yaml.Node
		if err := yaml.Unmarshal(dat, &doc); err != nil {
			return err
		}

		if err := setNetworkToken(&doc, token); err != nil {
			return err
		}

		out, err := yaml.Marshal(&doc)
		if err != nil {
			return err
		}

		fi, err := os.Stat(f)
		if err != nil {
			return err
		}

		if err := os.WriteFile(f, out, fi.Mode().Perm()); err != nil {
			return err
		}
	}

	return nil
}

func setNetworkToken(doc *yaml.Node, token string) error {
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return errors.New("invalid YAML document")
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return errors.New("root of config is not a YAML mapping")
	}

	p2p := mappingValue(root, "p2p")
	if p2p == nil {
		return errors.New("no p2p section in config file")
	}
	if p2p.Kind != yaml.MappingNode {
		return errors.New("p2p section is not a YAML mapping")
	}

	if t := mappingValue(p2p, "network_token"); t != nil {
		t.Value = token
		return nil
	}

	p2p.Content = append(p2p.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: "network_token"},
		&yaml.Node{Kind: yaml.ScalarNode, Value: token, Style: yaml.DoubleQuotedStyle},
	)
	return nil
}

func mappingValue(m *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

// maxConfigFileSize is the size above which a candidate is skipped rather than
// read. It is the bound sdk/collector already puts on a config file, repeated
// here because this package walks the same directories.
const maxConfigFileSize = 2 * 1024 * 1024

// FindYAMLWithKey will find and return files that contain a given key in them.
func FindYAMLWithKey(s string, opts ...collector.Option) ([]string, error) {
	o := &collector.Options{}

	var result []string
	if err := o.Apply(opts...); err != nil {
		return result, err
	}

	for _, f := range allFiles(o.ScanDir) {
		dat, err := readConfigCandidate(f)
		if err != nil {
			fmt.Printf("warning: skipping file '%s' - %s\n", f, err.Error())
			continue
		}

		found, err := unstructured.YAMLHasKey(s, dat)
		if err != nil {
			fmt.Printf("warning: skipping file '%s' - %s\n", f, err.Error())
			continue
		}

		if found {
			result = append(result, f)
		}
	}

	return result, nil
}

// readConfigCandidate reads a config candidate and refuses anything that is not
// a regular file of a plausible size.
//
// os.ReadFile opens without O_NONBLOCK, and opening a FIFO for reading blocks in
// open(2) until a writer arrives. On a node none ever does, so rotate-token
// parks for good, after ReplaceToken has rewritten the token in the config files
// and before writeEdgeVPNEnv has rewritten the edgevpn unit. O_NONBLOCK makes
// the open return whatever the path turns out to be, so the fstat that follows
// can reject it. A character device is the same shape of problem, and /dev/zero
// reads until memory runs out, which the size bound catches as well.
//
// O_NOFOLLOW is deliberately not set: a symlink to a real config is a layout
// Kairos supports, and following it lands on the regular file the fstat wants.
//
// sdk/collector grew the same guard for its own scan in kairos-io/kairos#4869.
// The two want to be one helper once that has landed.
func readConfigCandidate(f string) ([]byte, error) {
	file, err := os.OpenFile(f, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		return nil, err
	}

	if !stat.Mode().IsRegular() {
		return nil, errors.New("it is not a regular file")
	}

	if stat.Size() > maxConfigFileSize {
		return nil, fmt.Errorf("it is %d bytes, above the %d the collector reads", stat.Size(), maxConfigFileSize)
	}

	return io.ReadAll(file)
}

func allFiles(dir []string) []string {
	var files []string
	for _, d := range dir {
		if f, err := listFiles(d); err == nil {
			files = append(files, f...)
		}
	}
	return files
}

// listFiles returns the config candidates under dir, which are the .yml and
// .yaml files only. The walk used to return every file it saw, so rotate-token
// opened and read the whole of whatever a scanned directory happened to hold:
// the EFI binaries, grub modules and kernels of kairos-io/kairos#2064 when it
// is pointed at the live media, and a FIFO under any name at all.
func listFiles(dir string) ([]string, error) {
	var content []string

	err := filepath.Walk(dir,
		func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if info.IsDir() {
				return nil
			}
			if ext := filepath.Ext(path); ext != ".yml" && ext != ".yaml" {
				return nil
			}

			content = append(content, path)

			return nil
		})

	return content, err
}
