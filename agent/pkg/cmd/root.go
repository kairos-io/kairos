package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/kairos-io/kairos/v4/agent/internal/agent"
	"github.com/kairos-io/kairos/v4/agent/internal/bus"
	"github.com/kairos-io/kairos/v4/agent/internal/phonehome"
	"github.com/kairos-io/kairos/v4/agent/pkg/action"
	agentConfig "github.com/kairos-io/kairos/v4/agent/pkg/config"
	"github.com/kairos-io/kairos/v4/agent/pkg/constants"
	installer "github.com/kairos-io/kairos/v4/agent/pkg/extensions"
	v1 "github.com/kairos-io/kairos/v4/agent/pkg/implementations/imageextractor"
	"github.com/kairos-io/kairos/v4/agent/pkg/progress"
	"github.com/kairos-io/kairos/v4/agent/pkg/utils"
	"github.com/kairos-io/kairos/v4/internal/version"
	"github.com/kairos-io/kairos/v4/sdk/bundles"
	events "github.com/kairos-io/kairos/v4/sdk/bus"
	"github.com/kairos-io/kairos/v4/sdk/collector"
	"github.com/kairos-io/kairos/v4/sdk/kcrypt"
	"github.com/kairos-io/kairos/v4/sdk/machine"
	"github.com/kairos-io/kairos/v4/sdk/schema"
	"github.com/kairos-io/kairos/v4/sdk/state"
	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
	extensiontypes "github.com/kairos-io/kairos/v4/sdk/types/extensions"
	sdkLogger "github.com/kairos-io/kairos/v4/sdk/types/logger"
	sdkUtils "github.com/kairos-io/kairos/v4/sdk/utils"
	"github.com/kairos-io/kairos/v4/sdk/versioneer"
	"github.com/mudler/go-pluggable"
	"github.com/sanity-io/litter"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

// Output formats accepted by "upgrade list-releases --output".
const (
	outputFormatTerminal = "terminal"
	outputFormatJSON     = "json"
	outputFormatYAML     = "yaml"
)

// ReleasesToOutput renders the releases in the given format. The terminal
// format returns one release per element, so that the caller prints them one
// per line. The machine readable formats return a single element holding the
// whole document.
func ReleasesToOutput(rels []string, output string) []string {
	// Marshal an empty list as "[]" rather than "null", so that a caller
	// parsing the document always gets a list back.
	if rels == nil {
		rels = []string{}
	}

	switch strings.ToLower(output) {
	case outputFormatYAML:
		d, _ := yaml.Marshal(rels)
		return []string{string(d)}
	case outputFormatJSON:
		d, _ := json.Marshal(rels)
		return []string{string(d)}
	default:
		return rels
	}
}

// parseReleasesOutput validates the --output value of list-releases. An empty
// value keeps the terminal format, so that the flag stays optional. An unknown
// value is refused instead of falling back to the terminal format, so that a
// typo in a script is visible rather than quietly producing the wrong format.
func parseReleasesOutput(output string) (string, error) {
	switch strings.ToLower(output) {
	case "":
		return outputFormatTerminal, nil
	case outputFormatTerminal, outputFormatJSON, outputFormatYAML:
		return strings.ToLower(output), nil
	default:
		return "", fmt.Errorf("unknown output format %q, use one of %q, %q or %q",
			output, outputFormatJSON, outputFormatYAML, outputFormatTerminal)
	}
}

// releasesNotes returns the writer the human readable lines of list-releases go
// to. In a machine readable format they go to stderr, so that stdout carries
// the document and nothing else.
func releasesNotes(format string) io.Writer {
	if format == outputFormatTerminal {
		return os.Stdout
	}

	return os.Stderr
}

// printReleases writes the releases in the requested format.
func printReleases(w io.Writer, rels []string, format string) {
	for _, r := range ReleasesToOutput(rels, format) {
		fmt.Fprintln(w, r)
	}
}

// printProviderReleases writes the releases the providers answered with and
// reports whether there were any. A true result means the providers have
// answered the question and the registry does not need to be consulted. The
// header goes to notes rather than to w, so that it does not land in the middle
// of a machine readable document.
func printProviderReleases(w, notes io.Writer, tags []string, format string) bool {
	if len(tags) == 0 {
		return false
	}

	fmt.Fprintln(notes, "Available releases from provider:")
	printReleases(w, tags, format)

	return true
}

// globalFlags are the flags every kairos-agent command inherits from the root.
type globalFlags struct {
	strictValidation bool
	debug            bool
}

// envAnnotation marks a flag that also takes its value from an environment
// variable. applyEnvFlags reads it.
const envAnnotation = "kairos-agent/env"

// bindEnv makes the flag name also settable from the environment variable env.
// The command line wins over the environment.
func bindEnv(flags *pflag.FlagSet, name, env string) {
	f := flags.Lookup(name)
	f.Usage = fmt.Sprintf("%s [$%s]", f.Usage, env)
	_ = flags.SetAnnotation(name, envAnnotation, []string{env})
}

// applyEnvFlags sets every flag that was not given on the command line from
// its environment variable, when that variable is set. A flag set this way
// counts as given, so a check that asks whether the operator passed a flag,
// such as checkInstallBundleFlags, sees the environment too. A variable that
// is set but empty turns a boolean flag off.
func applyEnvFlags(flags *pflag.FlagSet) error {
	var err error
	flags.VisitAll(func(f *pflag.Flag) {
		if err != nil || f.Changed {
			return
		}
		for _, env := range f.Annotations[envAnnotation] {
			v, ok := os.LookupEnv(env)
			if !ok {
				continue
			}
			if v == "" && f.Value.Type() == "bool" {
				v = "false"
			}
			if setErr := flags.Set(f.Name, v); setErr != nil {
				err = fmt.Errorf("could not parse %q as %s value from environment variable %q for flag %s: %w", v, f.Value.Type(), env, f.Name, setErr)
			}
			return
		}
	})
	return err
}

// stopFlagsAtFirstArgument makes every command in the tree read flags only up
// to its first positional argument, and pass anything after it on as an
// argument. That is how the agent has always read its command line, and
// callers depend on it: install-bundle documents it, and the phone-home
// extension handlers build their argv around it.
func stopFlagsAtFirstArgument(cmd *cobra.Command) {
	cmd.Flags().SetInterspersed(false)
	for _, c := range cmd.Commands() {
		stopFlagsAtFirstArgument(c)
	}
}

// groupCommand fills in a command that only groups subcommands. Cobra treats a
// command without a RunE as a request for help and exits 0 whatever it was
// given, so a mistyped subcommand would pass for a successful one. NoArgs
// refuses it instead.
func groupCommand(cmd *cobra.Command) *cobra.Command {
	cmd.Args = cobra.NoArgs
	cmd.RunE = func(c *cobra.Command, _ []string) error {
		return c.Help()
	}
	return cmd
}

// usageError prints the usage of cmd to stderr and returns msg as the error,
// for a command given the wrong number of arguments.
func usageError(cmd *cobra.Command, msg string) error {
	_ = cmd.Usage()
	return errors.New(msg)
}

func addSourceFlag(flags *pflag.FlagSet, source *string) {
	flags.StringVar(source, "source", "", "Source for upgrade. Composed of `type:address`. Accepts `file:`,`dir:` `oci:`,or `ocifile:` for the type of source.\nFor example `ocifile:/var/share/myimage.tar`, `dir:/tmp/extracted` or `oci:repo/image:tag`")
}

func addAllowInsecureRegistriesFlag(flags *pflag.FlagSet, insecure *bool) {
	flags.BoolVar(insecure, "allow-insecure-registries", false, "Pull the image from a registry served over plain HTTP or presenting an untrusted/self-signed TLS certificate. Can also be set in the cloud config via \"install.allow-insecure-registries\" or \"upgrade.allow-insecure-registries\".")
}

func addKcryptNVIndexFlag(flags *pflag.FlagSet, nvIndex *string) {
	flags.StringVar(nvIndex, "nv-index", "", "NV index to operate on (defaults to configured index or 0x1500000)")
}

func addKcryptTPMDeviceFlag(flags *pflag.FlagSet, tpmDevice *string) {
	flags.StringVar(tpmDevice, "tpm-device", "", "TPM device path (defaults to configured device or system default)")
}

func addKcryptCIndexFlag(flags *pflag.FlagSet, cIndex *string) {
	flags.StringVar(cIndex, "c-index", "", "C index (certificate index) for decryption (defaults to configured index)")
}

// checkUpgrade is the check every upgrade command runs before it does
// anything: list-releases runs it too, as a subcommand of upgrade.
func checkUpgrade(source string) error {
	if err := validateSource(source); err != nil {
		return err
	}
	if bootFromLiveMedia() {
		return fmt.Errorf("cannot upgrade from live media/unknown boot state")
	}

	return checkRoot()
}

