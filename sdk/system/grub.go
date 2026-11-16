package system

import (
	"errors"
	"fmt"
	"strings"

	"github.com/kairos-io/kairos/v4/sdk/mounts"
	"github.com/kairos-io/kairos/v4/sdk/state"
	"github.com/kairos-io/kairos/v4/sdk/utils"
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

	editenv := utils.FindCommand("grub2-editenv", []string{"grub2-editenv", "grub-editenv"})
	return setGRUBEnv(opts, func(k, v string) (string, error) {
		return utils.SH(fmt.Sprintf(`%s %s/grubenv set "%s=%s"`, editenv, mountPath, k, v))
	})
}

// setGRUBEnv writes each option with set. It tries every option and returns
// all failures, so a failed write fails the changeset instead of only being
// printed, and one bad key does not hide the others.
func setGRUBEnv(opts map[string]string, set func(k, v string) (string, error)) error {
	var errs []error
	for k, v := range opts {
		out, err := set(k, v)
		if err != nil {
			errs = append(errs, fmt.Errorf("could not set boot option %q: %w: %s", k, err, strings.TrimSpace(out)))
		}
	}

	return errors.Join(errs...)
}
