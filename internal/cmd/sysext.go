package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/gofrs/uuid"
	aurorabootUtils "github.com/kairos-io/AuroraBoot/pkg/utils"
	"github.com/kairos-io/kairos/v4/sdk/sysext"
	"github.com/kairos-io/kairos/v4/sdk/types/logger"
	sdkImage "github.com/kairos-io/kairos/v4/sdk/utils/image"
	"github.com/urfave/cli/v2"
)

// Sysext/confext info: https://www.freedesktop.org/software/systemd/man/latest/systemd-sysext.html
// System extension images may – dynamically at runtime — extend the /usr/ and /opt/ directory hierarchies with additional files.
// Configuration extension images may – dynamically at runtime — extend the /etc/ directory hierarchy with additional files.

// SysextCmd generates a sysextension from the last layer of the given CONTAINER
var SysextCmd = cli.Command{
	Name:      "sysext",
	Usage:     "Generate a sysextension from the last layer of the given CONTAINER",
	ArgsUsage: "<name> <container>",

	Flags: append(
		commonFlagsSysextConfext(),
		&cli.BoolFlag{
			Name:    "service-reload",
			Aliases: []string{"service-load"},
			Value:   false,
			Usage:   "Make systemctl reload the service when loading the sysext. This is useful for sysext that provide systemd service files.",
		},
		// Deprecated: prefer --include-path=/opt. Kept as an indefinite alias --
		// the cost of carrying it is negligible; breaking scripts isn't.
		&cli.BoolFlag{
			Name:  "with-opt",
			Value: false,
			Usage: "Deprecated: prefer --include-path=/opt. Include files from /opt in the sysext.",
		},
		&cli.StringSliceFlag{
			Name:  "include-path",
			Usage: "Filesystem path to extract from the image layer (repeatable). /usr is always included.",
		},
	),
	Before: validateSysextConfextArgs,
	Action: generateSysextConfext,
}

// ConfextCmd generates a confextension from the last layer of the given CONTAINER
var ConfextCmd = cli.Command{
	Name:      "confext",
	Usage:     "Generate a confextension from the last layer of the given CONTAINER",
	ArgsUsage: "<name> <container>",

	Flags:  commonFlagsSysextConfext(),
	Before: validateSysextConfextArgs,
	Action: generateSysextConfext,
}

// commonFlagsSysextConfext returns the common flags for both sysext and confext commands
func commonFlagsSysextConfext() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:     "private-key",
			Value:    "",
			Usage:    "Private key to sign the sysext with",
			Required: false,
		},
		&cli.StringFlag{
			Name:     "certificate",
			Usage:    "Certificate to sign the sysext with",
			Required: false,
		},
		&cli.StringFlag{
			Name:  "output",
			Usage: "Output dir",
		},
		&cli.StringFlag{
			Name:  "arch",
			Value: "amd64",
			Usage: "Arch to get the image from and build the sysext for. Accepts amd64, arm64, and riscv64 values.",
		},
		&cli.BoolFlag{
			Name:  "debug",
			Value: false,
			Usage: "Enable debug logging",
		},
	}
}

// checkPackingIdentity refuses a build that cannot produce a root owned
// extension.
//
// The staging directory is filled by the layer extraction and by the
// extension-release file this command writes, neither of which chowns
// anything, and systemd-repart then copies that tree with the uid and gid it
// finds: repart.d has no ownership directive, and giving the tree away needs
// CAP_CHOWN. So a non-root build can only pack its own uid, and on a node that
// merges the extension every path in it answers `find / -xdev -nouser` and
// fails CIS DIL 6.1.11 and 6.1.12. The symptom appears on the installed
// machine, long after the build reported success, so say it here instead.
func checkPackingIdentity(buildType string, euid int) error {
	if euid == 0 {
		return nil
	}
	if buildType == "" {
		buildType = "extension"
	}
	return fmt.Errorf(
		"building a %s needs root: systemd-repart packs the staging tree with the uid and gid it finds, so running as uid %d would produce an extension whose files no Kairos node owns (CIS DIL 6.1.11 and 6.1.12 report every one of them). Run auroraboot as root: drop --user from the container run, unset securityContext.runAsUser in the Helm values, or run the host binary with sudo",
		buildType, euid)
}

