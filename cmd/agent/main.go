package main

import (
	"context"
	"fmt"
	"os"

	agent "github.com/kairos-io/kairos/internal/agent"
	"github.com/kairos-io/kairos/internal/bus"
	"github.com/kairos-io/kairos/internal/webui"

	bundles "github.com/kairos-io/kairos-sdk/bundles"
	"github.com/kairos-io/kairos-sdk/state"
	"github.com/kairos-io/kairos/internal/common"
	"github.com/kairos-io/kairos/pkg/config"
	machine "github.com/kairos-io/kairos/pkg/machine"
	"github.com/kairos-io/kairos/pkg/utils"

	"github.com/urfave/cli/v2"
)

var configScanDir = []string{"/oem", "/usr/local/cloud-config", "/run/initramfs/live"}

var cmds = []*cli.Command{
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
Manually upgrade a kairos node.

By default takes no arguments, defaulting to latest available release, to specify a version, pass it as argument:

$ kairos upgrade v1.20....

To retrieve all the available versions, use "kairos upgrade list-releases"

$ kairos upgrade list-releases

See https://kairos.io/docs/upgrade/manual/ for documentation.

`,
		Subcommands: []*cli.Command{
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
					list := utils.ListOutput(releases, c.String("output"))
					for _, i := range list {
						fmt.Println(i)
					}

					return nil
				},
			},
		},

		Action: func(c *cli.Context) error {
			var v string
			if c.Args().Len() == 1 {
				v = c.Args().First()
			}
			return agent.Upgrade(v, c.String("image"), c.Bool("force"), c.Bool("debug"), c.Bool("strict-validation"), configScanDir)
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
			if c.Args().Len() > 1 {
				dirs = c.Args().Slice()[1:]
			}

			return agent.Notify(c.Args().First(), dirs)
		},
	},

	{
		Name:      "start",
		Usage:     "Starts the kairos agent",
		UsageText: "starts the agent",
		Description: `
Starts the kairos agent which automatically bootstrap and advertize to the kairos network.
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
			if c.Args().Present() {
				dirs = c.Args().Slice()
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
		Usage: "Installs a kairos bundle",
		Description: `

Manually installs a kairos bundle.

E.g. kairos-agent install-bundle container:quay.io/kairos/kairos...

`,
		Aliases: []string{"i"},
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "repository",
				EnvVars: []string{"REPOSITORY"},
				Value:   "docker://quay.io/kairos/packages",
			},
		},
		UsageText: "Install a bundle manually in the node",
		Action: func(c *cli.Context) error {
			if c.Args().Len() != 1 {
				return fmt.Errorf("bundle name required")
			}

			return bundles.RunBundles([]bundles.BundleOption{bundles.WithRepository(c.String("repository")), bundles.WithTarget(c.Args().First())})
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
		Name:        "webui",
		Usage:       "Starts the webui",
		Description: "Starts the webui installer",
		Aliases:     []string{"w"},
		Action: func(c *cli.Context) error {
			return webui.Start(context.Background())
			//return nil
		},
	},
	{
		Name:        "config",
		Usage:       "get machine configuration",
		Description: "Print machine state information, e.g. `state get uuid` returns the machine uuid",
		Aliases:     []string{"c"},
		Action: func(c *cli.Context) error {
			runtime, err := state.NewRuntime()
			if err != nil {
				return err
			}

			fmt.Print(runtime)
			return err
		},
		Subcommands: []*cli.Command{
			{
				Name:        "show",
				Usage:       "Shows the machine configuration",
				Description: "Show the runtime configuration of the machine. It will scan the machine for all the configuration and will return the config file processed and found.",
				Aliases:     []string{"s"},
				Action: func(c *cli.Context) error {
					config, err := config.Scan(config.Directories(configScanDir...), config.NoLogs)
					if err != nil {
						return err
					}

					fmt.Printf("%s", config.String())
					return nil
				},
			},
			{
				Name:  "get",
				Usage: "Get specific data from the configuration",
				UsageText: `
Use it to retrieve configuration programmatically from the CLI:

$ kairos-agent config get k3s.enabled
true

or

$ kairos-agent config get k3s
enabled: true`,
				Description: "It allows to navigate the YAML config file by searching with 'yq' style keywords as `config get k3s` to retrieve the k3s config block",
				Aliases:     []string{"g"},
				Action: func(c *cli.Context) error {
					config, err := config.Scan(config.Directories(configScanDir...), config.NoLogs, config.StrictValidation(c.Bool("strict-validation")))
					if err != nil {
						return err
					}

					res, err := config.Query(c.Args().First())
					if err != nil {
						return err
					}
					fmt.Printf("%s", res)
					return nil
				},
			},
		},
	},
	{
		Name:        "state",
		Usage:       "get machine state",
		Description: "Print machine state information, e.g. `state get uuid` returns the machine uuid",
		Aliases:     []string{"s"},
		Action: func(c *cli.Context) error {
			runtime, err := state.NewRuntime()
			if err != nil {
				return err
			}

			fmt.Print(runtime)
			return err
		},
		Subcommands: []*cli.Command{
			{
				Name:        "apply",
				Usage:       "Applies a machine state",
				Description: "Applies machine configuration in runtimes",
				Aliases:     []string{"a"},
				Action: func(c *cli.Context) error {
					// TODO
					return nil
				},
			},
			{
				Name:        "get",
				Usage:       "get specific ",
				Description: "query state data",
				Aliases:     []string{"g"},
				Action: func(c *cli.Context) error {
					runtime, err := state.NewRuntime()
					if err != nil {
						return err
					}

					res, err := runtime.Query(c.Args().First())
					fmt.Print(res)
					return err
				},
			},
		},
	},

	{
		Name: "interactive-install",
		Description: `
Starts kairos in interactive mode install.

It will ask prompt for several questions and perform an install depending on the providers available in the system.

See also https://kairos.io/installation/interactive_install/ for documentation.

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
		Name:  "manual-install",
		Usage: "Starts the manual installation",
		Description: `
`,
		Aliases: []string{"m"},
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name: "device",
			},
			&cli.BoolFlag{
				Name: "poweroff",
			},
			&cli.BoolFlag{
				Name: "reboot",
			},
		},
		Action: func(c *cli.Context) error {
			if c.NArg() == 0 {
				return fmt.Errorf("expect one argument. the config file - if you don't have it, use the interactive-install")
			}
			config := c.Args().First()

			options := map[string]string{"device": c.String("device")}

			if c.Bool("poweroff") {
				options["poweroff"] = "true"
			}

			if c.Bool("reboot") {
				options["reboot"] = "true"
			}

			return agent.ManualInstall(config, options, c.Bool("strict-validation"))
		},
	},

	{
		Name:  "install",
		Usage: "Starts the kairos pairing installation",
		Description: `
Starts kairos in pairing mode.

It will print out a QR code which can be used with "kairos register" to send over a configuration and bootstraping a kairos node.

See also https://kairos.io/docs/installation/qrcode/ for documentation.

This command is meant to be used from the boot GRUB menu, but can be started manually`,
		Aliases: []string{"i"},
		Action: func(c *cli.Context) error {
			return agent.Install(configScanDir...)
		},
	},
	{
		Name:    "recovery",
		Aliases: []string{"r"},
		Action: func(c *cli.Context) error {
			return agent.Recovery()
		},
		Usage: "Starts kairos recovery mode",
		Description: `
Starts kairos recovery mode.

In recovery mode a QR code will be printed out on the screen which should be used in conjunction with "kairos bridge". Pass by the QR code as snapshot
to the bridge to connect over the machine which runs the "kairos recovery" command.

See also https://kairos.io/after_install/recovery_mode/ for documentation.

This command is meant to be used from the boot GRUB menu, but can likely be used standalone`,
	},

	{
		Name: "reset",
		Action: func(c *cli.Context) error {
			return agent.Reset(configScanDir...)
		},
		Usage: "Starts kairos reset mode",
		Description: `
Starts kairos reset mode, it will nuke completely the node data and restart fresh.
Attention ! this will delete any persistent data on the node. It is equivalent to re-init the node right after the installation.

In reset mode a the node will automatically reset

See also https://kairos.io/after_install/reset_mode/ for documentation.

This command is meant to be used from the boot GRUB menu, but can likely be used standalone`,
	},
	{
		Name: "validate",
		Action: func(c *cli.Context) error {
			config := c.Args().First()
			return agent.Validate(config)
		},
		Usage: "Validates a cloud config file",
		Description: `
The validate command expects a configuration file as its only argument. Local files and URLs are accepted.
		`,
	},
	{
		Name: "print-schema",
		Action: func(c *cli.Context) error {

			json, err := agent.JSONSchema(common.VERSION)

			if err != nil {
				return err
			}

			fmt.Println(json)

			return nil
		},
		Usage:       "Print out Kairos' Cloud Configuration JSON Schema",
		Description: `Prints out Kairos' Cloud Configuration JSON Schema`,
	},
}

func main() {
	bus.Manager.Initialize()

	app := &cli.App{
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:    "strict-validation",
				Usage:   "Fail instead of warn on validation errors.",
				EnvVars: []string{"STRICT_VALIDATIONS"},
			},
		},
		Name:    "kairos-agent",
		Version: common.VERSION,
		Authors: []*cli.Author{
			{
				Name: "Ettore Di Giacinto",
			},
		},
		Usage: "kairos agent start",
		Description: `
The kairos agent is a component to abstract away node ops, providing a common feature-set across kairos variants.
`,
		UsageText: ``,
		Copyright: "kairos authors",

		Commands: cmds,
	}

	err := app.Run(os.Args)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
