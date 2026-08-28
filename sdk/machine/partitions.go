package machine

import (
	"fmt"
	"os"

	"github.com/kairos-io/kairos/v4/sdk/kcrypt/lookup"
	"github.com/kairos-io/kairos/v4/sdk/utils"
)

func Umount(path string) error {
	out, err := utils.SH(fmt.Sprintf("umount %s", path))
	if err != nil {
		return fmt.Errorf("failed umounting: %s: %w", out, err)
	}
	return nil
}

func Remount(opt, path string) error {
	out, err := utils.SH(fmt.Sprintf("mount -o %s,remount %s", opt, path))
	if err != nil {
		return fmt.Errorf("failed umounting: %s: %w", out, err)
	}
	return nil
}

// Mount resolves the given filesystem label to a concrete device path and
// mounts it at mountpoint. Post-install callers (agent hooks copying logs,
// installing bundles, hardening SSH, wiring grub options) previously reached
// for `blkid -L <label>` directly, which on pre-fix encrypted installs races
// with udev between the LUKS container and its unlocked mapper
// (kairos-io/kairos#4403). lookup.MountSourceForLabel bypasses the by-label
// symlink entirely, returning the mapper path for LUKS-wrapped partitions
// and the plain partition path otherwise.
func Mount(label, mountpoint string) error {
	part, err := lookup.MountSourceForLabel(label)
	if err != nil {
		return fmt.Errorf("resolving mount source for label %q: %w", label, err)
	}
	if part == "" {
		return fmt.Errorf("resolving mount source for label %q: empty result", label)
	}

	if !utils.Exists(mountpoint) {
		if err := os.MkdirAll(mountpoint, 0755); err != nil {
			return err
		}
	}
	out, err := utils.SH(fmt.Sprintf("mount %s %s", part, mountpoint))
	if err != nil {
		return fmt.Errorf("mounting %s at %s: %w (output: %s)", part, mountpoint, err, out)
	}
	return nil
}