// supportedArches are the values --arch accepts, in the Go/OCI spelling the
// flag takes and the image platform uses. Keep it as the single source for
// both the flag validation and systemdArchitecture: the two used to be
// separate literals, riscv64 was added to one of them only, and a riscv64
// extension shipped declaring itself x86-64.
var supportedArches = []string{"amd64", "arm64", "riscv64"}

// systemdArchitecture returns the name systemd's architecture table uses for
// a --arch value, which is what belongs in the ARCHITECTURE= field of an
// extension-release file.
//
// The two namespaces are not the same namespace. systemd spells amd64
// "x86-64" and agrees with Go on arm64 and riscv64
// (systemd src/basic/architecture.c, architecture_to_string).
//
// Getting it wrong is silent. systemd-sysext compares the field against the
// running architecture and, when they differ, SKIPS the extension and reports
// success, logging the reason at debug level only
// (systemd src/shared/extension-util.c, extension_release_validate). The node
// boots with the .raw present and nothing merged. So report an unknown
// architecture as an error here rather than falling back to a default: a loud
// build failure is the only failure mode an operator can act on.
func systemdArchitecture(arch string) (string, error) {
	switch arch {
	case "amd64":
		return "x86-64", nil
	case "arm64":
		return "arm64", nil
	case "riscv64":
		return "riscv64", nil
	default:
		return "", fmt.Errorf("unsupported architecture: %s", arch)
	}
}

// validateSysextConfextArgs validates the arguments for both sysext and confext commands
func validateSysextConfextArgs(ctx *cli.Context) error {
	arch := ctx.String("arch")
	if !slices.Contains(supportedArches, arch) {
		return fmt.Errorf("unsupported architecture: %s", arch)
	}
	if ctx.NArg() < 2 {
		return fmt.Errorf("missing required arguments: <name> <container>")
	}
	if err := checkPackingIdentity(ctx.Command.Name, os.Geteuid()); err != nil {
		return err
	}
	return nil
}

