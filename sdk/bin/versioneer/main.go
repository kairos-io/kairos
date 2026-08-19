package main

import (
	"log"
	"os"

	"github.com/kairos-io/kairos-sdk/versioneer"
	"github.com/urfave/cli/v2"
)

func main() {
	app := &cli.App{Commands: versioneer.CliCommands()}

	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}
