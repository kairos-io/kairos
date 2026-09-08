package hook

import (
	"fmt"
	"path/filepath"
	"strings"

	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
	sdkSpec "github.com/kairos-io/kairos/v4/sdk/types/spec"
)

// OEMFiles drops the cloud-config files listed under install.oem_files into
// the cloud-config directory of the freshly installed system, so they are
// applied on its first boot.
type OEMFiles struct{}

func (OEMFiles) Run(c sdkConfig.Config, _ sdkSpec.Spec) error {
	if c.Install == nil || len(c.Install.OEMFiles) == 0 {
		return nil
	}
	c.Logger.Logger.Debug().Msg("Running OEMFiles hook")

	// Validate every name before mounting anything, so a typo in the last
	// entry does not leave the earlier ones written.
	for _, f := range c.Install.OEMFiles {
		if _, err := oemFileName(f.Name); err != nil {
			return err
		}
	}

	dir, cleanup, err := cloudConfigDir(c)
	if err != nil {
		return err
	}
	defer cleanup()

	for _, f := range c.Install.OEMFiles {
		name, err := oemFileName(f.Name)
		if err != nil {
			return err
		}
		path := filepath.Join(dir, name)
		if err := writeCloudConfig(c.Fs, path, f.Content); err != nil {
			return fmt.Errorf("writing %s: %w", path, err)
		}
		c.Logger.Logger.Debug().Str("file", path).Msg("Wrote oem file")
	}

	c.Logger.Logger.Debug().Msg("Finish OEMFiles hook")
	return nil
}

// oemFileName turns the name of an install.oem_files entry into a file name.
// A bare name gets the yaml extension yip expects, one that already carries
// it is left alone. The name has to stay a single file name: it decides what
// is written to a directory the installed system trusts.
func oemFileName(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("install.oem_files: entry without a name")
	}
	if name == "." || name == ".." || strings.ContainsRune(name, filepath.Separator) {
		return "", fmt.Errorf("install.oem_files: %q is not a file name", name)
	}
	if ext := filepath.Ext(name); ext == ".yaml" || ext == ".yml" {
		return name, nil
	}
	return name + ".yaml", nil
}
