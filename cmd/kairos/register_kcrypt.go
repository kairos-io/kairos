package main

import (
	kcrypt "github.com/kairos-io/kairos/v4/kcrypt/discovery"
	"github.com/spf13/cobra"
)

func init() {
	// "kcrypt-discovery-challenger" is the historical binary name used by
	// symlink invocation and existing scripts; it is registered as an alias
	// so `kairos kcrypt`, `kairos kcrypt-discovery-challenger`, and the
	// symlink `./kcrypt-discovery-challenger` all resolve to the same code.
	register(func() *cobra.Command {
		cmd := kcrypt.Command()
		cmd.Use = "kcrypt"
		cmd.Aliases = []string{"kcrypt-discovery-challenger"}
		return cmd
	}, nil)
}
