package cmd

import (
	"os"

	"github.com/kairos-io/AuroraBoot/pkg/utils"
	"github.com/kairos-io/kairos/v4/sdk/types/logger"
	"github.com/urfave/cli/v2"
)

var UkiPXECmd = cli.Command{
	Name:      "uki-pxe",
	Usage:     "Serve PXE boot files using a specified ISO file",
	ArgsUsage: "ISO_FILE",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "loglevel",
			Aliases: []string{"l"},
			Usage:   "Set the log level",
			Value:   "info",
		},
	},
	Before: func(c *cli.Context) error {
		// Ensure we have exactly one argument (the ISO file)
		if c.NArg() != 1 {
			return cli.Exit("exactly one argument required: the path to the ISO file", 1)
		}

		isoFile := c.Args().First()
		if _, err := os.Stat(isoFile); err != nil {
			if os.IsNotExist(err) {
				return cli.Exit("iso file does not exist", 1)
			}
			return cli.Exit("error checking iso file: "+err.Error(), 1)
		}
		return nil
	},
	Action: func(context *cli.Context) error {
		log := logger.NewKairosLogger("pxe", context.String("loglevel"), false)
		return utils.ServeUkiPXE(context.Args().First(), log)
	},
}
