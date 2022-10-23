package system

import (
	"fmt"

	"github.com/kairos-io/kairos/pkg/machine"
	"github.com/kairos-io/kairos/pkg/utils"
	"github.com/kairos-io/kairos/sdk/mounts"
	"github.com/kairos-io/kairos/sdk/state"
)

func SetGRUBOptions(opts map[string]string) error {

	runtime, err := state.NewRuntime()
	if err != nil {
		return err
	}

	oem := runtime.OEM
	if runtime.OEM.Name == "" {
		oem = runtime.Persistent
	}

	mounts.PrepareWrite(oem, "/tmp/oem")
	defer func() {
		machine.Umount("/tmp/oem")
	}()
	for k, v := range opts {
		out, err := utils.SH(fmt.Sprintf(`grub2-editenv /tmp/oem/grubenv set "%s=%s"`, k, v))
		if err != nil {
			fmt.Printf("could not set boot option: %s\n", out+err.Error())
			return nil // do not error out
		}
	}

	return nil
}