func newUpgradeCmd(global *globalFlags) *cobra.Command {
	var (
		image, source, bootEntry string
		recovery, insecure       bool
		dryRun                   bool
		excludePaths             []string
	)

	cmd := &cobra.Command{
		// TODO: Fix the implicit upgrade
		Use: "upgrade",
		Long: `
Manually upgrade a kairos node Active image. Does not upgrade the passive image. It upgrades the recovery image when the --recovery flag is passed.

To specify a version, pass it as argument using the --source flag. Passing just the Kairos version as the first argument is no longer supported. If you speficy a positional argument, it will be treated
as a value for the --source flag.

You can also specify the upgrade image by setting "upgrade.system.uri" for the active image or "upgrade.recovery-system.uri" for the recovery image, in the cloud config.

To pull from a registry served over plain HTTP or presenting an untrusted/self-signed TLS certificate, pass the --allow-insecure-registries flag (or set "upgrade.allow-insecure-registries: true" in the cloud config).

To check what an upgrade would do before running it, pass the --dry-run flag. It resolves the source (including the cloud config), the image size and where the transition image would be written, checks that the image manifest can be fetched from the registry, prints a summary and exits without changing the system.

To retrieve all the available versions, use "kairos upgrade list-releases". Use the --registry flag to specify a custom registry to retrieve the versions from, otherwise it will default to quay.io/kairos.

$ kairos upgrade list-releases

See https://kairos.io/docs/upgrade/manual/ for documentation.

`,
		PreRunE: func(*cobra.Command, []string) error {
			return checkUpgrade(source)
		},
		RunE: func(_ *cobra.Command, args []string) error {
			var upgradeSource string
			if len(args) == 1 {
				fmt.Println("Warning: Passing a version as a positional argument is deprecated. Use --source flag instead.")
				fmt.Println("The value will be used as a value for the --source flag")
				upgradeSource = args[0]
			}

			if source != "" {
				upgradeSource = source
			}

			if image != "" {
				fmt.Println("--image flag is deprecated, please use --source")
				// override source with image for now until we drop it
				upgradeSource = fmt.Sprintf("oci:%s", image)
			}

			if recovery && bootEntry != "" {
				return fmt.Errorf("only one of '--recovery' and '--boot-entry' can be set")
			}

			upgradeEntry := ""
			if recovery {
				upgradeEntry = constants.BootEntryRecovery
			} else if bootEntry != "" {
				upgradeEntry = bootEntry
			}

			return agent.Upgrade(upgradeSource, global.strictValidation, constants.GetUserConfigDirs(),
				upgradeEntry, insecure, dryRun, excludePaths...,
			)
		},
	}

	flags := cmd.Flags()
	flags.StringVar(&image, "image", "", "[DEPRECATED] Specify a full image reference, e.g.: quay.io/some/image:tag")
	addSourceFlag(flags, &source)
	flags.StringVar(&bootEntry, "boot-entry", "", "Specify a systemd-boot entry to upgrade (other than active/passive/recovery). The value should match the name of the '.efi' file.")
	flags.BoolVar(&recovery, constants.BootRecovery, false, "Upgrade recovery")
	flags.StringSliceVar(&excludePaths, "exclude-path", nil, "Paths to exclude from the upgrade process. Can be specified multiple times.")
	addAllowInsecureRegistriesFlag(flags, &insecure)
	flags.BoolVar(&dryRun, "dry-run", false, "Resolve the upgrade and print a summary of what would be done, without changing the system")

	cmd.AddCommand(newListReleasesCmd())
	return cmd
}

func newListReleasesCmd() *cobra.Command {
	var (
		output, registry string
		pre, all         bool
	)

	cmd := &cobra.Command{
		Use:   "list-releases",
		Short: "List all available releases versions",
		Long:  `List all available releases versions`,
		PreRunE: func(*cobra.Command, []string) error {
			// Refuse an unusable format before anything is printed.
			format, err := parseReleasesOutput(output)
			if err != nil {
				return err
			}

			if err := checkUpgrade(""); err != nil {
				return err
			}

			notes := releasesNotes(format)

			// Check if the registry is set in the OS release and warn that its deprecated
			registryAndOrg, err := sdkUtils.OSRelease("REGISTRY_AND_ORG")
			if err == nil {
				fmt.Fprintln(notes, "Warning: The 'REGISTRY_AND_ORG' OS release variable is deprecated. Use the '--registry' flag instead.")
				fmt.Fprintln(notes, "Warning: Using the values from 'REGISTRY_AND_ORG' instead of the flag.")
				registry = registryAndOrg
			}
			fmt.Fprintf(notes, "Using registry: %s\n", registry)
			return nil
		},
		RunE: func(*cobra.Command, []string) error {
			format, err := parseReleasesOutput(output)
			if err != nil {
				return err
			}
			notes := releasesNotes(format)

			if utils.IsUki() {
				fmt.Fprintln(notes, "You are running in \"trusted boot\" mode")
				fmt.Fprintln(notes, "Upgrading your OS requires a new image to be built an signed")
				fmt.Fprintln(notes, "Read the docs on how to do so: https://kairos.io/docs/upgrade/trustedboot/")
				// Still emit the document, so that a caller asking for
				// a machine readable format always gets one.
				if format != outputFormatTerminal {
					printReleases(os.Stdout, nil, format)
				}
				return nil
			}

			currentImage, err := agent.CurrentImage(registry)
			if err != nil {
				return err
			}
			fmt.Fprintf(notes, "Current image:\n%s\n\n", currentImage)

			var tags []string
			tags, err = getReleasesFromProvider(pre)
			if err != nil {
				return err
			}

			// Provider returns tags. Print and return.
			if printProviderReleases(os.Stdout, notes, tags, format) {
				return nil
			}

			if all {
				fmt.Fprintln(notes, "Available releases (all):")
				tags, err = agent.ListAllReleases(pre, registry)
				if err != nil {
					return err
				}
			} else {
				fmt.Fprintln(notes, "Available releases with higher version:")
				tags, err = agent.ListNewerReleases(pre, registry)
				if err != nil {
					return err
				}
			}

			if len(tags) == 0 {
				fmt.Fprintln(notes, "No newer releases found")
				// An empty list is still a valid answer, so emit the
				// document rather than leaving stdout empty.
				if format != outputFormatTerminal {
					printReleases(os.Stdout, nil, format)
				}
				return nil
			}

			printReleases(os.Stdout, tags, format)

			return nil
		},
	}

	flags := cmd.Flags()
	flags.StringVar(&output, "output", "", "Output format (json|yaml|terminal)")
	flags.BoolVar(&pre, "pre", false, "Include pre-releases (rc, beta, alpha)")
	flags.BoolVar(&all, "all", false, "Include older releases, and releases that would downgrade Kubernetes")
	flags.StringVar(&registry, "registry", "quay.io/kairos", "Registry to retrieve the releases from. Defaults to quay.io/kairos.")
	return cmd
}

func newUpgradeFinalizeCmd() *cobra.Command {
	var contextFile string

	cmd := &cobra.Command{
		Use:   "upgrade-finalize",
		Short: "internal: run the post-deploy finalize step of an upgrade",
		Long: `
This is a hidden subcommand invoked by the host kairos-agent during a
non-UKI upgrade after DeployImage. The host chroots into the deployed
target rootfs, bind-mounts its own state / recovery / OEM / persistent
/ EFI partitions under /host, and execs this command inside the chroot
so the target image's own kairos-agent runs the format-writing steps of
the upgrade (label state images, extra dirs, SELinux relabel, GRUB
default entry rebrand, ESP refresh, after-upgrade-chroot hook). Doing
so lets a format change (e.g. a new loader/entries key, a new GRUB
menu, a new boot-assessment counter) reach installed nodes without
requiring every previously released host agent to already understand
that format.

Not part of the public CLI; call sites and the wire contract may
change between releases.
`,
		Hidden: true,
		PreRunE: func(*cobra.Command, []string) error {
			return checkRoot()
		},
		RunE: func(*cobra.Command, []string) error {
			return agent.UpgradeFinalize(contextFile)
		},
	}

	cmd.Flags().StringVar(&contextFile, "context-file", "", "Path (inside the target chroot) to the JSON-serialized FinalizeContext the host wrote")
	_ = cmd.MarkFlagRequired("context-file")
	return cmd
}

func newNotifyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "notify <event> <config dir>...",
		Short: "emits the given event with a generic event payload",
		Long: `
Sends a generic event payload with the configuration found in the scanned directories.
`,
		RunE: func(_ *cobra.Command, args []string) error {
			dirs := []string{"/oem", "/usr/local/cloud-config"}
			if len(args) > 1 {
				dirs = args[1:]
			}

			var event string
			if len(args) > 0 {
				event = args[0]
			}
			return agent.Notify(event, dirs)
		},
	}
}

func newStartCmd() *cobra.Command {
	var (
		restart, force bool
		api            string
	)

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Starts the kairos agent",
		Long: `
Starts the kairos agent which automatically bootstrap and advertize to the kairos network.
`,
		Aliases: []string{"s"},
		RunE: func(_ *cobra.Command, args []string) error {
			dirs := []string{"/oem", "/usr/local/cloud-config"}
			if len(args) > 0 {
				dirs = args
			}

			opts := []agent.Option{
				agent.WithAPI(api),
				agent.WithDirectory(dirs...),
			}

			if force {
				fmt.Fprintln(os.Stderr, "Warning: --force has no effect on 'start' and will be removed in a future release")
			}

			if restart {
				opts = append(opts, agent.RestartAgent)
			}

			return agent.Run(opts...)
		},
	}

	flags := cmd.Flags()
	flags.BoolVar(&restart, "restart", false, "")
	flags.BoolVar(&force, "force", false, "Deprecated: has no effect. Kept so existing invocations keep working.")
	// Deliberately no default. The address is the p2p provider's to
	// decide: it both writes APILISTEN for the edgevpn daemon and
	// defaults its own CLI client to the same place. A default here
	// would win over the provider's and desynchronise the two ends.
	//
	// Note that an explicitly set value desynchronises them just the
	// same. Whatever arrives here reaches the daemon's APILISTEN and
	// the provider's in-process coordination client, but not the
	// provider's command line client, which has no way to learn it
	// and keeps its own default. Operators who set this must also
	// export EDGEVPN_API for `get-kubeconfig` and `role list`. The
	// real fix is for the provider CLI to derive its default from
	// the APILISTEN the provider already writes to
	// /etc/systemd/system.conf.d/edgevpn-kairos.env, which belongs
	// in provider/ (formerly kairos-io/provider-kairos).
	flags.StringVar(&api, "api", "", "Address of the edgevpn API the p2p provider co-ordinates over. "+
		"Leave unset to let the provider choose, which is what keeps the edgevpn "+
		"daemon's listen address and the provider CLI's client address in agreement. "+
		"Setting it moves the daemon only, so the provider CLI needs the same "+
		"address in EDGEVPN_API to keep working.")
	return cmd
}

