package machine

import (
	"fmt"
	"os"
	"strings"

	"github.com/kairos-io/kairos/pkg/utils"
)

func Umount(path string) {
	utils.SH(fmt.Sprintf("umount %s", path)) //nolint:errcheck
}

func Mount(label, mountpoint string) {
	part, _ := utils.SH(fmt.Sprintf("blkid -L %s", label))
	if part == "" {
		fmt.Printf("%s partition not found\n", label)
	}

	part = strings.TrimSuffix(part, "\n")

	if !Exists(mountpoint) {
		os.MkdirAll(mountpoint, 0755)
	}
	mount, err := utils.SH(fmt.Sprintf("mount %s %s", part, mountpoint))
	if err != nil {
		fmt.Printf("could not mount: %s\n", mount+err.Error())
	}
}
