// Package extensions holds the configuration types for system and
// configuration extensions, kept next to the other config types so providers
// and the agent can consume them without an import cycle.
package extensions

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// DefaultCatalogURL is the extension index a name is resolved against when the
// cloud config names no catalog of its own.
const DefaultCatalogURL = "https://kairos-io.github.io/hadron-layers/releases.json"

// Config is the node-wide extension configuration, set under the top level
// `extensions` key of a Kairos cloud config.
type Config struct {
	// Catalogs lists the extension indexes a bare extension name is resolved
	// against, searched in order. An empty list means the built-in default.
	Catalogs []string `yaml:"catalogs,omitempty" mapstructure:"catalogs" json:"catalogs,omitempty"`

	// IgnoreSignatures merges extensions that carry no signature systemd can
	// verify. It is off by default: under Trusted Boot an unsigned extension
	// is refused, which is the point of Trusted Boot.
	IgnoreSignatures bool `yaml:"ignore_signatures,omitempty" mapstructure:"ignore_signatures" json:"ignore_signatures,omitempty"`
}

// CatalogURLs returns the indexes to resolve names against: the ones the
// cloud config lists, or the built-in default when it lists none.
func (c Config) CatalogURLs() []string {
	if len(c.Catalogs) > 0 {
		return c.Catalogs
	}
	return []string{DefaultCatalogURL}
}

// Extension is one requested extension. It is either a name to look up in the
// configured catalogs, optionally with a version, or a URI or absolute path to
// an extension image, in which case Version has no meaning.
//
// In a cloud config it is written either as a plain string
//
//	extensions:
//	  - fwupd
//	  - fwupd@2.1.7
//	  - oci://ghcr.io/example/tools.sysext.raw
//	  - /run/initramfs/live/tools.sysext.raw
//
// or as a mapping, which is the only way to write a version constraint that
// contains a space:
//
//	extensions:
//	  - name: fwupd
//	    version: ">= 2.1, < 3"
type Extension struct {
	// Name is a catalog name, a URI or an absolute path.
	Name string `yaml:"name,omitempty" mapstructure:"name" json:"name,omitempty"`

	// Version is an exact catalog version or a semver constraint. It is only
	// meaningful for a catalog name; the empty string means the newest
	// version the catalog publishes.
	Version string `yaml:"version,omitempty" mapstructure:"version" json:"version,omitempty"`
}

// Extensions is a list of requested extensions.
type Extensions []Extension

// ParseExtension reads the shorthand string form of an extension. A name may
// carry a version after an `@`; a URI or a path may not, since `@` is part of
// a digest-pinned OCI reference and of nothing else in a path.
func ParseExtension(value string) (Extension, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return Extension{}, fmt.Errorf("extension may not be empty")
	}
	extension := Extension{Name: value}
	if extension.IsReference() {
		return extension, nil
	}
	if name, version, found := strings.Cut(value, "@"); found {
		if name == "" || version == "" {
			return Extension{}, fmt.Errorf("extension %q must be written as name@version", value)
		}
		extension = Extension{Name: name, Version: version}
	}
	return extension, nil
}

// IsReference reports whether the extension names an image directly, by URI or
// by path, rather than a name to resolve against the catalogs.
func (e Extension) IsReference() bool {
	if strings.HasPrefix(e.Name, "/") {
		return true
	}
	scheme, _, found := strings.Cut(e.Name, "://")
	if !found {
		// `oci:ghcr.io/...` is accepted by the URI parser too.
		scheme, _, found = strings.Cut(e.Name, ":")
	}
	if !found {
		return false
	}
	switch scheme {
	case "oci", "docker", "container", "file", "http", "https":
		return true
	default:
		return false
	}
}

// String renders the extension back into its shorthand form.
func (e Extension) String() string {
	if e.Version == "" {
		return e.Name
	}
	return e.Name + "@" + e.Version
}

// UnmarshalYAML accepts both the shorthand string and the mapping form.
func (e *Extension) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		var value string
		if err := node.Decode(&value); err != nil {
			return err
		}
		parsed, err := ParseExtension(value)
		if err != nil {
			return err
		}
		*e = parsed
		return nil
	}

	// A distinct type, so decoding the mapping does not call this method again.
	var mapping struct {
		Name    string `yaml:"name"`
		Version string `yaml:"version"`
	}
	if err := node.Decode(&mapping); err != nil {
		return err
	}
	if mapping.Name == "" {
		return fmt.Errorf("extension is missing its name")
	}
	if mapping.Version != "" && (Extension{Name: mapping.Name}).IsReference() {
		return fmt.Errorf("extension %q is a reference to one image, it cannot also take a version", mapping.Name)
	}
	*e = Extension{Name: mapping.Name, Version: mapping.Version}
	return nil
}

// MarshalYAML writes the shorthand form when it round-trips through
// ParseExtension, and the mapping form otherwise: a constraint like
// ">= 2.1, < 3" cannot be appended with an `@` and read back.
func (e Extension) MarshalYAML() (interface{}, error) {
	if e.Version == "" {
		return e.Name, nil
	}
	if !strings.ContainsAny(e.Version, " ,@") {
		return e.String(), nil
	}
	return struct {
		Name    string `yaml:"name"`
		Version string `yaml:"version"`
	}{e.Name, e.Version}, nil
}
