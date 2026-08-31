package main

import (
	"os"

	"github.com/kairos-io/kairos/v4/immucore/pkg/cmd"
)

// Apply Immutability profiles.
func main() {
	os.Exit(cmd.Run())
}
