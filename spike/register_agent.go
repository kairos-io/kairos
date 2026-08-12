//go:build !initramfs

package main

import (
	agent "github.com/kairos-io/kairos-agent/v2/pkg/cmd"
)

func init() {
	register("kairos-agent", agent.Run)
}
