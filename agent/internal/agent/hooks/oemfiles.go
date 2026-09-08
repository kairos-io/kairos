package hook

import (
	"fmt"
	"path/filepath"
	"strings"

	internalutils "github.com/kairos-io/kairos/v4/agent/pkg/utils"
	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
	sdkPartitions "github.com/kairos-io/kairos/v4/sdk/types/partitions"
	sdkSpec "github.com/kairos-io/kairos/v4/sdk/types/spec"
)

// OEMFiles drops the cloud-config files listed under install.oem_files into
// the OEM partition of the freshly installed system, so they are applied on
// its first boot.
type OEMFiles struct{}

func (OEMFiles) Run(c sdkConfig.Config, spec sdkSpec.Spec) error {
	if c.Install == nil || len(c.Install.OEMFiles) == 0 {
		return nil
	}
	c.Logger.Logger.Debug().Msg("Running OEMFiles hook")

	// Validate every name before writing anything, so a typo in the last
	// entry does not leave the earlier ones written.
	for _, f := range c.Install.OEMFiles {
		if _, err := oemFileName(f.Name); err != nil {
			return err
		}
	}

	dir, err := oemFilesDir(c, spec)
	if err != nil {
		return err
	}

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

// oemFilesDir returns the mountpoint the installer already has the OEM
// partition on, which the installed system reads as /oem. Nothing is mounted
// here: the hook runs from hook.PostInstall, while the partitions the
// installer mounted are all still up, and an install whose OEM partition
// could not be mounted has already failed by then. Any other directory would
// live on the live media and be gone after the reboot, so refuse instead of
// reporting a write that goes nowhere.
func oemFilesDir(c sdkConfig.Config, spec sdkSpec.Spec) (string, error) {
	part := oemPartition(spec)
	if part == nil || part.MountPoint == "" {
		return "", fmt.Errorf("install.oem_files: the install spec has no mounted OEM partition to write to")
	}
	mounted, err := internalutils.IsMounted(&c, part)
	if err != nil {
		return "", fmt.Errorf("install.oem_files: checking whether %s is mounted: %w", part.MountPoint, err)
	}
	if !mounted {
		return "", fmt.Errorf("install.oem_files: %s is not mounted, the files would not reach the installed system", part.MountPoint)
	}
	return part.MountPoint, nil
}

// oemPartition returns the OEM partition of an install spec, or nil for a
// spec that carries no partition table.
func oemPartition(spec sdkSpec.Spec) *sdkPartitions.Partition {
	installSpec, ok := spec.(sdkSpec.SharedInstallSpec)
	if !ok {
		return nil
	}
	return installSpec.GetPartitions().OEM
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
