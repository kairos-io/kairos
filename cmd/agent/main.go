package main

import (
	"fmt"
	"os"

	agent "github.com/c3os-io/c3os/internal/agent"
	"github.com/c3os-io/c3os/internal/bus"

	machine "github.com/c3os-io/c3os/pkg/machine"
	"github.com/c3os-io/c3os/pkg/utils"
	bundles "github.com/c3os-io/c3os/sdk/bundles"

	"github.com/urfave/cli"
)

var cmds = []cli.Command{
	{
		Name: "upgrade",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "force",
				Usage: "Force an upgrade",
			},
			&cli.BoolFlag{
				Name:  "debug",
				Usage: "Show debug output",
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
					releases := agent.ListReleases()
					releases = utils.ListOutput(releases, c.String("output"))
					for _, r := range releases {
						fmt.Println(r)
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

			return agent.Upgrade(v, c.String("image"), c.Bool("force"), c.Bool("debug"))
		},
	},

	{
		Name:      "notify",
		Usage:     "notify <event> <config dir>...",
		UsageText: "emits the given event with a generic event payload",
		Description: `
Sends a generic event payload with the configuration found in the scanned directories.
`,
		Aliases: []string{},
		Flags:   []cli.Flag{},
		Action: func(c *cli.Context) error {
			dirs := []string{"/oem", "/usr/local/cloud-config"}
			args := c.Args()
			if len(args) > 1 {
				dirs = args[1:]
			}

			return agent.Notify(args[0], dirs)
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
				Name: "restart",
			},
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

			opts := []agent.Option{
				agent.WithAPI(c.String("api")),
				agent.WithDirectory(dirs...),
			}

			if c.Bool("force") {
				opts = append(opts, agent.ForceAgent)
			}

			if c.Bool("restart") {
				opts = append(opts, agent.RestartAgent)
			}

			return agent.Run(opts...)
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
				Value:  "docker://quay.io/c3os/packages",
			},
		},
		UsageText: "Install a bundle manually in the node",
		Action: func(c *cli.Context) error {
			args := c.Args()
			if len(args) != 1 {
				return fmt.Errorf("bundle name required")
			}

			return bundles.RunBundles([]bundles.BundleOption{bundles.WithRepository(c.String("repository")), bundles.WithTarget(args[0])})
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
Starts c3os in interactive mode install.

It will ask prompt for several questions and perform an install depending on the providers available in the system.

See also https://docs.c3os.io/installation/interactive_install/ for documentation.

This command is meant to be used from the boot GRUB menu, but can be also started manually`,
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name: "shell",
			},
		},
		Usage: "Starts interactive installation",
		Action: func(c *cli.Context) error {
			return agent.InteractiveInstall(c.Bool("shell"))
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
			return agent.Install("/oem", "/usr/local/cloud-config", "/run/initramfs/live")
		},
	},
	{
		Name:    "recovery",
		Aliases: []string{"r"},
		Action: func(c *cli.Context) error {
			return agent.Recovery()
		},
		Usage: "Starts c3os recovery mode",
		Description: `
Starts c3os recovery mode.

In recovery mode a QR code will be printed out on the screen which should be used in conjunction with "c3os bridge". Pass by the QR code as snapshot
to the bridge to connect over the machine which runs the "c3os recovery" command.

See also https://docs.c3os.io/after_install/recovery_mode/ for documentation.

This command is meant to be used from the boot GRUB menu, but can likely be used standalone`,
	},

	{
		Name: "reset",
		Action: func(c *cli.Context) error {
			return agent.Reset()
		},
		Usage: "Starts c3os reset mode",
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
		Name:    "c3os-agent",
		Version: "0.1",
		Author:  "Ettore Di Giacinto",
		Usage:   "c3os agent start",
		Description: `
The c3os agent is a component to abstract away node ops, providing a common feature-set across c3os variants.
`,
		UsageText: ``,
		Copyright: "c3os authors",

		Commands: cmds,
	}

	err := app.Run(os.Args)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