// generateSysextConfext generates a sysext or confext based on the command name and the last layer of the given container image
func generateSysextConfext(ctx *cli.Context) error {
	buildType := ctx.Command.Name
	level := "info"
	if ctx.Bool("debug") {
		level = "debug"
	}
	logger := logger.NewKairosLogger("auroraboot", level, false)
	args := ctx.Args()

	name := args.Get(0)
	if _, err := os.Stat(fmt.Sprintf("%s.%s.raw", name, buildType)); err == nil {
		_ = os.Remove(fmt.Sprintf("%s.%s.raw", name, buildType))
	}
	logger.Infof("🚀 Start %s creation", buildType)

	dir, err := os.MkdirTemp("", fmt.Sprintf("auroraboot-%s-", buildType))
	if err != nil {
		return fmt.Errorf("creating temp directory: %w", err)
	}
	defer func(path string) {
		err := os.RemoveAll(path)
		if err != nil {
			logger.Logger.Error().Str("dir", dir).Err(err).Msg("⛔ removing dir")
		}
	}(dir)
	logger.Logger.Debug().Str("dir", dir).Msg("creating directory")

	// Get the image struct
	logger.Info("💿 Getting image info")
	platform := fmt.Sprintf("linux/%s", ctx.String("arch"))
	image, err := sdkImage.GetImage(args.Get(1), platform, nil, nil)
	if err != nil {
		logger.Logger.Error().Str("image", args.Get(1)).Err(err).Msg("⛔ getting image")
		return err
	}

	// We only want to extract files from /usr for sysext and /etc for confext, so we create a regex allowlist based on the build type.
	// Operators can extend the allowlist via --include-path (repeatable) or the
	// legacy --with-opt alias. Paths flow into SYSTEMD_SYSEXT_HIERARCHIES at
	// boot time (AuroraBoot bakes the drop-in via extensionHierarchies on the
	// artifact create payload).
	allowList := regexp.MustCompile(`^usr/*|^/usr/*`)
	var includes []string
	if buildType == "sysext" {
		includes = includePathsFromFlags(ctx)
		if len(includes) > 0 {
			parts := []string{`^usr/*`, `^/usr/*`}
			for _, p := range includes {
				p = strings.TrimPrefix(strings.TrimSpace(p), "/")
				if p == "" {
					continue
				}
				parts = append(parts, "^"+regexp.QuoteMeta(p)+"/*", "^/"+regexp.QuoteMeta(p)+"/*")
			}
			logger.Logger.Debug().Strs("includes", includes).Msg("extending sysext allowlist")
			allowList = regexp.MustCompile(strings.Join(parts, "|"))
		}
	}
	// The directory where the extension-release file will be created, based on the build type
	extensionReleaseDir := filepath.Join(dir, "/usr/lib/extension-release.d/")

	// If its a confext, we change the allowlist and the extension release dir to match /etc instead of /usr
	if buildType == "confext" {
		allowList = regexp.MustCompile(`^etc/*|^/etc/*`)
		extensionReleaseDir = filepath.Join(dir, "/etc/extension-release.d/")
	}
	// extract the files into the temp dir
	logger.Info("📤 Extracting archives from image layer")
	err = sysext.ExtractFilesFromLastLayer(image, dir, logger, allowList)
	if err != nil {
		logger.Logger.Error().Str("image", args.Get(1)).Err(err).Msg("⛔ extracting layer")
		return err
	}

	// Strip any os-release the base image happens to ship: systemd-sysext
	// refuses to merge an extension that contains /usr/lib/os-release
	// ("Extension contains '/usr/lib/os-release', which is not allowed,
	// refusing.") because it would let the extension redefine the host OS
	// identity. The extension-release.d file we write below is the correct
	// place to declare compat.
	for _, p := range []string{"usr/lib/os-release", "etc/os-release"} {
		if rerr := os.Remove(filepath.Join(dir, p)); rerr != nil && !os.IsNotExist(rerr) {
			logger.Logger.Warn().Str("path", p).Err(rerr).Msg("could not strip base os-release")
		}
	}

	// A sysext may carry anything under /usr, and --include-path widens that
	// further, but a Kairos node merges only the hierarchies named in
	// SYSTEMD_SYSEXT_HIERARCHIES. Files outside them are packed into the image
	// and then silently never appear, so the only symptom is a missing file on
	// a build that reported success. Say so while the operator can still act on
	// it. Confext merges /etc, which the list does not govern.
	if buildType == "sysext" {
		if werr := warnUnmergedHierarchies(dir, includes, func(line string) {
			logger.Logger.Warn().Msg(line)
		}); werr != nil {
			// The tree was just extracted into a directory we own, so a walk
			// failure says something is wrong with it, not with the check.
			logger.Logger.Error().Str("dir", dir).Err(werr).Msg("⛔ checking extension hierarchies")
			return werr
		}
	}

	// Now create the file that tells systemd that this is a sysext/confext!
	err = os.MkdirAll(extensionReleaseDir, os.ModeDir|os.ModePerm)
	if err != nil {
		logger.Logger.Error().Str("dir", extensionReleaseDir).Err(err).Msg("⛔ creating dir")
		return err
	}

	arch, err := systemdArchitecture(ctx.String("arch"))
	if err != nil {
		logger.Logger.Error().Str("arch", ctx.String("arch")).Err(err).Msg("⛔ writing release file")
		return err
	}

	extensionData := fmt.Sprintf("ID=_any\nARCHITECTURE=%s", arch)

	// If the extension ships any service files, we want this so systemd is reloaded and the service available immediately
	if ctx.Bool("service-reload") && buildType == "sysext" {
		extensionData = fmt.Sprintf("%s\nEXTENSION_RELOAD_MANAGER=1", extensionData)
	}
	logger.Logger.Debug().Str("file", fmt.Sprintf("extension-release.%s", name)).Str("content", extensionData).Msg("creating release file")
	err = os.WriteFile(filepath.Join(extensionReleaseDir, fmt.Sprintf("extension-release.%s", name)), []byte(extensionData), os.ModePerm)
	if err != nil {
		logger.Logger.Error().Str("file", fmt.Sprintf("extension-release.%s", name)).Err(err).Msg("⛔ creating releasefile")
		return err
	}

	logger.Logger.Info().Msgf("📦 Packing %s into raw image", buildType)
	// Call systemd-repart to create the sysext/confext based off the files
	outputFile := fmt.Sprintf("%s.%s.raw", name, buildType)
	if outputDir := ctx.String("output"); outputDir != "" {
		outputFile = filepath.Join(outputDir, outputFile)
	}
	// Call systemd-repart to create the sysext/confext based off the files.
	// --offline=yes tells systemd-repart to write partition data directly to
	// the output file instead of attaching it to a host loop device. The
	// output is a regular file we own, so a loop attach is unnecessary; more
	// importantly, on shared CI runners the per-host loop pool can be
	// exhausted by concurrent jobs and systemd-repart fails with
	// "Failed to make loopback device: Device or resource busy".
	//
	// --make-ddi=sysext selects systemd's own sysext.repart.d definitions, and
	// those copy /usr and /opt only. Extracting a third hierarchy into the
	// staging directory is therefore not enough: repart drops it silently and
	// the image is packed without it. When the operator asks for a hierarchy
	// the built-in definitions do not carry, write an equivalent definition
	// directory of our own and point repart at it instead.
	extra := extraHierarchies(includes)
	var ddiFlags []string
	if buildType == "sysext" && len(extra) > 0 {
		defDir, derr := writeSysextDefinitions(extra)
		if derr != nil {
			logger.Logger.Error().Err(derr).Msg("⛔ writing repart definitions")
			return derr
		}
		defer func(path string) {
			if rerr := os.RemoveAll(path); rerr != nil {
				logger.Logger.Error().Str("dir", path).Err(rerr).Msg("⛔ removing dir")
			}
		}(defDir)
		logger.Logger.Debug().Str("dir", defDir).Strs("hierarchies", extra).
			Msg("packing extra hierarchies with a generated repart definition")
		// --make-ddi= and --definitions= are mutually exclusive, so replace it
		// with the two settings it implies: create the output file, size it
		// from the content.
		ddiFlags = []string{
			fmt.Sprintf("--definitions=%s", defDir),
			"--empty=create",
			"--size=auto",
		}
	} else {
		ddiFlags = []string{fmt.Sprintf("--make-ddi=%s", buildType)}
	}
	cmdArgs := append(ddiFlags,
		"--offline=yes",
		"--image-policy=root=verity+signed+absent:usr=verity+signed+absent",
		fmt.Sprintf("--architecture=%s", arch),
		// Having a fixed predictable seed makes the Image UUID be always the same if the inputs are the same,
		// so its a reproducible image. So getting the same files and same cert/key should produce a reproducible image always
		// Another layer to verify images, even if its a manual check, we make it easier
		fmt.Sprintf("--seed=%s", uuid.NewV5(uuid.NamespaceDNS, fmt.Sprintf("kairos-%s", buildType))),
		fmt.Sprintf("--copy-source=%s", dir),
		outputFile, // output file
	)
	// Add signing flags or exclude partitions based on whether key/cert are provided
	cmdArgs = append(cmdArgs, aurorabootUtils.GetSysextSigningFlags(ctx.String("private-key"), ctx.String("certificate"))...)
	command := exec.Command("systemd-repart", cmdArgs...)
	out, err := command.CombinedOutput()
	logger.Logger.Debug().Str("output", string(out)).Msgf("building %s", buildType)
	if err != nil {
		logger.Logger.Error().Err(err).
			Str("command", strings.Join(command.Args, " ")).
			Str("output", string(out)).
			Msgf("⛔ building %s failed", buildType)
		return err
	}

	logger.Logger.Info().Str("output", outputFile).Msgf("🎉 Done %s creation", buildType)
	return nil
}

