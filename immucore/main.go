package main

import (
	"os"

	"github.com/kairos-io/kairos/immucore/pkg/cmd"
)

// Apply Immutability profiles.
func main() {
	os.Exit(cmd.Run())
}
