package cli

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/ipfs/go-log/v2"
	qr "github.com/kairos-io/go-nodepair/qrcode"
	"github.com/kairos-io/kairos/v4/sdk/utils"
	"github.com/mudler/edgevpn/api"
	"github.com/mudler/edgevpn/cmd"
	"github.com/mudler/edgevpn/pkg/logger"
	"github.com/mudler/edgevpn/pkg/node"
	"github.com/mudler/edgevpn/pkg/services"
	"github.com/mudler/edgevpn/pkg/vpn"
	"github.com/urfave/cli/v2"
)

func BridgeCMD(toolName string) *cli.Command {
	usage := "Connect to a kairos VPN network"
	description := `
		Starts a bridge with a kairos network or a node.

		# With a network

		By default, "bridge" will create a VPN network connection to the node with the token supplied, thus it requires elevated permissions in order to work.

		For example:

		$ sudo %s bridge --token <TOKEN>

		Will start a VPN, which local ip is fixed to 10.1.0.254 (tweakable with --address).

		The API will also be accessible at http://127.0.0.1:8080

		# With a node

		"%s bridge" can be used also to connect over to a node in recovery mode. When operating in this modality kairos bridge requires no specific permissions, indeed a tunnel
		will be created locally to connect to the machine remotely.

		For example:

		$ %s bridge --qr-code-image /path/to/image.png

		Will scan the QR code in the image and connect over. Further instructions on how to connect over will be printed out to the screen.

		See also: https://kairos.io/docs/reference/recovery_mode/

		`

	if toolName != "kairosctl" {
		usage += " (WARNING: this command will be deprecated in the next release, use the kairosctl binary instead)"
		description = "\t\tWARNING: This command will be deprecated in the next release. Please use the new kairosctl binary instead.\n" + description
	}

	flags := []cli.Flag{
		&cli.BoolFlag{
			Name:     "qr-code-snapshot",
			Required: false,
			Usage:    "Bool to take a local snapshot instead of reading from an image file for recovery",
			EnvVars:  []string{"QR_CODE_SNAPSHOT"},
		},
		&cli.StringFlag{
			Name:     "qr-code-image",
			Usage:    "Path to an image containing a valid QR code for recovery mode",
			Required: false,
			EnvVars:  []string{"QR_CODE_IMAGE"},
		},
		&cli.StringFlag{
			Name:  apiFlagName,
			Value: "127.0.0.1:8080",
			Usage: "Listening API url",
		},
		&cli.BoolFlag{
			Name:    "dhcp",
			EnvVars: []string{"DHCP"},
			Usage:   "Enable DHCP",
		},
		&cli.StringFlag{
			Value:   "10.1.0.254/24",
			Name:    "address",
			EnvVars: []string{"ADDRESS"},
			Usage:   "Specify an address for the bridge",
		},
		&cli.StringFlag{
			Value:   "/tmp/kairos",
			Name:    "lease-dir",
			EnvVars: []string{"lease-dir"},
			Usage:   "DHCP Lease directory",
		},
		&cli.StringFlag{
			Name:    "interface",
			Usage:   "Interface name",
			Value:   "kairos0",
			EnvVars: []string{"IFACE"},
		},
	}

	flags = append(flags, cmd.CommonFlags...)

	return &cli.Command{
		Name:        "bridge",
		UsageText:   fmt.Sprintf("%s %s", toolName, "bridge --token XXX"),
		Usage:       usage,
		Description: fmt.Sprintf(description, toolName, toolName, toolName),
		Flags:       flags,
		Action:      bridge,
	}
}

// bridge is just starting a VPN with edgevpn to the given network token.
func bridge(c *cli.Context) error {
	qrCodePath := ""
	fromQRCode := false
	var serviceUUID, sshPassword string

	if c.String("qr-code-image") != "" {
		qrCodePath = c.String("qr-code-image")
		fromQRCode = true
	}
	if c.Bool("qr-code-snapshot") {
		qrCodePath = ""
		fromQRCode = true
	}

	if fromQRCode {
		recoveryToken := qr.Reader(qrCodePath)
		data := utils.DecodeRecoveryToken(recoveryToken)
		if len(data) != 3 {
			fmt.Println("Token not decoded correctly")
			return fmt.Errorf("invalid token")
		}
		token := data[0]
		serviceUUID = data[1]
		sshPassword = data[2]
		if serviceUUID == "" || sshPassword == "" || token == "" {
			return fmt.Errorf("decoded invalid values")
		}

		err := c.Set("token", token)
		if err != nil {
			return err
		}
	}

	ctx := context.Background()

	nc := cmd.ConfigFromContext(c)

	lvl, err := log.LevelFromString(nc.LogLevel)
	if err != nil {
		lvl = log.LevelError
	}
	llger := logger.New(lvl)

	o, vpnOpts, err := nc.ToOpts(llger)
	if err != nil {
		llger.Fatal(err.Error())
	}

	opts := []node.Option{}

	if !fromQRCode {
		// We just connect to a VPN token
		o = append(o,
			services.Alive(
				time.Duration(20)*time.Second,
				time.Duration(10)*time.Second,
				time.Duration(10)*time.Second)...)

		if c.Bool("dhcp") {
			// Adds DHCP server
			address, _, err := net.ParseCIDR(c.String("address"))
			if err != nil {
				return err
			}
			nodeOpts, vO := vpn.DHCP(llger, 15*time.Minute, c.String("lease-dir"), address.String())
			o = append(o, nodeOpts...)
			vpnOpts = append(vpnOpts, vO...)
		}

		opts, err = vpn.Register(vpnOpts...)
		if err != nil {
			return err
		}
	} else {
		// We hook into a service
		llger.Info("Connecting to service", serviceUUID)
		llger.Info("SSH access password is", sshPassword)
		llger.Info("SSH server reachable at 127.0.0.1:2200")
		opts = append(opts, node.WithNetworkService(
			services.ConnectNetworkService(
				30*time.Second,
				serviceUUID,
				"127.0.0.1:2200",
			),
		))
		llger.Info("To connect, keep this terminal open and run in another terminal 'ssh 127.0.0.1 -p 2200' the password is ", sshPassword)
		llger.Info("Note: the connection might not be available instantly and first attempts will likely fail.")
		llger.Info("      Few attempts might be required before establishing a tunnel to the host.")
	}

	e, err := node.New(append(o, opts...)...)
	if err != nil {
		return err
	}

	go api.API(ctx, c.String(apiFlagName), 5*time.Second, 20*time.Second, e, nil, false) //nolint:errcheck

	return e.Start(ctx)
}
