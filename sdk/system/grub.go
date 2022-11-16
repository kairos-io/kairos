package system

import (
	"fmt"

	"github.com/kairos-io/kairos/pkg/machine"
	"github.com/kairos-io/kairos/pkg/utils"
	"github.com/kairos-io/kairos/sdk/mounts"
	"github.com/kairos-io/kairos/sdk/state"
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
	runtime, err := state.NewRuntime()
	if err != nil {
		return err
	}

	oem := runtime.OEM
	if runtime.OEM.Name == "" {
		oem = runtime.Persistent
	}

	if err := mounts.PrepareWrite(oem, "/tmp/oem"); err != nil {
		return err
	}
	defer func() {
		machine.Umount("/tmp/oem") //nolint:errcheck
	}()

	for k, v := range opts {
		out, err := utils.SH(fmt.Sprintf(`%s /tmp/oem/grubenv set "%s=%s"`, machine.FindCommand("grub2-editenv", []string{"grub2-editenv", "grub-editenv"}), k, v))
		if err != nil {
			fmt.Printf("could not set boot option: %s\n", out+err.Error())
		}
	}

	return nil
}
