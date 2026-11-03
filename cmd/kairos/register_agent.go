package main

import (
	agent "github.com/kairos-io/kairos/v4/agent/pkg/cmd"
	"github.com/spf13/cobra"
)

func init() {
	// "kairos-agent" is the historical binary name used by symlink invocation
	// and existing scripts; it is registered as an alias so both
	// `kairos agent --help` and `kairos kairos-agent --help` work.
	//
	// A failed agent command is reported the way the agent reports it, which
	// includes the progress event an installer frontend reads.
	register(func() *cobra.Command {
		cmd := agent.NewCommand()
		cmd.Use = "agent"
		cmd.Aliases = []string{"kairos-agent"}
		return cmd
	}, agent.ReportError)

	// The agent has its own `version` command, which prints the bare version,
	// or `kairos <version>` and the Go version with `--long`. Scripts parse
	// that output: AuroraBoot's install script compares it against a minimum
	// agent version. The agent does not define --version or -v, so those stay
	// with kairos.
	ownsVersion("agent", func(arg string) bool { return arg == "version" })
}
