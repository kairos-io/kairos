package cmd

import (
	"fmt"
	"strings"

	"github.com/kairos-io/AuroraBoot/internal"
	"github.com/kairos-io/AuroraBoot/pkg/ops"
	"github.com/kairos-io/AuroraBoot/pkg/schema"
	"github.com/kairos-io/kairos/v4/sdk/types/logger"
	"github.com/urfave/cli/v2"
)

var StartPixieCmd = cli.Command{
	Name:    "start-pixie",
	Aliases: []string{"sp"},
	Usage:   "Start the Pixiecore netboot server and serve custom PXE files (kernel, initrd, squashfs) with a cloud-config.",
	Description: `Start a Pixiecore-based PXE server to serve a kernel, initrd, and squashfs image for network booting.

Arguments:
  cloud-config-file   Path to the cloud-init or cloud-config YAML file, or ""
                       to boot without serving one over config_url. An empty
                       value is only safe when the booted image already has
                       everything it needs baked in from build time; a fully
                       unconfigured image will boot but never install itself.
  squashfs-file       Path to the root filesystem squashfs image.
  address             IP address to bind the server (e.g., 0.0.0.0).
  port                Port for the netboot server (e.g., 8080).
  initrd-file         Path to the initrd image.
  kernel-file         Path to the kernel image.

Options:
  --debug             Enable debug logging for troubleshooting.
  --grub-cfg          Path to the livecd grub config, so the netboot cmdline matches the ISO.

Options go before the positional arguments. Placed after them they are read as
positional arguments themselves, not as flags.

Example:
  start-pixie --debug user-data.yaml rootfs.squashfs 0.0.0.0 8080 initrd.img vmlinuz
  start-pixie --debug "" rootfs.squashfs 0.0.0.0 8080 initrd.img vmlinuz
  start-pixie --grub-cfg kairos-grub.cfg user-data.yaml rootfs.squashfs 0.0.0.0 8080 initrd.img vmlinuz
`,
	ArgsUsage: "<cloud-config-file|\"\"> <squashfs-file> <address> <port> <initrd-file> <kernel-file>",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "grub-cfg",
			Usage: "Path to the livecd grub config extracted by the netboot command, to boot with the same cmdline as the ISO",
		},
		&cli.BoolFlag{
			Name:  "debug",
			Usage: "Enable debug logging",
		},
		// Add more flags for optional NetBoot struct fields as needed
	},
	Action: func(c *cli.Context) error {
		cloudConfigFile := c.Args().Get(0)
		squashFSfile := c.Args().Get(1)
		address := c.Args().Get(2)
		netbootPort := c.Args().Get(3)
		initrdFile := c.Args().Get(4)
		kernelFile := c.Args().Get(5)

		// Checked before the required-argument check, so the message names the
		// mistake that was actually made rather than the count it causes.
		if err := ValidateNoFlagsAfterArgs(c.Args().Slice()); err != nil {
			cli.ShowCommandHelp(c, c.Command.Name)
			fmt.Println("")
			return err
		}

		if err := ValidateStartPixieArgs(squashFSfile, address, netbootPort, initrdFile, kernelFile); err != nil {
			cli.ShowCommandHelp(c, c.Command.Name)
			fmt.Println("")
			return err
		}

		loglevel := "info"
		if c.Bool("debug") {
			loglevel = "debug"
		}
		internal.Log = logger.NewKairosLogger("AuroraBoot", loglevel, false)

		if cloudConfigFile == "" {
			internal.Log.Logger.Warn().Msg("no cloud-config-file given: netbooting without config_url. " +
				"This is only safe if the image already has everything it needs baked in from build time -- " +
				"an image with no cloud-config attached at all will boot but never install itself.")
		}

		// Optionally parse NetBoot from flags here if desired
		nb := schema.NetBoot{} // Use defaults, or parse from CLI flags

		f := ops.StartPixiecore(
			cloudConfigFile,
			address,
			netbootPort,
			func() string { return squashFSfile }, // Wrap squashFSfile in a function
			func() string { return initrdFile },   // Wrap initrdFile in a function
			func() string { return kernelFile },   // Wrap kernelFile in a function
			func() string { return c.String("grub-cfg") },
			nb,
		)

		return f(c.Context)
	},
}

// ValidateStartPixieArgs checks the required positional arguments.
// cloudConfigFile is deliberately not one of them: it becomes config_url in
// the boot cmdline (pkg/ops/netboot.go), and an empty config_url is valid
// when the image doesn't need one fetched at netboot time -- the netboot
// manager relies on this (see internal/netbootmgr/manager.go's Start).
// Split out from Action so it can be unit-tested without going anywhere near
// the real server start, which binds a raw socket and blocks on network I/O.
func ValidateStartPixieArgs(squashFSfile, address, netbootPort, initrdFile, kernelFile string) error {
	if squashFSfile == "" || address == "" || netbootPort == "" || initrdFile == "" || kernelFile == "" {
		return fmt.Errorf("all arguments except cloud-config-file are required")
	}
	return nil
}

// ValidateNoFlagsAfterArgs rejects a flag that was written after the
// positional arguments.
//
// urfave/cli v2 stops parsing flags at the first positional argument, so
// `start-pixie ... vmlinuz --grub-cfg x` leaves --grub-cfg in Args() and the
// flag reads as unset. The command then netboots with the default cmdline and
// says nothing about it, which is the minor half of the QA failure on
// kairos-io/kairos#2573. None of this command's positional arguments (paths,
// an address, a port) legitimately starts with a dash, so treating one as a
// misplaced flag is unambiguous.
func ValidateNoFlagsAfterArgs(args []string) error {
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			return fmt.Errorf("%q came after the positional arguments, where it is not parsed as a flag: put options before <cloud-config-file>", a)
		}
	}
	return nil
}
