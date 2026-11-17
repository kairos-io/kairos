package cmd

import (
	"errors"
	"fmt"
	"strings"

	"github.com/kairos-io/AuroraBoot/internal/config"
	"github.com/kairos-io/AuroraBoot/pkg/constants"
	"github.com/kairos-io/AuroraBoot/pkg/extensions"
	"github.com/kairos-io/AuroraBoot/pkg/uki"
	"github.com/kairos-io/kairos/v4/sdk/types/logger"
	"github.com/urfave/cli/v2"
)

// Use:   "build-uki SourceImage",
// Short: "Build a UKI artifact from a container image",
var BuildUKICmd = cli.Command{
	Name:      "build-uki",
	Aliases:   []string{"bu"},
	Usage:     "Builds a UKI artifact from a container image",
	ArgsUsage: "<source>",
	Description: "Build a UKI artifact from a container image\n\n" +
		"SourceImage - should be provided as uri in following format <sourceType>:<sourceName>\n" +
		"    * <sourceType> - might be [\"dir\", \"file\", \"oci\", \"docker\"], as default is \"docker\"\n" +
		"    * <sourceName> - is path to file or directory, image name with tag version\n" +
		"The following files are expected inside the public keys directory for SecureBoot auto enroll:\n" +
		"    - db.auth\n" +
		"    - KEK.auth\n" +
		"    - PK.auth\n",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "name",
			Aliases: []string{"n"},
			Value:   "",
			Usage:   "Basename of the generated artifact (ignored for uki output type)",
		},
		&cli.StringFlag{
			Name:    "output-dir",
			Aliases: []string{"d"},
			Value:   ".",
			Usage:   "Output dir for artifact",
		},
		&cli.StringFlag{
			Name:    "output-type",
			Aliases: []string{"t"},
			Value:   string(constants.DefaultOutput),
			Usage:   fmt.Sprintf("Artifact output type [%s]", strings.Join(constants.OutPutTypes(), ", ")),
		},
		&cli.StringFlag{
			Name:    "overlay-rootfs",
			Aliases: []string{"o"},
			Usage:   "Dir with files to be applied to the system rootfs. All the files under this dir will be copied into the rootfs of the uki respecting the directory structure under the dir.",
		},
		&cli.StringFlag{
			Name:    "overlay-iso",
			Aliases: []string{"i"},
			Usage:   "Dir with files to be copied to the ISO rootfs.",
		},
		&cli.StringFlag{
			Name:  "boot-branding",
			Value: "Kairos",
			Usage: "Boot title branding",
		},
		&cli.BoolFlag{
			Name:  "include-version-in-config",
			Value: false,
			Usage: "Include the OS version in the .config file",
		},
		&cli.BoolFlag{
			Name:  "include-cmdline-in-config",
			Value: false,
			Usage: "Include the cmdline in the .config file. Only the extra values are included.",
		},
		&wholeStringSliceFlag{cli.StringSliceFlag{
			Name:    "extra-cmdline",
			Aliases: []string{"c"},
			Usage:   "Add extra efi files with this cmdline for the default 'norole' artifacts. This creates efi files with the default cmdline and extra efi files with the default+provided cmdline.",
		}},
		&cli.StringFlag{
			Name:    "extend-cmdline",
			Aliases: []string{"x"},
			Usage:   "Extend the default cmdline for the default 'norole' artifacts. This creates efi files with the default+provided cmdline.",
		},
		&cli.StringFlag{
			Name:  "cloud-config",
			Usage: "The cloud config to embed in the UKI",
		},
		&wholeStringSliceFlag{cli.StringSliceFlag{
			Name:    "single-efi-cmdline",
			Aliases: []string{"s"},
			Usage:   "Add one extra efi file with the default+provided cmdline. The syntax is '--single-efi-cmdline \"My Entry: cmdline,options,here\"'. The boot entry name is the text under which it appears in systemd-boot menu. Text before the first ':' is only a title if it has no '=' or '/'.",
		}},
		&cli.StringFlag{
			Name:     "public-keys",
			Usage:    "Directory with the public keys for auto enrolling them under Secure Boot",
			Required: false,
		},
		&cli.StringFlag{
			Name:     "tpm-pcr-private-key",
			Usage:    "Private key for signing the EFI PCR policy, Can be a PKCS11 URI or a PEM file",
			Required: true,
		},
		&cli.StringFlag{
			Name:     "sb-key",
			Usage:    "Private key to sign the EFI files for SecureBoot",
			Required: true,
		},
		&cli.StringFlag{
			Name:     "sb-cert",
			Usage:    "Certificate to sign the EFI files for SecureBoot",
			Required: true,
		},
		&cli.Int64Flag{
			Name:  "efi-size-warn",
			Value: 1024,
			Usage: "EFI file size warning threshold in megabytes",
		},
		&cli.StringFlag{
			Name:  "secure-boot-enroll",
			Value: "if-safe",
			Usage: "The value of secure-boot-enroll option of systemd-boot. Possible values: off|manual|if-safe|force. Minimum systemd version: 253. Docs: https://manpages.debian.org/experimental/systemd-boot/loader.conf.5.en.html. !! Danger: this feature might soft-brick your device if used improperly !!",
		},
		&cli.StringFlag{
			Name:  "splash",
			Usage: "Path to the custom logo splash BMP file.",
		},
		&cli.BoolFlag{
			Name:  "cmd-lines-v2",
			Value: false,
			Usage: "Use the new cmdline v2 format to generate a multiprofile efi with all the extra cmdlines. This requires systemd-boot 257 or newer.",
		},
		&cli.BoolFlag{
			Name:  "sdboot-in-source",
			Value: false,
			Usage: "Try to find systemd-boot files in the source rootfs instead of using the bundled ones.",
		},
		&cli.StringSliceFlag{
			Name:  "extension",
			Usage: "Add an extension by name or name@version, or file://<path> to bake in a .raw image this host already has (repeatable)",
		},
		&cli.StringSliceFlag{
			Name:  "extensions-catalog",
			Usage: "Path or URL of an extension catalog, repeatable. Searched in order, the first catalog publishing the name wins. Defaults to the hadron-layers catalog",
		},
		AllowInsecureRegistriesFlag,
	},
	Before: func(ctx *cli.Context) error {
		// TODO: Use MutuallyExclusiveFlags when urfave/cli v3 is stable:
		// https://github.com/urfave/cli/blob/7ec374fe2abd3e9c75369f6bb4191fe7866bd89c/command.go#L128
		if len(wholeStringSliceValues(ctx, "extra-cmdline")) > 0 && ctx.String("extend-cmdline") != "" {
			return errors.New("extra-cmdline and extend-cmdline flags are mutually exclusive")
		}
		for _, value := range ctx.StringSlice("extension") {
			if _, err := extensions.ParseRequest(value); err != nil {
				return err
			}
		}
		if ctx.String("public-keys") == "" {
			fmt.Println("Warning: public-keys directory is not set, Secure Boot auto enroll will not work. You can set it with --public-keys flag.")
		}
		return CheckRoot()
	},
	Action: func(ctx *cli.Context) error {
		args := ctx.Args()
		if args.Len() < 1 {
			return errors.New("no image provided")
		}

		opts, err := buildUKIOptions(ctx)
		if err != nil {
			return err
		}
		return uki.Build(opts)
	},
}

