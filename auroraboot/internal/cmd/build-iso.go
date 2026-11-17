package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/hashicorp/go-multierror"
	"github.com/kairos-io/AuroraBoot/internal"
	"github.com/kairos-io/kairos/v4/sdk/types/logger"

	"github.com/kairos-io/AuroraBoot/deployer"
	"github.com/kairos-io/AuroraBoot/internal/config"
	"github.com/kairos-io/AuroraBoot/pkg/constants"
	"github.com/kairos-io/AuroraBoot/pkg/extensions"
	"github.com/kairos-io/AuroraBoot/pkg/schema"
	"github.com/spectrocloud-labs/herd"
	"github.com/urfave/cli/v2"
)

var BuildISOCmd = cli.Command{
	Name:    "build-iso",
	Aliases: []string{"bi"},
	Usage:   "Builds an ISO from a container image or github release",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "cloud-config",
			Aliases: []string{"c"},
			Usage:   "The cloud config to embed in the ISO",
		},
		&cli.StringFlag{
			Name:    "override-name",
			Aliases: []string{"n"},
			Value:   "",
			Usage:   "Overrride default ISO file name",
		},
		&cli.StringFlag{
			Name:    "output",
			Aliases: []string{"o"},
			Value:   ".",
			Usage:   "Output directory for the ISO and its checksum (defaults to current directory)",
		},
		&cli.BoolFlag{
			Name:  "date",
			Value: false,
			Usage: "Adds a date suffix into the generated ISO file",
		},
		&cli.StringFlag{
			Name:  "overlay-rootfs",
			Usage: "Path of the overlayed rootfs data",
		},
		&cli.StringFlag{
			Name:  "overlay-iso",
			Usage: "Path of the overlayed iso data",
		},
		&cli.StringFlag{
			Name:    "loglevel",
			Aliases: []string{"l"},
			Usage:   "Set the log level",
			Value:   "info",
		},
		&cli.StringFlag{
			Name:  "arch",
			Usage: "Architecture to use when pulling container images (amd64, arm64, or riscv64). Defaults to host architecture if not specified.",
		},
		&cli.StringFlag{
			Name:  "extend-live-cmdline",
			Usage: "Append options to the kernel cmdline when booting from the live/installer ISO (e.g. rd.debug rd.shell). Does not affect the installed system.",
		},
		&cli.StringFlag{
			Name:  "live-console",
			Usage: "Replace the console options used when booting from the live/installer ISO",
		},
		&cli.StringFlag{
			Name:  "default-grub-entry",
			Usage: "Id (grub --id) of the live menu entry to boot when the grub timeout expires. One of " + strings.Join(constants.LiveGrubEntries, ", ") + ". Defaults to \"" + constants.LiveGrubEntryInstall + "\"",
		},
		&cli.StringSliceFlag{
			Name:  "extension",
			Usage: "System extension to include, repeatable. Either a catalog name, optionally with @version, or file://<path> to bake in a .raw image this host already has",
		},
		&cli.StringSliceFlag{
			Name:  "extensions-catalog",
			Usage: "System extension catalog URL or file, repeatable. Searched in order, the first catalog publishing the name wins. Defaults to the hadron-layers catalog",
		},
		AllowInsecureRegistriesFlag,
	},
	ArgsUsage: "<source>",
	Action: func(ctx *cli.Context) error {
		internal.Log = logger.NewKairosLogger("aurora", ctx.String("loglevel"), false)
		source := ctx.Args().Get(0)
		if source == "" {
			// Hack to prevent ShowAppHelpAndExit from checking only subcommands.
			// (in this case there is only the 'help' subcommand). We are exiting
			// anyway, so no harm from setting it to nil.
			ctx.Command.Subcommands = nil
			cli.ShowCommandHelp(ctx, ctx.Command.Name)
			fmt.Println("")

			return errors.New("no source defined")
		}

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

		cloudConfig := ""
		var err error
		ccPath := ctx.String("cloud-config")
		if ccPath == "" && len(ctx.Lineage()) > 1 {
			// A same-named app-level flag given before the subcommand would
			// otherwise be shadowed by this command's flag.
			ccPath = ctx.Lineage()[1].String("cloud-config")
		}
		if ccPath != "" {
			// we don't allow templating in this command (like we do at the top level one)
			// TODO: Should we allow it?
			cloudConfig, err = config.ReadCloudConfig(ccPath, map[string]interface{}{})
			if err != nil {
				return fmt.Errorf("reading cloud config: %w", err)
			}
		}
		r := schema.ReleaseArtifact{
			ContainerImage: source,
		}
		extensionValues := ctx.StringSlice("extension")
		extensionRequests := make([]extensions.Request, 0, len(extensionValues))
		for _, value := range extensionValues {
			request, err := extensions.ParseRequest(value)
			if err != nil {
				return err
			}
			extensionRequests = append(extensionRequests, request)
		}

		isoOptions := schema.ISO{
			OverrideName:       ctx.String("override-name"),
			IncludeDate:        ctx.Bool("date"),
			OverlayISO:         ctx.String("overlay-iso"),
			OverlayRootfs:      ctx.String("overlay-rootfs"),
			ExtendLiveCmdline:  ctx.String("extend-live-cmdline"),
			LiveConsole:        ctx.String("live-console"),
			DefaultGrubEntry:   ctx.String("default-grub-entry"),
			ExtensionsCatalogs: ctx.StringSlice("extensions-catalog"),
			Extensions:         extensionRequests,
		}

		if err := validateISOOptions(isoOptions); err != nil {
			return err
		}

		allowInsecure := ctx.Bool("allow-insecure-registries")
		c := schema.Config{
			ISO:                     isoOptions,
			State:                   ctx.String("output"),
			CloudConfig:             cloudConfig,
			Arch:                    ctx.String("arch"),
			AllowInsecureRegistries: &allowInsecure,
		}

		if c.State == "" {
			c.State = "."
		}
		if abs, err := filepath.Abs(c.State); err == nil {
			c.State = abs
		}

		// The cloud config copy, the unpacked rootfs and the netboot dir stay in a
		// private directory of this run, so --output only receives the ISO and
		// its checksum. It lives under os.TempDir() and not under --output, see
		// the note on tmpRootFs in the deployer.
		workDir, err := os.MkdirTemp("", "auroraboot-build-iso-")
		if err != nil {
			return fmt.Errorf("creating work directory: %w", err)
		}
		defer os.RemoveAll(workDir)
		defer removeOnSignal(workDir)()

		d := deployer.NewDeployer(c, r, herd.EnableInit)
		d.WorkDir = workDir
		for _, step := range []func() error{
			d.PrepDirs,
			d.StepCopyCloudConfig,
			d.StepDumpSource,
			d.StepGenISO,
		} {
			if err := step(); err != nil {
				return err
			}
		}

		if err := d.Run(ctx.Context); err != nil {
			return err
		}

		err = d.CollectErrors()

		errCleanup := d.CleanTmpDirs()
		if errCleanup != nil {
			// Append the cleanup error to the main errors if any
			err = multierror.Append(err, errCleanup)
		}

		return err
	},
}

func validateISOOptions(i schema.ISO) error {
	for _, path := range []string{i.OverlayISO, i.OverlayRootfs} {
		if path == "" {
			continue
		}

		_, err := os.Stat(path)
		if err == nil {
			continue
		}
		if os.IsNotExist(err) {
			return fmt.Errorf("invalid path '%s'", path)
		}
	}

	return nil
}
