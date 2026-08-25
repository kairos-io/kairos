package main

import (
	"os"

	"github.com/kairos-io/kairos/v4/agent/pkg/cmd"
)

func main() {
	os.Exit(cmd.Run())
}
