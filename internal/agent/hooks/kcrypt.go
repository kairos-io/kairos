package hook

import (
	"fmt"
	"strings"
	"time"

	config "github.com/kairos-io/kairos/pkg/config"
	schema "github.com/kairos-io/kairos/pkg/config/schemas"
	"github.com/kairos-io/kairos/pkg/machine"
	"github.com/kairos-io/kairos/pkg/utils"

	kcryptconfig "github.com/kairos-io/kcrypt/pkg/config"
)

type Kcrypt struct{}

func (k Kcrypt) Run(c config.Config) error {
	return k.run(c)
}

func (k Kcrypt) KRun(c schema.KConfig) error {
	return k.run(c)
}

func (k Kcrypt) run(c config.Configuration) error {
	if !c.HasEncryptedPartitions() {
		return nil
	}

	machine.Mount("COS_OEM", "/oem") //nolint:errcheck
	defer func() {
		machine.Umount("/oem") //nolint:errcheck
	}()

	kcryptc, err := kcryptconfig.GetConfiguration(kcryptconfig.ConfigScanDirs)
	if err != nil {
		fmt.Println("Failed getting kcrypt configuration: ", err.Error())
		if c.FOBE() {
			return err
		}
	}

	for _, p := range c.EncryptedPartitions() {
		out, err := utils.SH(fmt.Sprintf("kcrypt encrypt %s", p))
		if err != nil {
			fmt.Printf("could not encrypt partition: %s\n", out+err.Error())
			if c.FOBE() {
				return err
			}
			// Give time to show the error
			time.Sleep(10 * time.Second)
			return nil // do not error out
		}

		err = kcryptc.SetMapping(strings.TrimSpace(out))
		if err != nil {
			fmt.Println("Failed updating the kcrypt configuration file: ", err.Error())
			if c.FOBE() {
				return err
			}
		}
	}

	err = kcryptc.WriteMappings(kcryptconfig.MappingsFile)
	if err != nil {
		fmt.Println("Failed writing kcrypt partition mappings: ", err.Error())
		if c.FOBE() {
			return err
		}
	}

	return nil
}
