package system

import (
	"fmt"

	"github.com/kairos-io/kairos-sdk/mounts"
	"github.com/kairos-io/kairos-sdk/state"
	"github.com/kairos-io/kairos-sdk/utils"
)

func SetGRUBOptions(opts map[string]string) Option {
	return func(c *Changeset) error {
		if len(opts) > 0 {
			c.Add(func() error { return setGRUBOptions(opts) })
		}
		return nil
	}
}

func setGRUBOptions(opts map[string]string) error {
	mountPath := "/tmp/oem"
	defer mounts.Umount(state.PartitionState{Mounted: true, MountPoint: mountPath}) //nolint:errcheck
	runtime, err := state.NewRuntime()
	if err != nil {
		return err
	}

	oem := runtime.OEM
	if runtime.OEM.Name == "" {
		oem = runtime.Persistent
	}

	if err := mounts.PrepareWrite(oem, mountPath); err != nil {
		return err
	}

	for k, v := range opts {
		out, err := utils.SH(fmt.Sprintf(`%s /tmp/oem/grubenv set "%s=%s"`, utils.FindCommand("grub2-editenv", []string{"grub2-editenv", "grub-editenv"}), k, v))
		if err != nil {
			fmt.Printf("could not set boot option: %s\n", out+err.Error())
		}
	}

	return nil
}