func newInstallBundleCmd() *cobra.Command {
	var (
		repository, rootPath string
		localFile            bool
	)

	cmd := &cobra.Command{
		Use:                   "install-bundle [--local-file] [--repository TYPE://URL] [--root-path PATH] SCHEME://REFERENCE",
		DisableFlagsInUseLine: true,
		Short:                 "Installs a kairos bundle",
		Long: `Manually installs a kairos bundle.

The target has the form SCHEME://REFERENCE. Supported schemes:

  container:// or docker://  extract the image into --root-path
  run://                     extract the image to a temporary directory and run its run.sh
  package://                 install a luet package from --repository into --root-path

Flags must come before the target, anything after the target is not read as a flag.
A flag the target's scheme does not read is refused rather than ignored.

Examples:

  kairos-agent install-bundle container://quay.io/myorg/mybundle:latest
  kairos-agent install-bundle --local-file container:///absolute/path/mybundle.tar
`,
		PreRunE: func(*cobra.Command, []string) error {
			return checkRoot()
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return fmt.Errorf("bundle name required")
			}

			if err := checkInstallBundleFlags(cmd.Flags(), args[0]); err != nil {
				return err
			}

			return bundles.RunBundles([]bundles.BundleOption{bundles.WithRootFS(rootPath), bundles.WithRepository(repository), bundles.WithTarget(args[0]), bundles.WithLocalFile(localFile)})
		},
	}

	flags := cmd.Flags()
	flags.StringVar(&repository, "repository", "docker://quay.io/kairos/packages", "Luet repository as TYPE://URL, accepted only by package:// targets")
	bindEnv(flags, "repository", "REPOSITORY")
	flags.StringVar(&rootPath, "root-path", "/", "Directory the bundle is installed into, not used by run:// targets")
	flags.BoolVar(&localFile, "local-file", false, "Read the container, docker or run target from an image tarball on disk instead of pulling it from a registry, not accepted by package:// targets")
	bindEnv(flags, "local-file", "LOCAL_FILE")
	return cmd
}

func newUUIDCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "uuid",
		Short:   "Prints the local UUID",
		Long:    "Print node uuid",
		Aliases: []string{"u"},
		RunE: func(*cobra.Command, []string) error {
			fmt.Print(machine.UUID())
			return nil
		},
	}
}

func newConfigCmd(global *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "config",
		Short:   "Shows the machine configuration",
		Long:    "Show the runtime configuration of the machine. It will scan the machine for all the configuration and will return the config file processed and found.",
		Aliases: []string{"c"},
		RunE: func(*cobra.Command, []string) error {
			config, err := agentConfig.Scan(collector.Directories(constants.GetUserConfigDirs()...), collector.NoLogs)
			if err != nil {
				return err
			}

			configStr, err := config.Collector.String()
			if err != nil {
				return fmt.Errorf("getting config string: %w", err)
			}
			fmt.Printf("%s", configStr)
			return nil
		},
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "get",
		Short: "Get specific data from the configuration",
		Long:  "It allows to navigate the YAML config file by searching with 'yq' style keywords as `config get k3s` to retrieve the k3s config block",
		Example: `
Use it to retrieve configuration programmatically from the CLI:

$ kairos-agent config get k3s.enabled
true

or

$ kairos-agent config get k3s
enabled: true`,
		Aliases: []string{"g"},
		RunE: func(_ *cobra.Command, args []string) error {
			config, err := agentConfig.ScanNoLogs(collector.Directories(constants.GetUserConfigDirs()...), collector.NoLogs, collector.StrictValidation(global.strictValidation))
			if err != nil {
				return err
			}

			res, err := config.Collector.Query(firstArg(args))
			if err != nil {
				return err
			}
			fmt.Printf("%s", res)
			return nil
		},
	})
	return cmd
}

func newStateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "state",
		Short: "get machine state",
		Long:  "Print machine state information, e.g. `state get uuid` returns the machine uuid",
		RunE: func(_ *cobra.Command, args []string) error {
			// `state` takes no arguments of its own. Without this an unknown
			// subcommand falls through to here, prints the state and exits 0,
			// so a caller cannot tell a typo from the command it meant to run.
			if len(args) > 0 {
				return fmt.Errorf("unknown subcommand %q for \"state\"", args[0])
			}

			runtime, err := state.NewRuntime()
			if err != nil {
				return err
			}

			fmt.Print(runtime)
			return err
		},
	}

	cmd.AddCommand(&cobra.Command{
		Use:     "get",
		Short:   "get specific ",
		Long:    "query state data",
		Aliases: []string{"g"},
		RunE: func(_ *cobra.Command, args []string) error {
			runtime, err := state.NewRuntime()
			if err != nil {
				return err
			}

			res, err := runtime.Query(firstArg(args))
			fmt.Print(res)
			return err
		},
	})
	return cmd
}

func newRenderTemplateCmd(global *globalFlags) *cobra.Command {
	var file string

	cmd := &cobra.Command{
		Use:   "render-template",
		Short: "Render a Go template",
		Long:  "Render a Go template with machine state and config as data context",
		RunE: func(*cobra.Command, []string) error {
			config, err := agentConfig.ScanNoLogs(collector.Directories(constants.GetUserConfigDirs()...), collector.NoLogs, collector.StrictValidation(global.strictValidation))
			if err != nil {
				return err
			}

			runtime, err := state.NewRuntime()
			if err != nil {
				return err
			}

			result, err := action.RenderTemplate(file, config, runtime)
			if err != nil {
				return err
			}

			_, err = os.Stdout.Write(result)
			return err
		},
	}

	cmd.Flags().StringVarP(&file, "file", "f", "", "")
	_ = cmd.MarkFlagRequired("file")
	return cmd
}

func newInteractiveInstallCmd() *cobra.Command {
	var (
		shell, skipAuto bool
		source          string
	)

	cmd := &cobra.Command{
		Use:   "interactive-install",
		Short: "Starts interactive installation",
		Long: `
Starts kairos in interactive mode install.

It will ask prompt for several questions and perform an install depending on the providers available in the system.

See also https://kairos.io/installation/interactive_install/ for documentation.

This command is meant to be used from the boot GRUB menu, but can be also started manually`,
		PreRunE: func(*cobra.Command, []string) error {
			if err := validateSource(source); err != nil {
				return err
			}

			return checkRoot()
		},
		RunE: func(*cobra.Command, []string) error {
			log := sdkLogger.NewKairosLogger("agent", "info", true)
			// Get the viper config in case something in command line or env var has set it and set the level asap
			if viper.GetBool("debug") {
				log.SetLevel("debug")
			}

			// install.auto wins over the UX: an unattended config leaves
			// nothing to ask, so no installer is launched. Same call, same
			// answer, as the install-mode entry. Unless the operator asked to
			// skip it, which is the one way to get the installer on a node
			// whose datasource says "install me".
			if skipAutoInstall(skipAuto) {
				log.Infof("--%s was given, so install.auto is ignored and the installer runs", skipAutoInstallFlag)
			} else if installed, _, _, err := autoInstallFn(source, false, constants.GetUserConfigDirs()...); installed || err != nil {
				// --shell asks for a shell instead of the installer TUI. No
				// installer was launched here, so say so rather than dropping
				// the flag without a word.
				if err == nil && shell {
					log.Warnf("--shell was ignored: install.auto installed this node unattended, so no installer was launched")
				}

				return err
			}

			return agent.InteractiveInstall(shell, source, log)
		},
	}

	flags := cmd.Flags()
	flags.BoolVar(&shell, "shell", false, "")
	flags.BoolVar(&skipAuto, skipAutoInstallFlag, false, "Open the installer even when the config sets install.auto, instead of installing unattended. Also settable on the boot cmdline as "+skipAutoInstallCmdline+", which is how to reach it from a booted ISO.")
	bindEnv(flags, skipAutoInstallFlag, "KAIROS_SKIP_AUTO_INSTALL")
	addSourceFlag(flags, &source)
	return cmd
}

func newManualInstallCmd(global *globalFlags) *cobra.Command {
	var (
		device, source                  string
		poweroff, reboot, useDefaultDir bool
		insecure                        bool
	)

	cmd := &cobra.Command{
		Use:   "manual-install",
		Short: "Starts the manual installation",
		Long: `
`,
		Aliases: []string{"m"},
		PreRunE: func(*cobra.Command, []string) error {
			if err := validateSource(source); err != nil {
				return err
			}

			return checkRoot()
		},
		RunE: func(_ *cobra.Command, args []string) error {
			if len(args) == 0 {
				return fmt.Errorf("expect one argument. the config file - if you don't have it, use the interactive-install")
			}
			config := args[0]

			return agent.ManualInstall(config, source, device, reboot, poweroff, global.strictValidation, useDefaultDir, insecure)
		},
	}

	flags := cmd.Flags()
	flags.StringVar(&device, "device", "", "")
	flags.BoolVar(&poweroff, "poweroff", false, "")
	flags.BoolVar(&reboot, "reboot", false, "")
	flags.BoolVar(&useDefaultDir, "use-default-dirs", false, fmt.Sprintf("Force the config collector to use the default locations (%s) in addition to the provided config file", strings.Join(constants.GetUserConfigDirs(), ", ")))
	addSourceFlag(flags, &source)
	addAllowInsecureRegistriesFlag(flags, &insecure)
	return cmd
}