// builtinSysextHierarchies are the hierarchies systemd's own
// sysext.repart.d definitions copy into the image. Anything else has to be
// declared in a definition directory we write ourselves.
var builtinSysextHierarchies = []string{"/usr", "/opt"}

// extraHierarchies returns the requested hierarchies systemd's built-in
// sysext definitions do not copy, normalized to a leading slash, deduplicated
// and sorted so the generated definition is reproducible.
func extraHierarchies(includes []string) []string {
	seen := map[string]struct{}{}
	for _, p := range builtinSysextHierarchies {
		seen[p] = struct{}{}
	}
	var out []string
	for _, p := range includes {
		p = "/" + strings.Trim(strings.TrimSpace(p), "/")
		if p == "/" {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// writeSysextDefinitions writes a systemd-repart definition directory that
// mirrors systemd's sysext.repart.d, with one CopyFiles= line per hierarchy:
// the two built-in ones plus everything in extra. It returns the directory,
// which the caller owns and must remove.
//
// A CopyFiles= source that is absent from the staging directory is skipped by
// repart with a notice, so listing a hierarchy the image layer did not carry
// is harmless.
func writeSysextDefinitions(extra []string) (string, error) {
	dir, err := os.MkdirTemp("", "auroraboot-repart-")
	if err != nil {
		return "", fmt.Errorf("creating definitions directory: %w", err)
	}

	var copyFiles strings.Builder
	for _, p := range append(append([]string(nil), builtinSysextHierarchies...), extra...) {
		fmt.Fprintf(&copyFiles, "CopyFiles=%s/\n", p)
	}

	files := map[string]string{
		"10-root.conf": "[Partition]\n" +
			"Type=root\n" +
			"Format=erofs\n" +
			copyFiles.String() +
			"Verity=data\n" +
			"VerityMatchKey=root\n" +
			"Minimize=best\n",
		"20-root-verity.conf": "[Partition]\n" +
			"Type=root-verity\n" +
			"Verity=hash\n" +
			"VerityMatchKey=root\n" +
			"Minimize=best\n",
		"30-root-verity-sig.conf": "[Partition]\n" +
			"Type=root-verity-sig\n" +
			"Verity=signature\n" +
			"VerityMatchKey=root\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
			_ = os.RemoveAll(dir)
			return "", fmt.Errorf("writing %s: %w", name, err)
		}
	}
	return dir, nil
}

// includePathsFromFlags merges --include-path entries with the legacy
// --with-opt boolean (which is equivalent to --include-path=/opt) and
// dedupes the result. A one-time deprecation warning fires on stderr when
// --with-opt is set so existing scripts keep working without log spam.
func includePathsFromFlags(ctx *cli.Context) []string {
	out := append([]string(nil), ctx.StringSlice("include-path")...)
	if ctx.Bool("with-opt") {
		fmt.Fprintln(os.Stderr,
			"auroraboot sysext: --with-opt is deprecated; use --include-path=/opt instead.")
		out = append(out, "/opt")
	}
	seen := map[string]struct{}{}
	dedup := out[:0]
	for _, p := range out {
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		dedup = append(dedup, p)
	}
	return dedup
}
