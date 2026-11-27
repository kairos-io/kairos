package system

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/kairos-io/kairos/v4/sdk/mounts"
	"github.com/kairos-io/kairos/v4/sdk/state"
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

	partition, subpath := persistentCloudConfigTarget(runtime)
	return writeCloudConfig(partition, cloudConfig, subpath, filename)
}

// persistentCloudConfigTarget returns the partition and subpath that persistent
// cloud config is written to: the OEM partition root when there is one,
// otherwise the cloud-config directory on the persistent partition.
func persistentCloudConfigTarget(runtime state.Runtime) (state.PartitionState, string) {
	if runtime.OEM.Name == "" {
		return runtime.Persistent, "cloud-config"
	}
	return runtime.OEM, ""
}

func writeCloudConfig(oem state.PartitionState, cloudConfig, subpath, filename string) error {
	mountPath := "/tmp/oem"

	if err := mounts.PrepareWrite(oem, mountPath); err != nil {
		return err
	}
	// Paired with PrepareWrite rather than deferred before it. A partition
	// found mounted read only is read write from here until this runs, and
	// there is nothing to undo until PrepareWrite has succeeded.
	defer mounts.FinishWrite(oem, mountPath) //nolint:errcheck

	_ = os.MkdirAll(filepath.Join(mountPath, subpath), 0650)
	return os.WriteFile(filepath.Join(mountPath, subpath, fmt.Sprintf("%s.yaml", filename)), []byte(cloudConfig), 0650)
}

// WritePersistentCloudData adds cloud config data to oem (/oem or /usr/local/cloud-config, depending if OEM partition exists).
func WritePersistentCloudData(cloudConfig, filename string) Option {
	return func(c *Changeset) error {
		if len(cloudConfig) > 0 {
			c.Add(func() error { return addPersistentCloudConfig(cloudConfig, filename) })
		}
		return nil
	}
}

// WriteLocalPersistentCloudData adds cloud config data to /usr/local/cloud-config.
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

	partition, subpath := persistentCloudConfigTarget(runtime)
	return writeCloudConfig(partition, cloudConfig, subpath, filename)
}

func addLocalCloudConfig(cloudConfig, filename string) error {
	runtime, err := state.NewRuntime()
	if err != nil {
		return err
	}

	return writeCloudConfig(runtime.Persistent, cloudConfig, "cloud-config", filename)
}
