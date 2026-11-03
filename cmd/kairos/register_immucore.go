package main

import (
	immucore "github.com/kairos-io/kairos/v4/immucore/pkg/cmd"
	"github.com/spf13/cobra"
)

func init() {
	// "init" alias: on a Kairos UKI, /init is a symlink to /usr/bin/immucore
	// (created by AuroraBoot's uki builder, see pkg/uki/uki.go). The kernel
	// exec()s /init as PID 1 with argv[0]="/init", so once immucore itself
	// is a symlink to the multi-call kairos, argv[0]'s basename is "init"
	// rather than "immucore". Without this alias PID 1 exits with "unknown
	// sub-tool" and the kernel panics with "Attempted to kill init".
	register(func() *cobra.Command {
		cmd := immucore.NewCommand()
		cmd.Aliases = []string{"init"}
		return cmd
	}, nil)
}
