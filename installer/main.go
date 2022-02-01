package main

import (
	//"fmt"

	"bufio"
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"strings"
	"time"

	nodepair "github.com/mudler/go-nodepair"
	qr "github.com/mudler/go-nodepair/qrcode"
	"github.com/urfave/cli"
)

func optsToArgs(options map[string]string) (res []string) {
	for k, v := range options {
		if k != "device" && k != "cc" && k != "reboot" {
			res = append(res, fmt.Sprintf("--%s", k))
			res = append(res, fmt.Sprintf("%s", v))
		}
	}
	return
}

func runInstall(options map[string]string) {
	fmt.Println("Running install", options)
	f, _ := ioutil.TempFile("", "xxxx")

	device, ok := options["device"]
	if !ok {
		fmt.Println("device must be specified among options")
		os.Exit(1)
	}

	cloudInit, ok := options["cc"]
	if !ok {
		fmt.Println("cloudInit must be specified among options")
		os.Exit(1)
	}

	_, reboot := options["reboot"]

	ioutil.WriteFile(f.Name(), []byte(cloudInit), os.ModePerm)
	args := []string{}
	args = append(args, optsToArgs(options)...)
	args = append(args, "-c", f.Name(), fmt.Sprintf("%s", device))

	cmd := exec.Command("cos-installer", args...)
	cmd.Env = os.Environ()
	cmd.Stdout = os.Stdout
	cmd.Stdin = os.Stdin
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	if reboot {
		exec.Command("reboot").Start()
	}
}

func prompt(t string) (string, error) {
	fmt.Println(t)
	answer, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(answer), nil
}

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
				Name:    "install",
				Aliases: []string{"i"},
				Action: func(c *cli.Context) error {

					tk := nodepair.GenerateToken()
					fmt.Println("Starting p2p device installation enrollment")
					fmt.Println("a QR code will be displayed below.")
					fmt.Println("In another machine, run `c3os register` with the QR code visible on screen, ")
					fmt.Println("or `c3os register <file>` to register the machine from a photo.")
					fmt.Println("IF the qrcode is not displaying correctly,")
					fmt.Println("try booting with another vga option from the boot cmdline (e.g. vga=791).")
					fmt.Println("Starting in 5 seconds...")
					time.Sleep(5 * time.Second)

					qr.Print(tk)
					time.Sleep(5 * time.Second)

					r := map[string]string{}
					ctx, cancel := context.WithCancel(context.Background())
					defer cancel()

					go func() {
						prompt("p2p device enrollment started, press any key to abort pairing and drop to shell. To re-start enrollment, run 'c3os install'")
						// give tty1 back
						exec.Command("systemctl", "start", "--no-block", "getty@tty1").Start()
						cancel()
					}()

					if err := nodepair.Receive(ctx, &r, nodepair.WithToken(tk)); err != nil {
						return err
					}

					if len(r) == 0 {
						return errors.New("no configuration, stopping installation")
					}

					runInstall(r)

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
