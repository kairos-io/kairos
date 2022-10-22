package machine

import (
	"fmt"
	"os"
	"strings"

	"github.com/kairos-io/kairos/pkg/utils"
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

func Mount(label, mountpoint string) error {
	part, _ := utils.SH(fmt.Sprintf("blkid -L %s", label))
	if part == "" {
		fmt.Printf("%s partition not found\n", label)
		return fmt.Errorf("partition not found")
	}

	part = strings.TrimSuffix(part, "\n")

	if !Exists(mountpoint) {
		err := os.MkdirAll(mountpoint, 0755)
		if err != nil {
			return err
		}
	}
	mount, err := utils.SH(fmt.Sprintf("mount %s %s", part, mountpoint))
	if err != nil {
		fmt.Printf("could not mount: %s\n", mount+err.Error())
		return err
	}
	return nil
}
