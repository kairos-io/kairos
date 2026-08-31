package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/urfave/cli/v2"

	nodepair "github.com/kairos-io/go-nodepair"
	qr "github.com/kairos-io/go-nodepair/qrcode"
)

// RegisterCMD is only used temporarily to avoid duplication while the kairosctl sub-command is deprecated.
func RegisterCMD(toolName string) *cli.Command {
	subCommandName := "register"
	fullName := fmt.Sprintf("%s %s", toolName, subCommandName)
	usage := "Registers and bootstraps a node"
	description := fmt.Sprintf(` 
		Bootstraps a node which is started in pairing mode. It can send over a configuration file used to install the kairos node.

		For example:
		$ %s --config config.yaml --device /dev/sda ~/Downloads/screenshot.png

		will decode the QR code from ~/Downloads/screenshot.png and bootstrap the node remotely.

		If the image is omitted, a screenshot will be taken and used to decode the QR code.

		See also https://kairos.io/docs/getting-started/ for documentation.
		`, fullName)
	if toolName != "kairosctl" {
		usage += " (WARNING: this command will be deprecated in the next release, use the kairosctl binary instead)"
		description = "\t\tWARNING: This command will be deprecated in the next release. Please use the new kairosctl binary to register your nodes.\n" + description
	}

	return &cli.Command{
		Name:        subCommandName,
		UsageText:   fmt.Sprintf("%s --reboot --device /dev/sda /image/snapshot.png", fullName),
		Usage:       usage,
		Description: description,
		ArgsUsage:   "Register optionally accepts an image. If nothing is passed will take a screenshot of the screen and try to decode the QR code",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "config",
				Usage:    "Kairos YAML configuration file",
				Required: true,
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
			var ref string
			if c.Args().Len() == 1 {
				ref = c.Args().First()
			}

			return register(c.String("log-level"), ref, c.String("config"), c.String("device"), c.Bool("reboot"), c.Bool("poweroff"))
		},
	}
}

// isDirectory determines if a file represented
// by `path` is a directory or not.
func isDirectory(path string) (bool, error) {
	fileInfo, err := os.Stat(path)
	if err != nil {
		return false, err
	}

	return fileInfo.IsDir(), err
}

func isReadable(fileName string) bool {
	file, err := os.Open(fileName)
	if err != nil {
		if os.IsPermission(err) {
			return false
		}
	}
	file.Close()
	return true
}

func register(loglevel, arg, configFile, device string, reboot, poweroff bool) error {
	b, _ := os.ReadFile(configFile)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if arg != "" {
		isDir, err := isDirectory(arg)
		if err == nil && isDir {
			return fmt.Errorf("cannot register with a directory, please pass a file")
		} else if err != nil {
			return err
		}
		if !isReadable(arg) {
			return fmt.Errorf("cannot register with a file that is not readable")
		}
	}
	// dmesg -D to suppress tty ev
	fmt.Println("Sending registration payload, please wait")

	config := map[string]string{
		"device": device,
		"cc":     string(b),
	}

	if reboot {
		config["reboot"] = ""
	}

	if poweroff {
		config["poweroff"] = ""
	}

	err := nodepair.Send(
		ctx,
		config,
		nodepair.WithReader(qr.Reader),
		nodepair.WithToken(arg),
		nodepair.WithLogLevel(loglevel),
	)
	if err != nil {
		return err
	}

	fmt.Println("Payload sent, installation will start on the machine briefly")
	return nil
}
