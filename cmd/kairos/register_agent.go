package main

import (
	agent "github.com/kairos-io/kairos/v4/agent/pkg/cmd"
)

func init() {
	// "kairos-agent" is the historical binary name used by symlink invocation
	// and existing scripts; it is registered as an alias so both
	// `kairos agent --help` and `kairos kairos-agent --help` work.
	register("agent", agent.Run, "kairos-agent")

	// The agent has a `version` command that prints the bare version, and
	// `--long` for more. Scripts parse that output: AuroraBoot's install
	// script compares it against a minimum agent version. The agent does
	// not define --version or -v, so those stay with the dispatcher.
	ownsVersion("agent", func(arg string) bool { return arg == "version" })
}