func newInstallCmd() *cobra.Command {
	var (
		source   string
		insecure bool
	)

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Starts the kairos pairing installation",
		Long: `
Starts kairos in pairing mode.

It will print out a QR code which can be used with "kairos register" to send over a configuration and bootstraping a kairos node.

See also https://kairos.io/docs/installation/qrcode/ for documentation.

To pull the install image from a registry served over plain HTTP or presenting an untrusted/self-signed TLS certificate, pass the --allow-insecure-registries flag (or set "install.allow-insecure-registries: true" in the cloud config).

This command is meant to be used from the boot GRUB menu, but can be started manually`,
		Aliases: []string{"i"},
		PreRunE: func(*cobra.Command, []string) error {
			if err := validateSource(source); err != nil {
				return err
			}

			return checkRoot()
		},
		RunE: func(*cobra.Command, []string) error {
			// An unattended config installs and returns; only when there is a
			// decision left for a human does the provider flow run.
			installed, cc, scanErr, err := agent.AutoInstall(source, insecure, constants.GetUserConfigDirs()...)
			if installed || err != nil {
				return err
			}

			// cc is the config AutoInstall already scanned; handing it over
			// keeps a remote config_url from being fetched twice per boot.
			// scanErr goes along so Install can show it on the screen it draws.
			return agent.Install(cc, scanErr, source, insecure, constants.GetUserConfigDirs()...)
		},
	}

	addSourceFlag(cmd.Flags(), &source)
	addAllowInsecureRegistriesFlag(cmd.Flags(), &insecure)
	return cmd
}

func newRecoveryCmd() *cobra.Command {
	return &cobra.Command{
		Use:     constants.BootRecovery,
		Aliases: []string{"r"},
		Short:   "Starts kairos recovery mode",
		Long: `
Starts kairos recovery mode.

In recovery mode a QR code will be printed out on the screen which should be used in conjunction with "kairos bridge". Pass by the QR code as snapshot
to the bridge to connect over the machine which runs the "kairos recovery" command.

See also https://kairos.io/after_install/recovery_mode/ for documentation.

This command is meant to be used from the boot GRUB menu, but can likely be used standalone`,
		RunE: func(*cobra.Command, []string) error {
			return agent.Recovery()
		},
	}
}

func newResetCmd() *cobra.Command {
	var reboot, unattended, resetOem bool

	cmd := &cobra.Command{
		Use:   "reset",
		Short: "Starts kairos reset mode",
		Long: `
Starts kairos reset mode, it will nuke completely the node data and restart fresh.
Attention ! this will delete any persistent data on the node. It is equivalent to re-init the node right after the installation.

Note: the audit trail in /var/log/audit is copied off the persistent partition into a staging directory under /run and put back after the format. It is skipped, with a warning, unless the trail fits in half the free space of /run.

In reset mode a the node will automatically reset

See also https://kairos.io/after_install/reset_mode/ for documentation.

This command is meant to be used from the boot GRUB menu, but can likely be used standalone`,
		PreRunE: func(*cobra.Command, []string) error {
			return checkRoot()
		},
		RunE: func(*cobra.Command, []string) error {
			// The persistent partition is formatted by default during reset
			// (pkg/config/spec.go NewResetSpec sets FormatPersistent: true),
			// so there's no useful CLI knob to surface here.
			return agent.Reset(reboot, unattended, resetOem, constants.GetUserConfigDirs()...)
		},
	}

	flags := cmd.Flags()
	flags.BoolVar(&reboot, "reboot", false, "Enable automated reboot after reset. Has precedence over any config in the system.")
	flags.BoolVar(&unattended, "unattended", false, "Do not wait for user input and provide ttys after reset. Also sets the fast mode (do not wait 60 seconds before reset)")
	flags.BoolVar(&resetOem, "reset-oem", false, "Reset the OEM partition. Warning: this will delete any persistent data on the OEM partition.")
	return cmd
}

func newValidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate",
		Short: "Validates a cloud config file",
		Long: `
The validate command expects a configuration file as its only argument. Local files and URLs are accepted.
		`,
		RunE: func(_ *cobra.Command, args []string) error {
			return schema.Validate(firstArg(args))
		},
	}
}

func newPrintSchemaCmd() *cobra.Command {
	var ver string

	cmd := &cobra.Command{
		Use:   "print-schema",
		Short: "Print out Kairos' Cloud Configuration JSON Schema",
		Long:  `Prints out Kairos' Cloud Configuration JSON Schema`,
		RunE: func(*cobra.Command, []string) error {
			if ver == "" {
				ver = version.Version
			}

			json, err := schema.JSONSchema(ver)

			if err != nil {
				return err
			}

			fmt.Println(json)

			return nil
		},
	}

	cmd.Flags().StringVar(&ver, "version", "", "Print the current schema but with another version number, instead of using the agent version number.")
	return cmd
}

func newRunStageCmd(global *globalFlags) *cobra.Command {
	var (
		strict, analyze            bool
		cloudInitPaths, overridden []string
	)

	cmd := &cobra.Command{
		Use:   "run-stage STAGE",
		Short: "Run stage from cloud-init",
		Long:  "Run stage from cloud-init",
		PreRunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return usageError(cmd, "Stage to run missing")
			}

			return checkRoot()
		},
		RunE: func(_ *cobra.Command, args []string) error {
			stage := args[0]
			config, err := agentConfig.Scan(collector.Directories(constants.GetYipConfigDirs()...), collector.NoLogs)
			config.Strict = strict

			paths := runStageCloudInitPaths(cloudInitPaths, overridden, config)

			if global.debug {
				config.Logger.SetLevel("debug")
			}

			if err != nil {
				config.Logger.Errorf("Error reading config: %s\n", err)
			}
			return utils.RunStageWithPaths(config, stage, analyze, paths)
		},
	}

	flags := cmd.Flags()
	flags.BoolVar(&strict, "strict", false, "Enable strict mode. Fails and exits on stage errors")
	flags.StringSliceVar(&cloudInitPaths, "cloud-init-paths", nil, "Extra paths to add to the run stage")
	flags.StringSliceVar(&overridden, "override-cloud-init-paths", nil, "Override paths to use when running the stage, removing defaults. Supercedes --cloud-init-paths")
	flags.BoolVarP(&analyze, "analyze", "a", false, "Only print the modules that would run in the order they would run")
	return cmd
}

func newPullImageCmd() *cobra.Command {
	var platform string

	cmd := &cobra.Command{
		Use:   "pull-image [-l] IMAGE TARGET",
		Short: "Pull remote image to local file",
		Long:  "Pull remote image to local file",
		PreRunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 2 {
				return usageError(cmd, "Either Image or target argument missing")
			}

			return checkRoot()
		},
		RunE: func(_ *cobra.Command, args []string) error {
			image := args[0]
			destination, err := filepath.Abs(args[1])
			if err != nil {
				return fmt.Errorf("invalid path %s", destination)
			}
			config, err := agentConfig.Scan(collector.Directories(constants.GetUserConfigDirs()...), collector.NoLogs)
			if err != nil {
				return err
			}
			config.Logger.Infof("Starting download and extraction for image %s to %s\n", image, destination)
			e := v1.OCIImageExtractor{}
			if err = e.ExtractImage(image, destination, platform); err != nil {
				return err
			}
			config.Logger.Infof("Image %s downloaded and extracted to %s correctly\n", image, destination)
			return nil
		},
	}

	cmd.Flags().StringVar(&platform, "platform", fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH), "Platform/arch to pull image from")
	return cmd
}

func newVersionCmd() *cobra.Command {
	var long bool

	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print kairos-agent version",
		Long:  "Print kairos-agent version",
		RunE: func(*cobra.Command, []string) error {
			if long {
				fmt.Printf("kairos %s\ngo     %s\n", version.Version, runtime.Version())
			} else {
				fmt.Println(version.Version)
			}
			return nil
		},
	}

	cmd.Flags().BoolVarP(&long, "long", "l", false, "Print long version info")
	return cmd
}

func newVersioneerCmd() *cobra.Command {
	cmd := groupCommand(&cobra.Command{
		Use:   "versioneer",
		Short: "versioneer subcommands",
		Long:  "versioneer subcommands",
	})
	cmd.AddCommand(versioneer.CliCommands()...)
	return cmd
}

func newBootentryCmd() *cobra.Command {
	var selected string

	cmd := &cobra.Command{
		Use:   "bootentry [--select]",
		Short: "bootentry subcommands",
		Long:  "bootentry subcommands",
		PreRunE: func(*cobra.Command, []string) error {
			return checkRoot()
		},
		RunE: func(*cobra.Command, []string) error {
			cfg, err := agentConfig.Scan(collector.Directories(constants.GetUserConfigDirs()...), collector.NoLogs)
			if err != nil {
				return err
			}
			// If we got a selection just go for it, otherwise enter an interactive mode to show entries and let user choose one
			if selected != "" {
				return action.SelectBootEntry(cfg, selected)
			}
			return action.ListBootEntries(cfg)
		},
	}

	cmd.Flags().StringVarP(&selected, "select", "s", "", "Select the boot entry")
	return cmd
}

