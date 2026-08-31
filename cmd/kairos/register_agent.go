package main

import (
	agent "github.com/kairos-io/kairos/v4/agent/pkg/cmd"
)

func init() {
	// "kairos-agent" is the historical binary name used by symlink invocation
	// and existing scripts; it is registered as an alias so both
	// `kairos agent --help` and `kairos kairos-agent --help` work.
	register("agent", agent.Run, "kairos-agent")
}
