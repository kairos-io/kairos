// Package state manages the .kairos-state file kept on the persistent
// partition. The file records the machine's first install and the most recent
// active/recovery upgrade plus the last factory reset, together with the
// image source for each event. It gives users a persistent, human-readable
// snapshot of the machine's lifecycle history.
package state

import (
	"errors"
	"os"
	"path/filepath"

	sdkFs "github.com/kairos-io/kairos-sdk/types/fs"
	"gopkg.in/yaml.v3"
)

// FileName is the name of the state file at the root of the persistent partition.
const FileName = ".kairos-state"

// NeverValue is the placeholder used for events that have not happened yet.
const NeverValue = "never"

// KairosState is the on-disk representation of the state file. FirstInstall is
// set once when the machine is installed and never changes afterwards. The
// remaining pairs track the most recent occurrence of each lifecycle event.
// Passive state is intentionally not tracked here — passive is derived state
// (the previous active image after an upgrade), not an event.
type KairosState struct {
	FirstInstall        string `yaml:"first-install"`
	FirstInstallSource  string `yaml:"first-install-source"`
	LastActiveUpgrade   string `yaml:"last-active-upgrade"`
	LastActiveSource    string `yaml:"last-active-source"`
	LastRecoveryUpgrade string `yaml:"last-recovery-upgrade"`
	LastRecoverySource  string `yaml:"last-recovery-source"`
	LastReset           string `yaml:"last-reset"`
	LastResetSource     string `yaml:"last-reset-source"`
}

// Default returns a KairosState with all timestamps set to NeverValue and all
// sources empty.
func Default() *KairosState {
	return &KairosState{
		FirstInstall:        NeverValue,
		LastActiveUpgrade:   NeverValue,
		LastRecoveryUpgrade: NeverValue,
		LastReset:           NeverValue,
	}
}

// Path returns the full state-file path under the given persistent mount point.
func Path(mountPoint string) string {
	return filepath.Join(mountPoint, FileName)
}

// Load reads the state file at path. If the file does not exist Default() is
// returned with a nil error. A YAML parse error is returned as-is so callers
// can decide whether to back it up and start fresh.
func Load(fs sdkFs.KairosFS, path string) (*KairosState, error) {
	data, err := fs.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Default(), nil
		}
		return nil, err
	}
	s := Default()
	if err := yaml.Unmarshal(data, s); err != nil {
		return nil, err
	}
	return s, nil
}

// Save writes the state file to path with 0644 permissions.
func (s *KairosState) Save(fs sdkFs.KairosFS, path string) error {
	data, err := yaml.Marshal(s)
	if err != nil {
		return err
	}
	return fs.WriteFile(path, data, 0644)
}