func newGrubeditCmd(global *globalFlags) *cobra.Command {
	var grubEnv string

	cmd := &cobra.Command{
		Use:   "grubedit [set] KEY=VALUE",
		Short: "Show or edit grub variables.",
		Long: "Show or edit grub variables.\n\n" +
			"Use `grubedit set KEY=VALUE` to set a grub variable. If the value is empty, it will remove the variable.\n\n" +
			"Use `grubedit` to list all the grub variables found in the grub environment file.",
		RunE: func(*cobra.Command, []string) error {
			config, err := agentConfig.Scan(collector.Directories(constants.GetUserConfigDirs()...), collector.NoLogs, collector.StrictValidation(global.strictValidation))
			if err != nil {
				return err
			}
			if _, err := config.Fs.Stat(grubEnv); err != nil && os.IsNotExist(err) {
				// If file doesn't exist, we just log it and return
				// Is not really an error, its just that there is no file so no vars are set
				config.Logger.Logger.Info().Msgf("No file %s found", grubEnv)
				return nil
			}
			vars, err := utils.ReadPersistentVariables(grubEnv, config)
			if err != nil {
				config.Logger.Logger.Error().Err(err).Msgf("Error reading grub variables from %s", grubEnv)
				return err
			}
			if len(vars) == 0 {
				config.Logger.Logger.Info().Msgf("No grub variables found in %s", grubEnv)
			} else {
				// Format the variables into a nice aligned list
				maxKeyLen := 0
				for k := range vars {
					if len(k) > maxKeyLen {
						maxKeyLen = len(k)
					}
				}
				var b strings.Builder
				for k, v := range vars {
					fmt.Fprintf(&b, "%-*s = %s\n", maxKeyLen, k, v)
				}
				config.Logger.Logger.Info().Msgf("Grub variables found in %s:\n%s", grubEnv, b.String())
			}
			return nil
		},
	}

	// Persistent, so that `grubedit set` reads the same file.
	cmd.PersistentFlags().StringVar(&grubEnv, "grub-file", filepath.Join(constants.OEMPath, constants.GrubEnv), "Path to the grub environment file to list or set variables to/from.")

	cmd.AddCommand(&cobra.Command{
		Use:   "set KEY=VALUE",
		Short: "Add or Edit a grub variable. Setting an existing variable to an empty value will remove it",
		PreRunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return usageError(cmd, "File and key=value arguments missing")
			}
			return nil
		},
		RunE: func(_ *cobra.Command, args []string) error {
			config, err := agentConfig.Scan(collector.Directories(constants.GetUserConfigDirs()...), collector.NoLogs, collector.StrictValidation(global.strictValidation))
			if err != nil {
				return err
			}

			// Transform the key=value argument into a map
			parts := strings.SplitN(args[0], "=", 2)
			vars := make(map[string]string)
			if len(parts) != 2 {
				return fmt.Errorf("key=value argument must be in the format KEY=VALUE")
			}
			if parts[1] == "" {
				// If the value is empty, we remove the variable
				vars[parts[0]] = ""
			} else {
				// Otherwise we set the variable to the value
				vars[parts[0]] = parts[1]
			}

			err = utils.SetPersistentVariables(grubEnv, vars, config)
			if err != nil {
				config.Logger.Logger.Error().Err(err).Msgf("Error setting grub variables in %s", args[0])
				return err
			}
			if parts[1] == "" {
				config.Logger.Logger.Info().Msgf("Removed grub variable %s from %s", parts[0], grubEnv)
			} else {
				config.Logger.Logger.Info().Msgf("Set grub variable %s=%s in %s", parts[0], parts[1], grubEnv)
			}

			return nil
		},
	})
	return cmd
}

// newExtensionCmd builds the sysext or the confext command. extType is both
// the command's name and the kind of extension its subcommands act on.
func newExtensionCmd(extType string) *cobra.Command {
	cmd := groupCommand(&cobra.Command{
		Use:   extType,
		Short: extType + " subcommands",
		Long:  extType + " subcommands",
	})
	cmd.AddCommand(sysextConfextCommands(extType)...)
	return cmd
}

func newLogsCmd() *cobra.Command {
	var outputPath string

	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Collect logs from the system",
		Long: `Collect logs from various sources on the Kairos system and create a compressed tarball.

The command will collect logs from:
- Journal logs from specified services (default: kairos-agent, systemd, k3s)
- Log files from specified paths with globbing support

Configuration can be provided in the Kairos config file under the 'logs' section:

logs:
  journal:
  - myservice
  - myotherservice
  files:
  - /var/log/mybinary/*
  - /var/log/something.log

The output will be a tarball with logs organized by type (journal/, files/).`,
		RunE: func(*cobra.Command, []string) error {
			// Get the filesystem and runner from the config
			cfg, err := agentConfig.Scan(collector.Directories(constants.GetUserConfigDirs()...), collector.NoLogs)
			if err != nil {
				return fmt.Errorf("failed to scan config: %w", err)
			}

			return agent.ExecuteLogsCommand(cfg.Fs, cfg.Logger, cfg.Runner, outputPath)
		},
	}

	cmd.Flags().StringVarP(&outputPath, "output", "o", "./kairos-logs.tar.gz", "Output path for the logs tarball")
	return cmd
}

func newKcryptCmd() *cobra.Command {
	cmd := groupCommand(&cobra.Command{
		Use:   "kcrypt",
		Short: "kcrypt subcommands",
		Long:  "kcrypt subcommands for managing encryption-related operations",
	})

	preRunCheckRoot := func(*cobra.Command, []string) error {
		return checkRoot()
	}

	var checkNVIndex, checkTPMDevice string
	checknv := &cobra.Command{
		Use:   "checknv",
		Short: "Check if data exists in TPM NV memory",
		Long: `Check if data exists in a TPM NV index.

This command checks whether the specified NV index contains data.
It will return an error if the index doesn't exist or is empty.

Default behavior:
- Uses NV index from config or defaults to 0x1500000 (legacy local passphrase index)
- Uses the same TPM device as configured (or system default if none specified)`,
		PreRunE: preRunCheckRoot,
		RunE: func(*cobra.Command, []string) error {
			cfg, err := agentConfig.Scan(collector.Directories(constants.GetUserConfigDirs()...), collector.NoLogs)
			if err != nil {
				return fmt.Errorf("failed to scan config: %w", err)
			}
			return action.KcryptCheckNV(cfg, checkNVIndex, checkTPMDevice)
		},
	}
	addKcryptNVIndexFlag(checknv.Flags(), &checkNVIndex)
	addKcryptTPMDeviceFlag(checknv.Flags(), &checkTPMDevice)

	var readNVIndex, readTPMDevice, readCIndex string
	readnv := &cobra.Command{
		Use:   "readnv",
		Short: "Read and decrypt value from TPM NV memory",
		Long: `Read and decrypt the value stored in a TPM NV index.

This command reads the value from the specified TPM NV index. If the value
is encrypted (as is the case for local passphrases), it will be automatically
decrypted using the C index from flags or config before outputting.

For encrypted passphrases stored by kairos-agent, this will output the
decrypted passphrase. For other NV indices, it will output the raw value.

Default behavior:
- Uses NV index from config or defaults to 0x1500000 (legacy local passphrase index)
- Uses the same TPM device as configured (or system default if none specified)
- Uses C index from config if available (or can be specified via --c-index flag)
- Automatically decrypts if C index is available`,
		PreRunE: preRunCheckRoot,
		RunE: func(*cobra.Command, []string) error {
			cfg, err := agentConfig.Scan(collector.Directories(constants.GetUserConfigDirs()...), collector.NoLogs)
			if err != nil {
				return fmt.Errorf("failed to scan config: %w", err)
			}
			return action.KcryptReadNV(cfg, readNVIndex, readTPMDevice, readCIndex)
		},
	}
	addKcryptNVIndexFlag(readnv.Flags(), &readNVIndex)
	addKcryptTPMDeviceFlag(readnv.Flags(), &readTPMDevice)
	addKcryptCIndexFlag(readnv.Flags(), &readCIndex)

	var cleanupNVIndex, cleanupTPMDevice string
	var cleanupConfirmed bool
	cleanupnv := &cobra.Command{
		Use:   "cleanupnv",
		Short: "Clean up TPM NV memory",
		Long: `Clean up TPM NV memory by undefining specific NV indices.

⚠️  DANGER: This command removes data from TPM memory!
⚠️  If you delete the wrong index, your encrypted disk may become UNBOOTABLE!

This command helps clean up TPM NV memory. It can be used to clean up
legacy local passphrase storage (now handled by kairos-sdk) or any other
TPM NV indices that need to be removed.

The command will prompt for confirmation before deletion unless you use
the --i-know-what-i-am-doing flag to skip the safety prompt.

Default behavior:
- Uses NV index from config or defaults to 0x1500000 (legacy local passphrase index)
- Uses the same TPM device as configured (or system default if none specified)
- Prompts for confirmation with safety warnings`,
		PreRunE: preRunCheckRoot,
		RunE: func(*cobra.Command, []string) error {
			cfg, err := agentConfig.Scan(collector.Directories(constants.GetUserConfigDirs()...), collector.NoLogs)
			if err != nil {
				return fmt.Errorf("failed to scan config: %w", err)
			}
			return action.KcryptCleanup(cfg, cleanupNVIndex, cleanupTPMDevice, cleanupConfirmed)
		},
	}
	addKcryptNVIndexFlag(cleanupnv.Flags(), &cleanupNVIndex)
	addKcryptTPMDeviceFlag(cleanupnv.Flags(), &cleanupTPMDevice)
	cleanupnv.Flags().BoolVar(&cleanupConfirmed, "i-know-what-i-am-doing", false, "Skip confirmation prompt (DANGEROUS: may make encrypted disks unbootable)")

	unlockAll := &cobra.Command{
		Use:   "unlock-all",
		Short: "Unlocks all encrypted partitions",
		Long: `Unlocks all encrypted partitions found on the system.

This command scans the system for LUKS encrypted partitions and unlocks them
using the appropriate encryption method (TPM+PCR, Remote KMS, or Local TPM)
based on the system configuration.

The command automatically:
- Detects UKI mode vs non-UKI mode
- Scans for kcrypt configuration (challenger server, mdns, etc.)
- Finds all encrypted partitions that are not already unlocked
- Unlocks them using the appropriate method`,
		PreRunE: preRunCheckRoot,
		RunE: func(*cobra.Command, []string) error {
			cfg, err := agentConfig.Scan(collector.Directories(constants.GetUserConfigDirs()...), collector.NoLogs)
			if err != nil {
				return fmt.Errorf("failed to scan config: %w", err)
			}
			return kcrypt.UnlockAllEncryptedPartitions(cfg.Logger)
		},
	}

	var encryptConfirmed bool
	encrypt := &cobra.Command{
		Use:   "encrypt LABEL [LABEL...]",
		Short: "Encrypt plaintext partitions in place, by filesystem label",
		Long: `Encrypt the given partitions in place, using the configured method
(local TPM, or the kcrypt challenger server when one is configured).

WARNING: Encrypting a partition DESTROYS ALL DATA on it!

This is the manual counterpart of boot time encryption
(kcrypt.encrypt_on_boot): the same operation, run from the command line,
typically from recovery. It is defensive by default:

- A partition that is already a LUKS container is skipped, so the command
  is safe to re-run.
- Partitions the running system depends on (OEM, state, recovery, EFI)
  are refused. Encrypt those at install time instead.
- A mounted partition is refused; unmount it first.
- A label that cannot be found, or whose filesystem cannot be determined,
  is an error rather than a guess.
- The result is verified before success is reported.

The command prompts for confirmation unless --i-know-what-i-am-doing is
given. The partitions are left locked; they unlock on the next boot, or
with 'kairos-agent kcrypt unlock-all'.`,
		PreRunE: preRunCheckRoot,
		RunE: func(_ *cobra.Command, args []string) error {
			if len(args) == 0 {
				return fmt.Errorf("no partition labels given; usage: kairos-agent kcrypt encrypt LABEL [LABEL...]")
			}
			cfg, err := agentConfig.Scan(collector.Directories(constants.GetUserConfigDirs()...), collector.NoLogs)
			if err != nil {
				return fmt.Errorf("failed to scan config: %w", err)
			}
			return action.KcryptEncrypt(cfg, args, encryptConfirmed)
		},
	}
	encrypt.Flags().BoolVar(&encryptConfirmed, "i-know-what-i-am-doing", false, "Skip confirmation prompt (DANGEROUS: encrypting destroys all data on the partitions)")

	cmd.AddCommand(checknv, readnv, cleanupnv, unlockAll, encrypt)
	return cmd
}

func newPhoneHomeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "phone-home",
		Short: "Start phone-home connection to a management server",
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Pull phonehome defaults from cloud-config via the shared Collector.
			// CLI flags still win: they override anything from the merged config.
			var cfg phonehome.Config
			scanned, err := agentConfig.Scan(
				collector.Directories(constants.GetUserConfigDirs()...),
				collector.NoLogs,
			)
			if err == nil {
				// Route the package-level Logger (used by the command
				// handlers and Uninstall helpers) through the agent's own
				// KairosLogger so every phone-home line lands in the same
				// journal/file stream.
				phonehome.SetLogger(scanned.Logger)

				if cfg, err = LoadPhoneHomeConfig(scanned); err != nil {
					return err
				}
			}

			MergePhoneHomeFlags(cmd.Flags(), &cfg)

			if cfg.URL == "" {
				return fmt.Errorf("phonehome URL required (use --url, PHONEHOME_URL env, or a phonehome: section in cloud-config)")
			}

			ctx := cmd.Context()
			var client *phonehome.Client
			apiKeyFn := func() string {
				if client != nil {
					return client.APIKey()
				}
				return ""
			}
			// stopFn is captured before the client exists; the handler resolves
			// it lazily so `unregister` can break the Run loop from inside.
			stopFn := func() {
				if client != nil {
					client.Stop()
				}
			}
			handler := phonehome.DefaultCommandHandler(cfg.URL, apiKeyFn, cfg.IsAllowed, stopFn, scanned, &cfg)
			clientOpts := []phonehome.ClientOption{phonehome.WithCommandHandler(handler)}
			if scanned != nil {
				clientOpts = append(clientOpts, phonehome.WithLogger(scanned.Logger))
			}
			client = phonehome.NewClient(&cfg, clientOpts...)
			return client.Run(ctx)
		},
	}

	flags := cmd.Flags()
	flags.String("url", "", "Phone-home server URL")
	bindEnv(flags, "url", "PHONEHOME_URL")
	flags.String("token", "", "Registration token")
	bindEnv(flags, "token", "PHONEHOME_REGISTRATION_TOKEN")
	flags.String("group", "", "Node group to join")
	bindEnv(flags, "group", "PHONEHOME_GROUP")
	flags.Duration("heartbeat-interval", 30*time.Second, "Heartbeat interval")

	cmd.AddCommand(&cobra.Command{
		Use:   "uninstall",
		Short: "Stop the phone-home service and remove all phone-home configs and credentials from this node",
		Long: "Tears down the phone-home installation on the local node: stops and disables the systemd service, " +
			"removes the unit file, drops the saved credentials, and removes the phone-home cloud-config files. " +
			"Run this after a node has been decommissioned on the server (especially when the server could not " +
			"reach the node to run the teardown remotely).",
		RunE: func(*cobra.Command, []string) error {
			// CLI invocation runs in its own process, so stopping the
			// service unit SIGTERMs the service process (not us) —
			// safe, and ensures we converge even on a node where the
			// service is still up.
			summary, err := phonehome.Uninstall(true)
			if summary != "" {
				fmt.Println(summary)
			}
			if err != nil {
				return fmt.Errorf("uninstall completed with errors: %w", err)
			}
			return nil
		},
	})
	return cmd
}

// firstArg returns the first positional argument, or "" when there is none.
func firstArg(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return args[0]
}

// LoadPhoneHomeConfig returns the phonehome: section of the scanned
// cloud-config, or a zero Config when there is none. A section that does not
// parse is an error: dropping it would leave the URL empty and send the user
// after a missing url key when the real fault is, say, heartbeat_interval: 30
// with no unit.
func LoadPhoneHomeConfig(scanned *sdkConfig.Config) (phonehome.Config, error) {
	cc, ok, err := phonehome.LoadFromCollector(scanned)
	if err != nil {
		return phonehome.Config{}, err
	}
	if !ok {
		return phonehome.Config{}, nil
	}
	return *cc, nil
}

// MergePhoneHomeFlags overlays the phone-home command line on top of the
// configuration the Collector read from cloud-config. A flag wins only when
// the user actually passed it.
//
// The three string flags carry no default, so an empty value already means
// "not given". heartbeat-interval carries a non-zero default, so its value
// alone cannot say that, and asking the parser is the only way to tell a user
// writing --heartbeat-interval=30s apart from a user writing nothing. The unit
// the agent installs runs "kairos-agent phone-home" with no flags, so without
// that question the default would overwrite phonehome.heartbeat_interval on
// every node and the config key would do nothing.
//
// When neither side sets the interval, cfg.HeartbeatInterval stays zero and
// the client falls back to phonehome.DefaultHeartbeatInterval, which is the
// same 30 seconds the flag advertises.
func MergePhoneHomeFlags(flags *pflag.FlagSet, cfg *phonehome.Config) {
	if v, _ := flags.GetString("url"); v != "" {
		cfg.URL = v
	}
	if v, _ := flags.GetString("token"); v != "" {
		cfg.RegistrationToken = v
	}
	if v, _ := flags.GetString("group"); v != "" {
		cfg.Group = v
	}
	if flags.Changed("heartbeat-interval") {
		cfg.HeartbeatInterval, _ = flags.GetDuration("heartbeat-interval")
	}
}

// bootStateFlags are the flags that pick the boot entry an extension command
// acts on. At most one of them may be set.
type bootStateFlags struct {
	active, passive, recovery, common bool
}

// add registers the four flags, with usage text that names what the command
// does for each entry.
func (b *bootStateFlags) add(flags *pflag.FlagSet, active, passive, recovery, common string) {
	flags.BoolVar(&b.active, constants.BootActive, false, active)
	flags.BoolVar(&b.passive, constants.BootPassive, false, passive)
	flags.BoolVar(&b.recovery, constants.BootRecovery, false, recovery)
	flags.BoolVar(&b.common, constants.BootCommon, false, common)
}

func (b *bootStateFlags) moreThanOne() bool {
	return moreThanOneEnabled(b.active, b.passive, b.recovery, b.common)
}

func (b *bootStateFlags) none() bool {
	return noneOfEnabled(b.active, b.passive, b.recovery, b.common)
}

// bootState returns the boot entry the flags select, or "" when none is set.
func (b *bootStateFlags) bootState() string {
	for k, v := range map[string]bool{
		constants.BootActive:   b.active,
		constants.BootPassive:  b.passive,
		constants.BootRecovery: b.recovery,
		constants.BootCommon:   b.common,
	} {
		if v {
			return k
		}
	}
	return ""
}

// checkSysext refuses to run an extension command on a system without
// systemd-sysext, which every one of them ends up calling.
func checkSysext() error {
	_, err := exec.LookPath("systemd-sysext")
	if err != nil {
		return fmt.Errorf("systemd-sysext not found in PATH")
	}
	return nil
}

