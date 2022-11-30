package system

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/kairos-io/kairos/pkg/machine"
	"github.com/kairos-io/kairos/sdk/mounts"
	"github.com/kairos-io/kairos/sdk/state"
)

// WriteCloudConfigData adds cloud config data in runtime.
func WriteCloudConfigData(cloudConfig, filename string) Option {
	return func(c *Changeset) error {
		if len(cloudConfig) > 0 {
			c.Add(func() error { return addCloudConfig(cloudConfig, filename) })
		}
		return nil
	}
}

func addCloudConfig(cloudConfig, filename string) error {
	runtime, err := state.NewRuntime()
	if err != nil {
		return err
	}

	oem := runtime.OEM
	if runtime.OEM.Name == "" {
		return addLocalCloudConfig(cloudConfig, filename)
	}

	return writeCloudConfig(oem, cloudConfig, "", filename)
}

func writeCloudConfig(oem state.PartitionState, cloudConfig, subpath, filename string) error {
	mountPath := "/tmp/oem"

	if err := mounts.PrepareWrite(oem, mountPath); err != nil {
		return err
	}
	defer func() {
		machine.Umount(mountPath) //nolint:errcheck
	}()
	_ = os.MkdirAll(filepath.Join(mountPath, subpath), 0650)
	return os.WriteFile(filepath.Join(mountPath, subpath, fmt.Sprintf("%s.yaml", filename)), []byte(cloudConfig), 0650)
}

// WriteCloudConfigData adds cloud config data to oem (/oem or /usr/local/cloud-config, depending if OEM partition exists).
func WritePersistentCloudData(cloudConfig, filename string) Option {
	return func(c *Changeset) error {
		if len(cloudConfig) > 0 {
			c.Add(func() error { return addPersistentCloudConfig(cloudConfig, filename) })
		}
		return nil
	}
}

// WriteLocalCloudConfigData adds cloud config data to /usr/local/cloud-config.
func WriteLocalPersistentCloudData(cloudConfig, filename string) Option {
	return func(c *Changeset) error {
		if len(cloudConfig) > 0 {
			c.Add(func() error { return addLocalCloudConfig(cloudConfig, filename) })
		}
		return nil
	}
}

func addPersistentCloudConfig(cloudConfig, filename string) error {
	runtime, err := state.NewRuntime()
	if err != nil {
		return err
	}

	return writeCloudConfig(runtime.State, cloudConfig, "", filename)
}

func addLocalCloudConfig(cloudConfig, filename string) error {
	runtime, err := state.NewRuntime()
	if err != nil {
		return err
	}

	return writeCloudConfig(runtime.Persistent, cloudConfig, "cloud-config", filename)
}
