package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/kairos-io/AuroraBoot/internal"
	"github.com/kairos-io/AuroraBoot/pkg/ops"
	"github.com/kairos-io/kairos/v4/sdk/types/logger"
	"github.com/urfave/cli/v2"
)

var UnpackCmd = cli.Command{
	Name:    "unpack",
	Aliases: []string{"pull"},
	Usage:   "Unpack a container image to a directory",
	Description: `Unpack extracts a container image to a local directory. It supports pulling
images for a specific architecture, which is useful for cross-platform builds.

Examples:
  # Unpack an image to a directory (uses host architecture)
  auroraboot unpack quay.io/kairos/ubuntu:latest /tmp/rootfs

  # Unpack an arm64 image on an amd64 host
  auroraboot unpack --arch arm64 quay.io/kairos/ubuntu:latest /tmp/rootfs

  # Unpack with debug logging
  auroraboot --debug unpack --arch arm64 quay.io/kairos/ubuntu:latest /tmp/rootfs
`,
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "arch",
			Usage: "Architecture to pull (amd64, arm64, or riscv64). Defaults to host architecture if not specified.",
		},
		&cli.StringFlag{
			Name:    "loglevel",
			Aliases: []string{"l"},
			Usage:   "Set the log level",
			Value:   "info",
		},
		AllowInsecureRegistriesFlag,
	},
	ArgsUsage: "<image> <destination>",
	Action: func(ctx *cli.Context) error {
		internal.Log = logger.NewKairosLogger("aurora", ctx.String("loglevel"), false)

		if ctx.NArg() < 2 {
			cli.ShowCommandHelp(ctx, ctx.Command.Name)
			fmt.Println("")
			return errors.New("requires <image> and <destination> arguments")
		}

		image := ctx.Args().Get(0)
		destination := ctx.Args().Get(1)

		// Validate arch flag if provided
		arch := ctx.String("arch")
		if arch != "" {
			validArchs := []string{"amd64", "arm64", "riscv64"}
			isValid := false
			for _, valid := range validArchs {
				if arch == valid {
					isValid = true
					break
				}
			}
			if !isValid {
				return fmt.Errorf("invalid architecture '%s': must be 'amd64', 'arm64', or 'riscv64'", arch)
			}
		}

		// Create destination directory if it doesn't exist
		if err := os.MkdirAll(destination, 0755); err != nil {
			return fmt.Errorf("failed to create destination directory: %w", err)
		}

		internal.Log.Logger.Info().
			Str("image", image).
			Str("destination", destination).
			Str("arch", arch).
			Msg("Unpacking container image")

		// Use the existing DumpSource function which already supports arch
		dumpFn := ops.DumpSource(image, func() string { return destination }, arch, ctx.Bool("allow-insecure-registries"))
		if err := dumpFn(ctx.Context); err != nil {
			return fmt.Errorf("failed to unpack image: %w", err)
		}

		internal.Log.Logger.Info().
			Str("image", image).
			Str("destination", destination).
			Msg("Successfully unpacked container image")

		return nil
	},
}