// nolint:gocyclo // cobra command builder: the subcommand definitions (list, enable, disable, install, remove) live in-line so the whole sysext/confext CLI shape is grep-friendly; splitting them into per-subcommand functions would only move the complexity, not reduce it.
func sysextConfextCommands(extType string) []*cobra.Command {
	var listFlags bootStateFlags
	list := &cobra.Command{
		Use:   "list [--active|--passive|--recovery|--common]",
		Short: "List all the installed extensions",
		Long:  "List all the installed extensions",
		PreRunE: func(*cobra.Command, []string) error {
			if err := checkSysext(); err != nil {
				return err
			}

			if listFlags.moreThanOne() {
				return fmt.Errorf("only one of --active, --passive, --recovery or --common can be set")
			}

			if err := checkRoot(); err != nil {
				return err
			}
			return nil
		},
		RunE: func(*cobra.Command, []string) error {
			cfg, err := agentConfig.Scan(collector.Directories(constants.GetUserConfigDirs()...), collector.NoLogs)
			if err != nil {
				return err
			}
			out, err := action.ListExtensions(cfg, listFlags.bootState(), extType)
			if err != nil {
				return err
			}
			if len(out) == 0 {
				cfg.Logger.Logger.Info().Msg("No system extensions found")
				return nil
			}
			for _, ext := range out {
				cfg.Logger.Info(litter.Sdump(ext))
			}
			return nil
		},
	}
	listFlags.add(list.Flags(),
		"List the extensions for the active boot entry",
		"List the extensions for the passive boot entry",
		"List the extensions for the recovery boot entry",
		"List the extensions for the common boot entry (applies to all boot states)")

	var enableFlags bootStateFlags
	var enableNow bool
	//nolint:dupl // enable and disable are intentionally parallel commands so they can be read side-by-side; refactoring them behind a shared builder makes the CLI definition harder to grep and see all flags at a glance.
	enable := &cobra.Command{
		Use:   "enable [--active|--passive|--recovery|--common] EXTENSION",
		Short: "Enable a installed system extension for a give entry",
		Long:  "Enable a system extension for a given boot entry (active or passive)",
		PreRunE: func(_ *cobra.Command, args []string) error {
			if err := checkSysext(); err != nil {
				return err
			}

			if len(args) != 1 {
				return fmt.Errorf("extension name required")
			}

			if enableFlags.moreThanOne() {
				return fmt.Errorf("only one of --active, --passive, --recovery or --common can be set")
			}

			if enableFlags.none() {
				return fmt.Errorf("either --active, --passive, --recovery or --common must be set")
			}

			if err := checkRoot(); err != nil {
				return err
			}
			return nil
		},
		RunE: func(_ *cobra.Command, args []string) error {
			cfg, err := agentConfig.Scan(collector.Directories(constants.GetUserConfigDirs()...), collector.NoLogs)
			if err != nil {
				return err
			}

			if err := action.EnableExtension(cfg, args[0], enableFlags.bootState(), extType, enableNow); err != nil {
				cfg.Logger.Logger.Error().Err(err).Msg("failed enabling system extension")
				return err
			}
			return nil
		},
	}
	enableFlags.add(enable.Flags(),
		"Enable the system extension for the active boot entry",
		"Enable the system extension for the passive boot entry",
		"List the system extensions for the recovery boot entry",
		"List the system extensions for the common boot entry (applies to all boot states)")
	enable.Flags().BoolVar(&enableNow, "now", false, "Enable the system extension now and reload systemd-sysext")

	var disableFlags bootStateFlags
	var disableNow bool
	//nolint:dupl // parallel to the enable command above; see the note there.
	disable := &cobra.Command{
		Use:   "disable [--active|--passive|--recovery|--common] EXTENSION",
		Short: "Disable a installed system extension for a give entry",
		Long:  "Disable a system extension for a given boot entry (active or passive)",
		PreRunE: func(_ *cobra.Command, args []string) error {
			if err := checkSysext(); err != nil {
				return err
			}

			if len(args) != 1 {
				return fmt.Errorf("extension name required")
			}

			if disableFlags.moreThanOne() {
				return fmt.Errorf("only one of --active, --passive, --recovery or --common can be set")
			}

			if disableFlags.none() {
				return fmt.Errorf("either --active, --passive, --recovery or --common must be set")
			}
			if err := checkRoot(); err != nil {
				return err
			}
			return nil
		},
		RunE: func(_ *cobra.Command, args []string) error {
			cfg, err := agentConfig.Scan(collector.Directories(constants.GetUserConfigDirs()...), collector.NoLogs)
			if err != nil {
				return err
			}
			if err := action.DisableExtension(cfg, args[0], disableFlags.bootState(), extType, disableNow); err != nil {
				cfg.Logger.Logger.Error().Err(err).Msg("failed disabling system extension")
				return err
			}
			return nil
		},
	}
	disableFlags.add(disable.Flags(),
		"Disable the system extension for the active boot entry",
		"Disable the system extension for the passive boot entry",
		"List the system extensions for the recovery boot entry",
		"List the system extensions for the common boot entry (applies to all boot states)")
	disable.Flags().BoolVar(&disableNow, "now", false, "Disable the system extension now and reload systemd-sysext")

	var (
		catalogs       []string
		catalogVersion string
	)
	install := &cobra.Command{
		Use:   "install [--catalog URL]... [--version VERSION] URI|NAME",
		Short: "Install a system extension",
		Long:  "Install a system extension from a URI or catalog",
		PreRunE: func(*cobra.Command, []string) error {
			return checkSysext()
		},
		RunE: func(_ *cobra.Command, args []string) error {
			if len(args) != 1 {
				return fmt.Errorf("extension URI or name required")
			}
			if len(catalogs) > 0 && extType == "confext" {
				return fmt.Errorf("--catalog is only supported for sysext")
			}
			if catalogVersion != "" && extType != "sysext" {
				return fmt.Errorf("--version requires --catalog")
			}
			requested := args[0]
			cfg, err := agentConfig.Scan(collector.Directories(constants.GetUserConfigDirs()...), collector.NoLogs)
			if err != nil {
				return err
			}
			if err := installCatalogOrURIExtension(cfg, extensionCatalogURLs(catalogs, cfg, extType), requested, catalogVersion, extType); err != nil {
				cfg.Logger.Logger.Error().Err(err).Msg("failed installing system extension")
				return err
			}
			cfg.Logger.Logger.Info().Msgf("System extension %s installed", requested)
			return nil
		},
	}
	install.Flags().StringSliceVar(&catalogs, "catalog", nil, "Extension catalog URL, repeatable. Searched in order, the first catalog publishing the name wins. Defaults to `extensions.catalogs` from the cloud config.")
	install.Flags().StringVar(&catalogVersion, "version", "", "Extension version from the catalog. An exact version, or a semver constraint such as \">= 2.1, < 3\"")

	var removeNow bool
	remove := &cobra.Command{
		Use:   "remove EXTENSION",
		Short: "Remove a system extension",
		Long:  "Remove a installed system extension",
		PreRunE: func(*cobra.Command, []string) error {
			return checkSysext()
		},
		RunE: func(_ *cobra.Command, args []string) error {
			if len(args) != 1 {
				return fmt.Errorf("extension required")
			}
			extension := args[0]
			cfg, err := agentConfig.Scan(collector.Directories(constants.GetUserConfigDirs()...), collector.NoLogs)
			if err != nil {
				return err
			}
			if err := action.RemoveExtension(cfg, extension, extType, removeNow); err != nil {
				cfg.Logger.Logger.Error().Err(err).Msg("failed removing system extension")
				return err
			}
			cfg.Logger.Logger.Info().Msgf("System extension %s removed", extension)
			return nil
		},
	}
	remove.Flags().BoolVar(&removeNow, "now", false, "Remove the system extension now and reload systemd-sysext")

	return []*cobra.Command{list, enable, disable, install, remove}
}

// extensionCatalogURLs picks the catalogs to search: the repeated --catalog
// flags when given, otherwise `extensions.catalogs` from the cloud config,
// which itself falls back to the built-in default. Confexts have no catalog.
func extensionCatalogURLs(flagged []string, cfg *sdkConfig.Config, extType string) []string {
	if len(flagged) > 0 {
		return flagged
	}
	if extType != "sysext" {
		return nil
	}
	return cfg.Extensions.CatalogURLs()
}

func installCatalogOrURIExtension(cfg *sdkConfig.Config, catalogURLs []string, requested, version, extType string) error {
	var catalogErr error
	if len(catalogURLs) > 0 {
		err := installCatalogExtension(cfg, catalogURLs, requested, version)
		if err == nil {
			return nil
		}
		// An explicit version can only mean a catalog lookup, so do not go on
		// to read the name as a URI and report a confusing parse error.
		if version != "" {
			return err
		}
		catalogErr = err
	}
	if err := validateSourceSysext(requested); err != nil {
		if catalogErr != nil {
			return catalogErr
		}
		return err
	}
	return action.InstallExtension(cfg, requested, extType)
}

func installCatalogExtension(cfg *sdkConfig.Config, catalogURLs []string, name, version string) error {
	catalogs, err := installer.FetchCatalogs(cfg, catalogURLs)
	if err != nil {
		return err
	}
	uri, err := installer.ResolveURI(cfg, catalogs, extensiontypes.Extension{Name: name, Version: version})
	if err != nil {
		return err
	}
	return action.InstallExtension(cfg, uri, "sysext")
}

// Run runs the kairos-agent CLI with os.Args and returns a process exit code.
// It is called from the standalone kairos-agent binary.
func Run() int {
	// The bus is not initialized here. Initializing it before the command line
	// is parsed loads the providers from the default directories, and the
	// commands that read a provider directory out of the configuration, such
	// as notify, then have nowhere to put it. Each command that uses the bus
	// initializes it, with the directories that command resolved.

	return runApp(NewCommand(), os.Args)
}

// runApp runs cmd with args, args[0] being the program name as in os.Args, and
// returns a process exit code. A fatal error goes to ReportError.
func runApp(cmd *cobra.Command, args []string) int {
	cmd.SetArgs(args[1:])
	if err := cmd.Execute(); err != nil {
		ReportError(err)
		return 1
	}
	return 0
}

// ReportError reports a fatal kairos-agent error. It is written to stderr, so
// stdout carries only what the command was asked to produce and a
// machine-readable output format stays parseable on failure. The multi-call
// kairos binary calls this for a failed agent command too.
//
// The same error also goes out as a progress error event, which is the only
// thing an installer frontend reads. sdk/agentrun runs this binary as
// `kairos-agent manual-install` and shows the frontend its stdout, so a
// failure that never reaches the install action (the config scan,
// CheckConfigForUsers, CheckConfigForExtraPartitions, installSpec.Sanitize,
// validateSource, checkRoot) would otherwise reach the browser as nothing but
// "exit status 1". Emitting here rather than inside InstallAction.Run covers
// those, covers the UKI install action, which never emitted at all, and
// cannot emit the same failure twice, because every install path returns its
// error to this one place. The event is a no-op unless KAIROS_AGENT_PROGRESS
// is set.
func ReportError(err error) {
	progress.EmitError(err.Error())
	fmt.Fprintln(os.Stderr, err)
}

