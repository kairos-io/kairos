package hook

import (
	"fmt"

	config "github.com/kairos-io/kairos/pkg/config"
	"github.com/kairos-io/kairos/sdk/system"
)

type GrubOptions struct{}

func (b GrubOptions) Run(c config.Config) error {
	err := system.Apply(system.SetGRUBOptions(c.Install.GrubOptions))
	if err != nil {
		fmt.Println(err)
	}
	return nil
}
