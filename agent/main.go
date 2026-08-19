package main

import (
	"os"

	"github.com/kairos-io/kairos/agent/pkg/cmd"
)

func main() {
	os.Exit(cmd.Run())
}
