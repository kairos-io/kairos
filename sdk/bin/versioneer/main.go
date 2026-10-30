package main

import (
	"log"

	"github.com/kairos-io/kairos/v4/sdk/versioneer"
	"github.com/spf13/cobra"
)

func main() {
	root := &cobra.Command{
		Use:               "versioneer",
		SilenceUsage:      true,
		SilenceErrors:     true,
		CompletionOptions: cobra.CompletionOptions{DisableDefaultCmd: true},
	}
	root.AddCommand(versioneer.CliCommands()...)

	if err := root.Execute(); err != nil {
		log.Fatal(err)
	}
}
