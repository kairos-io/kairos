package hook

import (
	"fmt"
	"strings"

	config "github.com/kairos-io/kairos/pkg/config"
	"github.com/kairos-io/kairos/pkg/utils"
)

type GrubOptions struct{}

func (b GrubOptions) Run(c config.Config) error {
	oem, _ := utils.SH("blkid -L COS_OEM")
	if oem == "" {
		fmt.Println("OEM partition not found")
		return nil // do not error out
	}

	oem = strings.TrimSuffix(oem, "\n")

	oemMount, err := utils.SH(fmt.Sprintf("mkdir /tmp/oem && mount %s /tmp/oem", oem))
	if err != nil {
		fmt.Printf("could not mount oem: %s\n", oemMount+err.Error())
		return nil // do not error out
	}

	for k, v := range c.Install.GrubOptions {
		out, err := utils.SH(fmt.Sprintf("grub2-editenv /tmp/oem/grubenv set %s=%s", k, v))
		if err != nil {
			fmt.Printf("could not set boot option: %s\n", out+err.Error())
			return nil // do not error out
		}
	}

	utils.SH("umount /tmp/oem") //nolint:errcheck
	return nil
}
