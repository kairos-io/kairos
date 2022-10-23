package hook

import (
	config "github.com/kairos-io/kairos/pkg/config"
	"github.com/kairos-io/kairos/sdk/system"
)

type GrubOptions struct{}

func (b GrubOptions) Run(c config.Config) error {
	return system.SetGRUBOptions(c.Install.GrubOptions)
}
