package main

import (
	//"fmt"

	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/mudler/c3os/installer/systemd"
	edgeVPNClient "github.com/mudler/edgevpn/api/client"
	service "github.com/mudler/edgevpn/api/client/service"
	"github.com/mudler/edgevpn/pkg/node"
	nodepair "github.com/mudler/go-nodepair"
	qr "github.com/mudler/go-nodepair/qrcode"
	"github.com/pterm/pterm"
	"github.com/urfave/cli"
)

func main() {
	app := &cli.App{
		Name:        "c3os",
		Version:     "0.1",
		Author:      "Ettore Di Giacinto",
		Usage:       "c3os (register|install)",
		Description: "c3os registers and installs c3os boxes",
		UsageText:   ``,
		Copyright:   "Ettore Di Giacinto",

		Commands: []cli.Command{
			{
				Name: "register",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name: "config",
					},
					&cli.StringFlag{
						Name: "device",
					},
					&cli.BoolFlag{
						Name: "reboot",
					},
				},
				Action: func(c *cli.Context) error {
					args := c.Args()
					var ref string
					if len(args) == 1 {
						ref = args[0]
					}

					b, _ := ioutil.ReadFile(c.String("config"))
					ctx, cancel := context.WithCancel(context.Background())
					defer cancel()
					// dmesg -D to suppress tty ev

					fmt.Println("Sending registration payload, please wait")

					config := map[string]string{
						"device": c.String("device"),
						"cc":     string(b),
					}

					if c.Bool("reboot") {
						config["reboot"] = ""
					}

					err := nodepair.Send(
						ctx,
						config,
						nodepair.WithReader(qr.Reader),
						nodepair.WithToken(ref),
					)
					if err != nil {
						return err
					}

					fmt.Println("Payload sent, installation will start on the machine briefly")

					return nil
				},
			},
			{
				Name:      "generate-token",
				Aliases:   []string{"g"},
				UsageText: "Generate a network token",
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
				Name:      "setup",
				Aliases:   []string{"s"},
				UsageText: "Automatically setups the node",
				Action: func(c *cli.Context) error {
					dir := "/oem"
					args := c.Args()
					if len(args) > 0 {
						dir = args[0]
					}

					return setup(dir)
				},
			},
			{
				Name: "get-kubeconfig",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:  "api",
						Value: "localhost:8080",
					},
				},
				Action: func(c *cli.Context) error {
					cc := service.NewClient(
						"c3os",
						edgeVPNClient.NewClient(edgeVPNClient.WithHost(fmt.Sprintf("http://%s", c.String("api")))))
					str, _ := cc.Get("kubeconfig", "master")
					b, _ := base64.URLEncoding.DecodeString(str)
					masterIP, _ := cc.Get("master", "ip")
					fmt.Println(strings.ReplaceAll(string(b), "127.0.0.1", masterIP))
					return nil
				},
			},
			{
				Name:    "install",
				Aliases: []string{"i"},
				Action: func(c *cli.Context) error {

					// Reads config, and if present and offline is defined,
					// runs the installation
					cc, err := ScanConfig("/oem")
					if err == nil && cc.C3OS != nil && cc.C3OS.Offline {
						runInstall(map[string]string{
							"device": cc.C3OS.Device,
							"cc":     cc.cloudFileContent,
						})
						if cc.C3OS.Reboot {
							Reboot()
						} else {
							svc, err := systemd.Getty(1)
							if err == nil {
								svc.Start()
							}
						}
						return nil
					}

					printBanner(banner)
					tk := nodepair.GenerateToken()

					pterm.DefaultBox.WithTitle("Installation").WithTitleBottomRight().WithRightPadding(0).WithBottomPadding(0).Println(
						`Welcome to c3os!
p2p device installation enrollment is starting.
A QR code will be displayed below. 
In another machine, run "c3os register" with the QR code visible on screen,
or "c3os register <file>" to register the machine from a photo.
IF the qrcode is not displaying correctly,
try booting with another vga option from the boot cmdline (e.g. vga=791).`)

					pterm.Info.Println("Starting in 5 seconds...")
					pterm.Print("\n\n") // Add two new lines as spacer.

					time.Sleep(5 * time.Second)

					qr.Print(tk)

					r := map[string]string{}
					ctx, cancel := context.WithCancel(context.Background())
					defer cancel()

					go func() {
						prompt("Waiting for registration, press any key to abort pairing. To restart run 'c3os install'.")
						// give tty1 back
						svc, err := systemd.Getty(1)
						if err == nil {
							svc.Start()
						}
						cancel()
					}()

					if err := nodepair.Receive(ctx, &r, nodepair.WithToken(tk)); err != nil {
						return err
					}

					if len(r) == 0 {
						return errors.New("no configuration, stopping installation")
					}

					pterm.Info.Println("Starting installation")
					runInstall(r)

					pterm.Info.Println("Installation completed, press enter to go back to the shell.")
					return nil
				},
			},
		},
	}

	err := app.Run(os.Args)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
