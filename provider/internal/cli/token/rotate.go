package token

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kairos-io/kairos/v4/agent/pkg/config"
	"github.com/kairos-io/kairos/v4/sdk/collector"
	"github.com/kairos-io/kairos/v4/sdk/unstructured"
	"github.com/kairos-io/provider-kairos/v2/internal/provider"
	providerConfig "github.com/kairos-io/provider-kairos/v2/internal/provider/config"
	"github.com/kairos-io/provider-kairos/v2/internal/services"
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

	err = provider.SetupVPN(services.EdgeVPNDefaultInstance, apiAddress, rootDir, false, providerCfg)
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

// FindYAMLWithKey will find and return files that contain a given key in them.
func FindYAMLWithKey(s string, opts ...collector.Option) ([]string, error) {
	o := &collector.Options{}

	var result []string
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

func allFiles(dir []string) []string {
	var files []string
	for _, d := range dir {
		if f, err := listFiles(d); err == nil {
			files = append(files, f...)
		}
	}
	return files
}

func listFiles(dir string) ([]string, error) {
	var content []string

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
