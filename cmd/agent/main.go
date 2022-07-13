package main

import (
	//"fmt"

	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/c3os-io/c3os/internal/bus"
	cmd "github.com/c3os-io/c3os/internal/cmd"
	machine "github.com/c3os-io/c3os/internal/machine"

	"github.com/c3os-io/c3os/internal/github"
	config "github.com/c3os-io/c3os/pkg/config"
	"github.com/urfave/cli"
	"gopkg.in/yaml.v2"
)

var cmds = []cli.Command{
	{
		Name: "upgrade",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "force",
				Usage: "Force an upgrade",
			},
			&cli.StringFlag{
				Name:  "image",
				Usage: "Specify an full image reference, e.g.: quay.io/some/image:tag",
			},
		},
		Description: `
Manually upgrade a c3os node.

By default takes no arguments, defaulting to latest available release, to specify a version, pass it as argument:

$ c3os upgrade v1.20....

To retrieve all the available versions, use "c3os upgrade list-releases"

$ c3os upgrade list-releases

See https://docs.c3os.io/after_install/upgrades/#manual for documentation.

`,
		Subcommands: []cli.Command{
			{
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:  "output",
						Usage: "Output format (json|yaml|terminal)",
					},
				},
				Name:        "list-releases",
				Description: `List all available releases versions`,
				Action: func(c *cli.Context) error {
					rels, err := github.FindReleases(context.Background(), "", "c3os-io/c3os")
					if err != nil {
						return err
					}

					switch strings.ToLower(c.String("output")) {
					case "yaml":
						d, _ := yaml.Marshal(rels)
						fmt.Println(string(d))
					case "json":
						d, _ := json.Marshal(rels)
						fmt.Println(string(d))
					default:
						for _, r := range rels {
							fmt.Println(r)
						}
					}

					return nil
				},
			},
		},
		Action: func(c *cli.Context) error {
			args := c.Args()
			var v string
			if len(args) == 1 {
				v = args[0]
			}

			return upgrade(v, c.String("image"), c.Bool("force"))
		},
	},

	{
		Name:      "start",
		Usage:     "Starts the c3os agent",
		UsageText: "starts the agent",
		Description: `
Starts the c3os agent which automatically bootstrap and advertize to the c3os network.
`,
		Aliases: []string{"s"},
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name: "force",
			},
			&cli.StringFlag{
				Name:  "api",
				Value: "http://127.0.0.1:8080",
			},
		},
		Action: func(c *cli.Context) error {
			dirs := []string{"/oem", "/usr/local/cloud-config"}
			args := c.Args()
			if len(args) > 0 {
				dirs = args
			}

			return agent(c.String("api"), dirs, c.Bool("force"))
		},
	},
	{
		Name:  "install-bundle",
		Usage: "Installs a c3os bundle",
		Description: `

Manually installs a c3os bundle.

E.g. c3os-agent install-bundle container:quay.io/c3os/c3os...

`,
		Aliases: []string{"i"},
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:   "repository",
				EnvVar: "REPOSITORY",
			},
		},
		UsageText: "Install a bundle manually in the node",
		Action: func(c *cli.Context) error {
			args := c.Args()
			if len(args) != 1 {
				return fmt.Errorf("bundle name required")
			}

			return machine.RunBundles([]machine.BundleOption{machine.WithRepository(c.String("repository")), machine.WithTarget(args[0])})
		},
	},
	{
		Name:  "rotate",
		Usage: "Rotate a c3os node network configuration via CLI",
		Description: `

Updates a c3os node VPN configuration.

For example, to update the network token in a node:
$ c3os rotate --network-token XXX
`,
		Aliases: []string{"r"},
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name: "restart",
			},
			&cli.StringFlag{
				Name:   "network-token",
				EnvVar: "NETWORK_TOKEN",
			},
			&cli.StringFlag{
				Name:  "api",
				Value: "127.0.0.1:8080",
			},
			&cli.StringFlag{
				Name: "root-dir",
			},
		},
		UsageText: "Rotate network token manually in the node",
		Action: func(c *cli.Context) error {
			dirs := []string{"/oem", "/usr/local/cloud-config"}
			args := c.Args()
			if len(args) > 0 {
				dirs = args
			}

			return rotate(dirs, c.String("network-token"), c.String("api"), c.String("root-dir"), c.Bool("restart"))
		},
	},

	{
		Name:        "get-network-token",
		Description: "Print network token from local configuration",
		Usage:       "Print network token from local configuration",
		Action: func(c *cli.Context) error {
			dirs := []string{"/oem", "/usr/local/cloud-config"}
			args := c.Args()
			if len(args) > 0 {
				dirs = args
			}
			cc, err := config.Scan(config.Directories(dirs...))
			if err != nil {
				return err
			}
			fmt.Print(cc.C3OS.NetworkToken)
			return nil
		},
	},
	{
		Name:        "uuid",
		Usage:       "Prints the local UUID",
		Description: "Print node uuid",
		Aliases:     []string{"u"},
		Action: func(c *cli.Context) error {
			fmt.Print(machine.UUID())
			return nil
		},
	},
	{
		Name: "interactive-install",
		Description: `
Starts c3os in interactive mode.

It will ask prompt for several questions and perform an install

See also https://docs.c3os.io/installation/interactive_install/ for documentation.

This command is meant to be used from the boot GRUB menu, but can be started manually`,
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name: "shell",
			},
		},
		Usage: "Starts interactive installation",
		Action: func(c *cli.Context) error {
			return interactiveInstall(c.Bool("shell"))
		},
	},
	{
		Name:  "install",
		Usage: "Starts the c3os pairing installation",
		Description: `
Starts c3os in pairing mode.

It will print out a QR code which can be used with "c3os register" to send over a configuration and bootstraping a c3os node.

See also https://docs.c3os.io/installation/device_pairing/ for documentation.

This command is meant to be used from the boot GRUB menu, but can be started manually`,
		Aliases: []string{"i"},
		Action: func(c *cli.Context) error {
			return install("/oem", "/usr/local/cloud-config", "/run/initramfs/live")
		},
	},
	{
		Name:    "recovery",
		Aliases: []string{"r"},
		Action:  recovery,
		Usage:   "Starts c3os recovery mode",
		Description: `
Starts c3os recovery mode.

In recovery mode a QR code will be printed out on the screen which should be used in conjuction with "c3os bridge". Pass by the QR code as snapshot
to the bridge to connect over the machine which runs the "c3os recovery" command.

See also https://docs.c3os.io/after_install/recovery_mode/ for documentation.

This command is meant to be used from the boot GRUB menu, but can likely be used standalone`,
	},

	{
		Name:   "reset",
		Action: reset,
		Usage:  "Starts c3os reset mode",
		Description: `
Starts c3os reset mode, it will nuke completely the node data and restart fresh.
Attention ! this will delete any persistent data on the node. It is equivalent to re-init the node right after the installation.

In reset mode a the node will automatically reset

See also https://docs.c3os.io/after_install/reset_mode/ for documentation.

This command is meant to be used from the boot GRUB menu, but can likely be used standalone`,
	},
}

func main() {
	bus.Manager.Initialize()

	app := &cli.App{
		Name:    "c3os",
		Version: "0.1",
		Author:  "Ettore Di Giacinto",
		Usage:   "c3os CLI to bootstrap, upgrade, connect and manage a c3os network",
		Description: `
The c3os CLI can be used to manage a c3os box and perform all day-two tasks, like:
- register a node
- connect to a node in recovery mode
- to establish a VPN connection
- set, list roles
- interact with the network API

and much more.

For all the example cases, see: https://docs.c3os.io .
`,
		UsageText: ``,
		Copyright: "Ettore Di Giacinto",

		Commands: cmd.CommonCommand(cmds...),
	}

	err := app.Run(os.Args)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
