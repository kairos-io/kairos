package main

import (
	//"fmt"

	"fmt"
	"os"

	cmd "github.com/c3os-io/c3os/internal/cmd"

	"github.com/urfave/cli"
)

var cliCmd = []cli.Command{
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
}

func main() {

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

		Commands: cmd.CommonCommand(cliCmd...),
	}

	err := app.Run(os.Args)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
