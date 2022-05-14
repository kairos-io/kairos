package main

import (
	//"fmt"

	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	config "github.com/c3os-io/c3os/cli/config"
	"github.com/c3os-io/c3os/cli/github"
	edgeVPNClient "github.com/mudler/edgevpn/api/client"
	service "github.com/mudler/edgevpn/api/client/service"
	"github.com/mudler/edgevpn/pkg/node"
	"github.com/urfave/cli"
	"gopkg.in/yaml.v2"
)

func main() {

	networkApi := []cli.Flag{
		&cli.StringFlag{
			Name:  "api",
			Usage: "API Address",
			Value: "http://localhost:8080",
		},
		&cli.StringFlag{
			Name:  "network-id",
			Value: "c3os",
			Usage: "Kubernetes Network Deployment ID",
		},
	}
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

		Commands: []cli.Command{
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
				Name:      "register",
				UsageText: "register --reboot --device /dev/sda /image/snapshot.png",
				Usage:     "Registers and bootstraps a node",
				Description: `
Bootstraps a node which is started in pairing mode. It can send over a configuration file used to install the c3os node.

For example:
$ c3os register --config config.yaml --device /dev/sda ~/Downloads/screenshot.png

will decode the QR code from ~/Downloads/screenshot.png and bootstrap the node remotely.

If the image is omitted, a screenshot will be taken and used to decode the QR code.

See also https://docs.c3os.io/installation/device_pairing/ for documentation.
`,
				ArgsUsage: "Register optionally accepts an image. If nothing is passed will take a screenshot of the screen and try to decode the QR code",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:  "config",
						Usage: "C3OS YAML configuration file",
					},
					&cli.StringFlag{
						Name:  "device",
						Usage: "Device used for the installation target",
					},
					&cli.BoolFlag{
						Name:  "reboot",
						Usage: "Reboot node after installation",
					},
					&cli.BoolFlag{
						Name:  "poweroff",
						Usage: "Shutdown node after installation",
					},
					&cli.StringFlag{
						Name:  "log-level",
						Usage: "Set log level",
					},
				},

				Action: func(c *cli.Context) error {
					args := c.Args()
					var ref string
					if len(args) == 1 {
						ref = args[0]
					}

					return register(c.String("log-level"), ref, c.String("config"), c.String("device"), c.Bool("reboot"), c.Bool("poweroff"))
				},
			},
			{
				Name:      "create-config",
				Aliases:   []string{"c"},
				UsageText: "Create a config with a generated network token",

				Usage: "Creates a pristine config file",
				Description: `
Prints a vanilla YAML configuration on screen which can be used to bootstrap a c3os network.
`,
				ArgsUsage: "Optionally takes a token rotation interval (seconds)",

				Action: func(c *cli.Context) error {
					l := int(^uint(0) >> 1)
					args := c.Args()
					if len(args) > 0 {
						if i, err := strconv.Atoi(args[0]); err == nil {
							l = i
						}
					}
					cc := &config.Config{C3OS: &config.C3OS{NetworkToken: node.GenerateNewConnectionData(l).Base64()}}
					y, _ := yaml.Marshal(cc)
					fmt.Println(string(y))
					return nil
				},
			},
			{
				Name:      "generate-token",
				Aliases:   []string{"g"},
				UsageText: "Generate a network token",
				Usage:     "Creates a new token",
				Description: `
Generates a new token which can be used to bootstrap a c3os network.
`,
				ArgsUsage: "Optionally takes a token rotation interval (seconds)",

				Action: func(c *cli.Context) error {
					l := int(^uint(0) >> 1)
					args := c.Args()
					if len(args) > 0 {
						if i, err := strconv.Atoi(args[0]); err == nil {
							l = i
						}
					}
					fmt.Println(node.GenerateNewConnectionData(l).Base64())
					return nil
				},
			},
			{
				Name:      "agent",
				Usage:     "Starts the c3os agent",
				UsageText: "starts the agent",
				Description: `
Starts the c3os agent which automatically bootstrap and advertize to the c3os network.
`,
				Aliases: []string{"a"},
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
				Name:      "bridge",
				UsageText: "bridge --network-token XXX",
				Usage:     "Connect to a c3os VPN network",
				Description: `
Starts a bridge with a c3os network or a node. 

# With a network

By default, "bridge" will create a VPN network connection to the node with the token supplied, thus it requires elevated permissions in order to work.

For example:

$ sudo c3os bridge --network-token <TOKEN>

Will start a VPN, which local ip is fixed to 10.1.0.254 (tweakable with --address).

The API will also be accessible at http://127.0.0.1:8080

# With a node

"c3os bridge" can be used also to connect over to a node in recovery mode. When operating in this modality c3os bridge requires no specific permissions, indeed a tunnel
will be created locally to connect to the machine remotely.

For example:

$ c3os bridge --qr-code-image /path/to/image.png

Will scan the QR code in the image and connect over. Further instructions on how to connect over will be printed out to the screen.

See also: https://docs.c3os.io/after_install/troubleshooting/#connect-to-the-cluster-network and https://docs.c3os.io/after_install/recovery_mode/

`,
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:     "network-token",
						Required: false,
						EnvVar:   "NETWORK_TOKEN",
						Usage:    "Network token to connect over",
					},
					&cli.StringFlag{
						Name:     "log-level",
						Required: false,
						EnvVar:   "LOGLEVEL",
						Value:    "info",
						Usage:    "Bridge log level",
					},
					&cli.BoolFlag{
						Name:     "qr-code-snapshot",
						Required: false,
						Usage:    "Bool to take a local snapshot instead of reading from an image file for recovery",
						EnvVar:   "QR_CODE_SNAPSHOT",
					},
					&cli.StringFlag{
						Name:     "qr-code-image",
						Usage:    "Path to an image containing a valid QR code for recovery mode",
						Required: false,
						EnvVar:   "QR_CODE_IMAGE",
					},
					&cli.StringFlag{
						Name:  "api",
						Value: "127.0.0.1:8080",
						Usage: "Listening API url",
					},
					&cli.BoolFlag{
						Name:   "dhcp",
						EnvVar: "DHCP",
						Usage:  "Enable DHCP",
					},
					&cli.StringFlag{
						Value:  "10.1.0.254/24",
						Name:   "address",
						EnvVar: "ADDRESS",
						Usage:  "Specify an address for the bridge",
					},
					&cli.StringFlag{
						Value:  "/tmp/c3os",
						Name:   "lease-dir",
						EnvVar: "lease-dir",
						Usage:  "DHCP Lease directory",
					},
				},
				Action: bridge,
			},
			{
				Name:      "get-kubeconfig",
				Usage:     "Return a deployment kubeconfig",
				UsageText: "Retrieve a c3os network kubeconfig (only for automated deployments)",
				Description: `
Retrieve a network kubeconfig and prints out to screen.

If a deployment was bootstrapped with a network token, you can use this command to retrieve the master node kubeconfig of a network id.

For example:

$ c3os get-kubeconfig --network-id c3os
`,
				Flags: networkApi,
				Action: func(c *cli.Context) error {
					cc := service.NewClient(
						c.String("network-id"),
						edgeVPNClient.NewClient(edgeVPNClient.WithHost(c.String("api"))))
					str, _ := cc.Get("kubeconfig", "master")
					b, _ := base64.RawURLEncoding.DecodeString(str)
					masterIP, _ := cc.Get("master", "ip")
					fmt.Println(strings.ReplaceAll(string(b), "127.0.0.1", masterIP))
					return nil
				},
			},
			{
				Name:  "role",
				Usage: "Set or list node roles",
				Subcommands: []cli.Command{
					{
						Flags:     networkApi,
						Name:      "set",
						Usage:     "Set a node role",
						UsageText: "c3os role set <UUID> master",
						Description: `
Sets a node role propagating the setting to the network.

A role must be set prior to the node joining a network. You can retrieve a node UUID by running "c3os uuid".

Example:

$ (node A) c3os uuid
$ (node B) c3os role set <UUID of node A> master
`,
						Action: func(c *cli.Context) error {
							cc := service.NewClient(
								c.String("network-id"),
								edgeVPNClient.NewClient(edgeVPNClient.WithHost(c.String("api"))))
							return cc.Set("role", c.Args()[0], c.Args()[1])
						},
					},
					{
						Flags:       networkApi,
						Name:        "list",
						Description: "List node roles",
						Action: func(c *cli.Context) error {
							cc := service.NewClient(
								c.String("network-id"),
								edgeVPNClient.NewClient(edgeVPNClient.WithHost(c.String("api"))))
							advertizing, _ := cc.AdvertizingNodes()
							fmt.Println("Node\tRole")
							for _, a := range advertizing {
								role, _ := cc.Get("role", a)
								fmt.Printf("%s\t%s\n", a, role)
							}
							return nil
						},
					},
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
					cc, err := config.Scan(dirs...)
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
					fmt.Print(uuid())
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
					return install("/oem", "/usr/local/cloud-config")
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
		},
	}

	err := app.Run(os.Args)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