// buildUKIOptions maps the build-uki flags and arguments onto uki.Options.
func buildUKIOptions(ctx *cli.Context) (uki.Options, error) {
	args := ctx.Args()
	logLevel := "info"
	if ctx.Bool("debug") {
		logLevel = "debug"
	}
	log := logger.NewKairosLogger("auroraboot", logLevel, false)
	extensionRequests := make([]extensions.Request, 0, len(ctx.StringSlice("extension")))
	for _, value := range ctx.StringSlice("extension") {
		request, err := extensions.ParseRequest(value)
		if err != nil {
			return uki.Options{}, err
		}
		extensionRequests = append(extensionRequests, request)
	}

	cloudConfig := ""
	ccPath := ctx.String("cloud-config")
	if ccPath == "" && len(ctx.Lineage()) > 1 {
		// A same-named app-level flag given before the subcommand would
		// otherwise be shadowed by this command's flag.
		ccPath = ctx.Lineage()[1].String("cloud-config")
	}
	if ccPath != "" {
		cc, err := config.ReadCloudConfig(ccPath, map[string]interface{}{})
		if err != nil {
			return uki.Options{}, fmt.Errorf("reading cloud config: %w", err)
		}
		cloudConfig = cc
	}

	return uki.Options{
		Source:                  args.Get(0),
		OutputDir:               ctx.String("output-dir"),
		OutputType:              ctx.String("output-type"),
		Name:                    ctx.String("name"),
		OverlayRootfs:           ctx.String("overlay-rootfs"),
		OverlayISO:              ctx.String("overlay-iso"),
		BootBranding:            ctx.String("boot-branding"),
		IncludeVersionInConfig:  ctx.Bool("include-version-in-config"),
		IncludeCmdlineInConfig:  ctx.Bool("include-cmdline-in-config"),
		ExtraCmdlines:           wholeStringSliceValues(ctx, "extra-cmdline"),
		ExtendCmdline:           ctx.String("extend-cmdline"),
		SingleEfiCmdlines:       wholeStringSliceValues(ctx, "single-efi-cmdline"),
		PublicKeysDir:           ctx.String("public-keys"),
		TPMPCRPrivateKey:        ctx.String("tpm-pcr-private-key"),
		SBKey:                   ctx.String("sb-key"),
		SBCert:                  ctx.String("sb-cert"),
		SecureBootEnroll:        ctx.String("secure-boot-enroll"),
		Splash:                  ctx.String("splash"),
		CmdLinesV2:              ctx.Bool("cmd-lines-v2"),
		SdBootInSource:          ctx.Bool("sdboot-in-source"),
		AllowInsecureRegistries: ctx.Bool("allow-insecure-registries"),
		Extensions:              extensionRequests,
		ExtensionsCatalogs:      ctx.StringSlice("extensions-catalog"),
		CloudConfig:             cloudConfig,
		Logger:                  &log,
	}, nil
}
