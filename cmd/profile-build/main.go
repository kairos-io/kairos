package main

import (
	"fmt"
	"os"

	"github.com/kairos-io/kairos/internal/common"
	"github.com/kairos-io/kairos/sdk/profile"
	"github.com/urfave/cli/v2"
)

func main() {

	app := &cli.App{
		Name:    "profile-build",
		Version: common.VERSION,
		Authors: []*cli.Author{
			{
				Name: "Kairos authors",
			},
		},
		Usage: "Build kairos framework images",
		Description: `
Uses profile files to build kairos images`,
		UsageText: ``,
		Copyright: "kairos authors",
		ArgsUsage: "flavor profileName profileFile outputDirectory",
		Action: func(c *cli.Context) error {
			return profile.BuildFlavor(c.Args().Get(0), c.Args().Get(1), c.Args().Get(2))
		},
	}

	err := app.Run(os.Args)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
