package imagesum

import (
	"fmt"
	"path/filepath"
	"strings"

	sdkFs "github.com/kairos-io/kairos/v4/sdk/types/fs"
	"gopkg.in/yaml.v3"
)

// Action is what immucore does with the checksum of the image it is about to mount.
type Action string

const (
	// None does not check anything and does not read the image. This is the default.
	None Action = "none"
	// Warn checks the image and logs every problem, then boots anyway.
	Warn Action = "warn"
	// Fail checks the image and stops the boot on a problem.
	Fail Action = "fail"
)

// ParseAction converts a config or cmdline value. The empty string is None.
// ok is false for anything that is not none, warn or fail.
func ParseAction(s string) (a Action, ok bool) {
	switch Action(strings.ToLower(strings.TrimSpace(s))) {
	case "", None:
		return None, true
	case Warn:
		return Warn, true
	case Fail:
		return Fail, true
	}
	return None, false
}

// Config is the `image-checksum` block of the cloud config. Every key can also
// be given on the kernel cmdline in dot notation, e.g. image-checksum.create=true,
// and the cmdline wins over the config.
type Config struct {
	// Create makes kairos-agent write a sha256 sidecar next to each OS image when
	// it installs, upgrades or resets. Off by default. When off, stale sidecars
	// are still removed so they never describe an image they do not belong to.
	//
	// It is a pointer so an explicit false survives the cmdline filter
	// (a round trip through the Config struct with omitempty), which drops a plain
	// false. Without that the cmdline could not turn a config's true off.
	Create *bool `yaml:"create,omitempty" mapstructure:"create" json:"create,omitempty"`
	// Verify is what immucore does with the checksum at boot: none, warn or fail.
	Verify Action `yaml:"verify,omitempty" mapstructure:"verify" json:"verify,omitempty"`
	// Require makes an image without a usable checksum (missing or stale) count
	// as a problem. Without it those images are skipped, which is what keeps
	// systems installed before this feature booting.
	// A pointer for the same reason as Create.
	Require *bool `yaml:"require,omitempty" mapstructure:"require" json:"require,omitempty"`
}

// CreateEnabled reports whether checksums are to be written. Off when unset.
func (c Config) CreateEnabled() bool { return c.Create != nil && *c.Create }

// RequireEnabled reports whether an image without a usable checksum is a problem. Off when unset.
func (c Config) RequireEnabled() bool { return c.Require != nil && *c.Require }

// Policy is the part of the Config immucore needs at boot.
func (c Config) Policy() Policy {
	return Policy{Verify: c.Verify, Require: c.RequireEnabled()}
}

// Policy is the boot time part of the configuration. Immucore runs before the
// cloud config is available (OEM is mounted after the root image), so the agent
// stores the policy next to the images, on the partition immucore mounts first.
type Policy struct {
	Verify  Action `yaml:"verify,omitempty"`
	Require bool   `yaml:"require,omitempty"`
}

// PolicyFile is the name of the policy file, stored in the same directory as the images.
const PolicyFile = "image-checksum.conf"

// Active reports whether the policy asks for any checking.
func (p Policy) Active() bool { return p.Verify == Warn || p.Verify == Fail }

// WritePolicy stores the policy in dir. A policy that asks for nothing removes
// the file instead, so the file only exists while there is something to enforce.
func WritePolicy(fs sdkFs.KairosFS, dir string, p Policy) error {
	path := filepath.Join(dir, PolicyFile)
	if !p.Active() && !p.Require {
		if err := fs.Remove(path); err != nil && !isNotExist(err) {
			return err
		}
		return nil
	}
	data, err := yaml.Marshal(p)
	if err != nil {
		return err
	}
	return writeAtomic(fs, path, data)
}

// ReadPolicy reads the policy stored in dir. found is false if there is none.
// An unparsable file or an unknown action is an error.
func ReadPolicy(fs sdkFs.KairosFS, dir string) (p Policy, found bool, err error) {
	path := filepath.Join(dir, PolicyFile)
	data, err := fs.ReadFile(path)
	if err != nil {
		if isNotExist(err) {
			return Policy{}, false, nil
		}
		return Policy{}, false, err
	}
	var raw struct {
		Verify  string `yaml:"verify"`
		Require bool   `yaml:"require"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return Policy{}, true, fmt.Errorf("parsing %s: %w", path, err)
	}
	a, ok := ParseAction(raw.Verify)
	if !ok {
		return Policy{}, true, fmt.Errorf("parsing %s: unknown verify action %q (want none, warn or fail)", path, raw.Verify)
	}
	return Policy{Verify: a, Require: raw.Require}, true, nil
}
