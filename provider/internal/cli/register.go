package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/urfave/cli/v2"

	nodepair "github.com/kairos-io/go-nodepair"
	qr "github.com/kairos-io/go-nodepair/qrcode"
)

// defaultPairingTimeout bounds the wait for the node to appear on the pairing
// network. nodepair.Send blocks until the context ends, so without a deadline
// a node that never appears leaves this command printing progress forever,
// with no exit and no error. Pass --timeout 0 to wait indefinitely, which is
// what the command did before the flag existed.
const defaultPairingTimeout = 15 * time.Minute

// RegisterCMD builds the register command under the given tool name, which is
// only used to render usage text so the examples name whatever entrypoint the
// reader actually reached this command through.
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
			&cli.DurationFlag{
				Name:  "timeout",
				Usage: "Give up if the node has not paired within this duration. 0 waits forever.",
				Value: defaultPairingTimeout,
			},
		},
		Action: func(c *cli.Context) error {
			var ref string
			if c.Args().Len() == 1 {
				ref = c.Args().First()
			}

			return register(c.String("log-level"), ref, c.String("config"), c.String("device"), c.Bool("reboot"), c.Bool("poweroff"), c.Duration("timeout"))
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

func register(loglevel, arg, configFile, device string, reboot, poweroff bool, timeout time.Duration) error {
	b, _ := os.ReadFile(configFile)
	ctx, cancel := pairingContext(timeout)
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

	// nodepair.Send returns nil when the context ends, so an expired deadline
	// is indistinguishable from a delivered payload unless the context is
	// examined here.
	if ctx.Err() != nil {
		return fmt.Errorf("the node did not pair within %s. Check that it is booted in pairing mode and reachable on the same network, then retry with a longer --timeout", timeout)
	}

	fmt.Println("Payload sent, installation will start on the machine briefly")
	return nil
}

// pairingContext returns a context bounded by timeout, or an unbounded one
// when timeout is zero or negative.
func pairingContext(timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return context.WithCancel(context.Background())
	}

	return context.WithTimeout(context.Background(), timeout)
}