// NewCommand builds the kairos-agent CLI.
func NewCommand() *cobra.Command {
	var global globalFlags

	root := groupCommand(&cobra.Command{
		Use:   "kairos-agent",
		Short: "kairos agent start",
		Long: `
The kairos agent is a component to abstract away node ops, providing a common feature-set across kairos variants.
`,
		Version: version.Version,
		// ReportError writes the error itself, and most failures are not
		// usage mistakes, so the usage text would only bury the reason.
		SilenceErrors:     true,
		SilenceUsage:      true,
		CompletionOptions: cobra.CompletionOptions{DisableDefaultCmd: true},
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			if err := applyEnvFlags(cmd.Flags()); err != nil {
				return err
			}
			setDebug(global.debug)
			return nil
		},
	})

	flags := root.PersistentFlags()
	flags.BoolVar(&global.strictValidation, "strict-validation", false, "Fail instead of warn on validation errors.")
	bindEnv(flags, "strict-validation", "STRICT_VALIDATIONS")
	flags.BoolVar(&global.debug, "debug", false, "enable debug output")
	bindEnv(flags, "debug", "KAIROS_AGENT_DEBUG")

	root.AddCommand(
		newUpgradeCmd(&global),
		newUpgradeFinalizeCmd(),
		newNotifyCmd(),
		newStartCmd(),
		newInstallBundleCmd(),
		newUUIDCmd(),
		newConfigCmd(&global),
		newStateCmd(),
		newRenderTemplateCmd(&global),
		newInteractiveInstallCmd(),
		newManualInstallCmd(&global),
		newInstallCmd(),
		newRecoveryCmd(),
		newResetCmd(),
		newValidateCmd(),
		newPrintSchemaCmd(),
		newRunStageCmd(&global),
		newPullImageCmd(),
		newVersionCmd(),
		newVersioneerCmd(),
		newBootentryCmd(),
		newGrubeditCmd(&global),
		newExtensionCmd("sysext"),
		newExtensionCmd("confext"),
		newLogsCmd(),
		newKcryptCmd(),
		newPhoneHomeCmd(),
	)

	stopFlagsAtFirstArgument(root)
	return root
}

// setDebug turns debug output on when the --debug flag, KAIROS_AGENT_DEBUG or
// rd.kairos.debug on the kernel command line asks for it.
func setDebug(flag bool) {
	var debug bool
	// Get debug from env or cmdline
	cmdline, _ := os.ReadFile("/proc/cmdline")
	if strings.Contains(string(cmdline), "rd.kairos.debug") {
		debug = true
	}

	if os.Getenv("KAIROS_AGENT_DEBUG") == "true" {
		debug = true
	}

	if flag {
		debug = true
	}

	// Set debug from here already, so it's loaded by the Config unmarshall
	viper.Set("debug", debug)
	if debug {
		// Dont hide private fields, we want the full object biew
		litter.Config.HidePrivateFields = false
		// Hide logger and client fields from litter as otherwise the config dumps are huge and a bit useless
		litter.Config.FieldExclusions = regexp.MustCompile(`Logger|logger|Client`)
	}
}

func checkRoot() error {
	if os.Geteuid() != 0 {
		return errors.New("this command requires root privileges")
	}

	return nil
}

// autoInstallFn is a seam so a spec can assert whether interactive-install
// consulted install.auto, without running a real installation.
var autoInstallFn = agent.AutoInstall

const (
	// skipAutoInstallFlag opts interactive-install out of the install.auto
	// check, so the installer runs even on a node whose datasource asks for an
	// unattended install. Off by default: unattended still wins.
	skipAutoInstallFlag = "skip-auto-install"

	// skipAutoInstallCmdline is the boot-cmdline spelling of that flag. The
	// kairos-interactive unit's ExecStart is fixed, so an operator who booted
	// an ISO to look around cannot pass the flag itself; editing the GRUB
	// entry is what they can do.
	skipAutoInstallCmdline = "kairos.skip-auto-install"
)

// cmdlineEnables reports whether a kernel command line turns flag on, either
// bare or as flag=true / flag=1. Whole fields are compared so a longer token
// that merely starts with flag does not enable it.
func cmdlineEnables(cmdline, flag string) bool {
	for _, field := range strings.Fields(cmdline) {
		switch field {
		case flag, flag + "=true", flag + "=1":
			return true
		}
	}

	return false
}

// skipAutoInstall reports whether the operator asked interactive-install to
// ignore install.auto. Three ways in, none of them the default: the flag, the
// KAIROS_SKIP_AUTO_INSTALL env var (both handled by the flag itself), and the
// boot cmdline. flag is the value of the flag, which the env var also sets.
func skipAutoInstall(flag bool) bool {
	if flag {
		return true
	}

	cmdline, err := os.ReadFile("/proc/cmdline")
	if err != nil {
		return false
	}

	return cmdlineEnables(string(cmdline), skipAutoInstallCmdline)
}

// runStageCloudInitPaths resolves the cloud-init paths a `run-stage` run reads,
// and keeps config.CloudInitPaths in step with them.
//
// --cloud-init-paths adds to the defaults; --override-cloud-init-paths replaces
// everything, defaults included, which is what its usage text has always
// promised. The composition lives here rather than in RunStage so that every
// other caller of RunStage (install, upgrade, reset, the boot hooks) keeps
// reading the defaults.
func runStageCloudInitPaths(extra, override []string, config *sdkConfig.Config) []string {
	if len(extra) > 0 {
		config.CloudInitPaths = append(config.CloudInitPaths, extra...)
	}

	if len(override) > 0 {
		config.CloudInitPaths = override
		return override
	}

	return utils.CloudInitPaths(config)
}

// checkInstallBundleFlags refuses a flag the target's scheme cannot read.
// Only LuetInstaller reads Repository, and only the two OCI installers read
// LocalFile, so each flag used to be accepted on the other scheme and then
// dropped without a word: the operator who passed --repository to a
// container:// bundle got the image the target named, from the registry the
// target named, and no sign that the flag had been ignored.
//
// An unreadable target is left alone here so that RunBundles keeps reporting
// it, with the bundle name it already puts in that message.
func checkInstallBundleFlags(flags *pflag.FlagSet, target string) error {
	scheme, err := (&bundles.BundleConfig{Target: target}).TargetScheme()
	if err != nil {
		return nil //nolint:nilerr // RunBundles reports the invalid target
	}

	if scheme == "package" {
		if flags.Changed("local-file") {
			return fmt.Errorf("--local-file is not read by a package:// target, it applies to container://, docker:// and run:// targets (unset the flag or the LOCAL_FILE environment variable)")
		}
		return nil
	}

	if flags.Changed("repository") {
		return fmt.Errorf("--repository is only read by a package:// target, and %s is a %s:// target (unset the flag or the REPOSITORY environment variable)", target, scheme)
	}

	return nil
}

func validateSource(source string) error {
	if source == "" {
		return nil
	}

	r, err := regexp.Compile(`^oci:|^dir:|^file:|^ocifile:`)
	if err != nil {
		return err
	}
	if !r.MatchString(source) {
		return fmt.Errorf("source %s does not match any of oci:, dir:, file: or ocifile: ", source)
	}

	return nil
}

func validateSourceSysext(source string) error {
	if source == "" {
		return nil
	}

	r, err := regexp.Compile(`^oci:|^file:|^http:|^https:`)
	if err != nil {
		return err
	}
	if !r.MatchString(source) {
		return fmt.Errorf("source %s does not match any of oci:, file: or http(s): ", source)
	}

	return nil
}

// Check
func bootFromLiveMedia() bool {
	// Check if the system is booted from a LIVE media by checking if the file /run/cos/livecd is present
	r, err := state.NewRuntime()
	if err != nil {
		return false
	}
	if r.BootState == state.LiveCD || r.BootState == state.Unknown {
		return true
	}

	return false
}

// releaseCollector gathers the releases every provider answers with.
//
// go-pluggable runs each provider in its own goroutine and emits each result to
// the response listeners the same way, so collect is called concurrently when
// more than one provider answers. Unmarshalling straight into a shared slice
// both races and replaces what the previous provider contributed, which is why
// the answers are appended under a lock instead.
type releaseCollector struct {
	mu   sync.Mutex
	tags []string
}

// collect adds the releases carried by one provider's response.
func (rc *releaseCollector) collect(data string) {
	if data == "" {
		return
	}

	var tags []string
	if err := json.Unmarshal([]byte(data), &tags); err != nil {
		fmt.Printf("warn: failed unmarshalling data: '%s'\n", err.Error())
		return
	}

	rc.mu.Lock()
	defer rc.mu.Unlock()
	rc.tags = append(rc.tags, tags...)
}

// releases returns everything collected so far.
func (rc *releaseCollector) releases() []string {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	return rc.tags
}

func getReleasesFromProvider(includePrereleases bool) ([]string, error) {
	bus.Manager.Initialize()

	rc := &releaseCollector{}
	bus.Manager.Response(events.EventAvailableReleases, func(p *pluggable.Plugin, r *pluggable.EventResponse) {
		rc.collect(r.Data)
	})

	configYAML := fmt.Sprintf("IncludePreReleases: %t", includePrereleases)
	_, err := bus.Manager.Publish(events.EventAvailableReleases, events.EventPayload{Config: configYAML})
	if err != nil {
		return rc.releases(), fmt.Errorf("failed publishing event: %w", err)
	}

	return rc.releases(), nil
}

func moreThanOneEnabled(bools ...bool) bool {
	count := 0
	for _, b := range bools {
		if b {
			count++
		}
		if count > 1 {
			return true
		}
	}
	return false
}

func noneOfEnabled(bools ...bool) bool {
	for _, b := range bools {
		if b {
			return false // Found at least one true, so not "none of enabled"
		}
	}
	return true // No true values found, so "none of enabled"
}
