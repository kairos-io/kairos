//go:build !initramfs

package main

import (
	agent "github.com/kairos-io/kairos/agent/pkg/cmd"
)

func init() {
	register("kairos-agent", agent.Run)
}
