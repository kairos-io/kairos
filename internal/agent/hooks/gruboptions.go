package hook

import (
	"fmt"

	"github.com/kairos-io/kairos-sdk/system"
	config "github.com/kairos-io/kairos/pkg/config"
)

type GrubOptions struct{}

func (b GrubOptions) Run(c config.Config) error {
	err := system.Apply(system.SetGRUBOptions(c.Install.GrubOptions))
	if err != nil {
		fmt.Println(err)
	}
	return nil
}

type GrubPostInstallOptions struct{}

func (b GrubPostInstallOptions) Run(c config.Config) error {
	err := system.Apply(system.SetGRUBOptions(c.GrubOptions))
	if err != nil {
		fmt.Println(err)
	}
	return nil
}
