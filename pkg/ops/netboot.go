package ops

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/diskfs/go-diskfs"
	"github.com/kairos-io/AuroraBoot/internal"
	"github.com/kairos-io/AuroraBoot/pkg/constants"
	"github.com/kairos-io/AuroraBoot/pkg/netboot"
	"github.com/kairos-io/AuroraBoot/pkg/schema"
	agentconstants "github.com/kairos-io/kairos/v4/agent/pkg/constants"
	"github.com/kairos-io/kairos/v4/sdk/iso"
	extensiontypes "github.com/kairos-io/kairos/v4/sdk/types/extensions"
	"gopkg.in/yaml.v3"
)

// valueGetOnCall is a function type that returns a string value.
// It is used to defer the retrieval of a value until the function is called,
// allowing for dynamic values to be fetched at the time of the call.
// This is mainly due to the fact that the deployer might not have the values available at the time of the registration,
// but rather at the time of the call, e.g. when the ISO file is downloaded or the destination directory is created.
type valueGetOnCall func() string

// DefaultNetbootPrefix is the base name of the netboot artifacts when the
// configuration names none.
const DefaultNetbootPrefix = "kairos"

// NetbootArtifactPaths returns the three paths ExtractNetboot writes under dst
// for the given prefix, which is iso.name from the configuration.
//
// The server that serves those files has to ask for the same three names, so
// the producer and the consumer both read them from here. Spelling them twice
// is what made a run with iso.name set extract <name>-kernel and then serve
// kairos-kernel.
func NetbootArtifactPaths(dst, prefix string) (squashFS, kernel, initrd string) {
	if prefix == "" {
		prefix = DefaultNetbootPrefix
	}

	return filepath.Join(dst, fmt.Sprintf("%s.squashfs", prefix)),
		filepath.Join(dst, fmt.Sprintf("%s-kernel", prefix)),
		filepath.Join(dst, fmt.Sprintf("%s-initrd", prefix))
}

// NetbootGrubCfgPath is the path ExtractNetboot writes the livecd grub config
// to, for the given prefix, which is iso.name from the configuration.
//
// The server reads that file back to build the netboot cmdline, so it has to
// ask for the same name. Spelling it on only one side is what made a run with
// iso.name set write <name>-grub.cfg and then look for kairos-grub.cfg.
func NetbootGrubCfgPath(dst, prefix string) string {
	if prefix == "" {
		prefix = DefaultNetbootPrefix
	}

	return filepath.Join(dst, fmt.Sprintf("%s-grub.cfg", prefix))
}

// ExtractNetboot extracts all the required netbooting artifacts
// isoFunc is a function that returns the path to the ISO file
// we need the function to be passed so its executed in the context of the deployer as otherwise
// the ISO file might not be available at the time of the call
func ExtractNetboot(isoFunc, dstFunc valueGetOnCall, prefix string) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		src := isoFunc()
		dst := dstFunc()

		if _, err := os.Stat(dst); err != nil && os.IsNotExist(err) {
			return fmt.Errorf("destination directory %s does not exist: %w", dst, err)
		}
		internal.Log.Logger.Info().Str("prefix", prefix).Str("source", src).Str("destination", dst).Msg("Extracting netboot artifacts")

		squashFS, kernel, initrd := NetbootArtifactPaths(dst, prefix)

		for _, a := range []struct {
			inIso, artifact string
		}{
			{"/rootfs.squashfs", squashFS},
			{"/boot/kernel", kernel},
			{"/boot/initrd", initrd},
		} {
			if err := iso.ExtractFileFromIso(a.inIso, src, a.artifact, &internal.Log); err != nil {
				internal.Log.Logger.Error().Err(err).Str("artifact", a.artifact).Str("source", src).Str("destination", dst).Msgf("Failed extracting netboot artfact")
				return err
			}
		}

		// The livecd grub config holds the cmdline the ISO boots with, which
		// the netboot cmdline is meant to match. Not every ISO has one, so a
		// miss only costs the caller those options: hence the nil logger, the
		// error of an expected miss does not belong on the Error channel.
		artifact := NetbootGrubCfgPath(dst, prefix)
		if err := iso.ExtractFileFromIso(filepath.Join(constants.GrubPrefixDir, constants.GrubCfg), src, artifact, nil); err != nil {
			internal.Log.Logger.Warn().Err(err).Str("artifact", artifact).Str("source", src).Msg("No livecd grub config in the ISO, netboot will boot with the default cmdline")
			_ = os.Remove(artifact)
		}

		carried, err := extractNetbootExtensions(src, dst)
		if err != nil {
			internal.Log.Logger.Error().Err(err).Str("source", src).Str("destination", dst).Msg("Failed extracting the bundled extensions")
			return err
		}
		if len(carried) > 0 {
			internal.Log.Logger.Info().Strs("extensions", carried).Str("destination", dst).Msg("Extracted the bundled system extensions")
		}
		internal.Log.Logger.Info().Msg("Artifacts extracted")

		return nil
	}
}

func StartPixiecore(cloudConfigFile, address, netbootPort string, squashFSfileGet, initrdFileGet, kernelFileGet, grubCfgFileGet valueGetOnCall, nb schema.NetBoot) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		internal.Log.Logger.Info().Msgf("Start pixiecore")
		// do them in the context of the deployer so we can use the functions at the time of the call
		squashFSfile := squashFSfileGet()
		initrdFile := initrdFileGet()
		kernelFile := kernelFileGet()

		line, err := netbootCmdline(squashFSfile, cloudConfigFile, grubCfgFileGet, nb)
		if err != nil {
			return err
		}
		internal.Log.Logger.Info().Str("cmdline", line).Msg("Netbooting")

		return netboot.Server(kernelFile, line, address, netbootPort, initrdFile, true)
	}
}

// netbootCmdline renders the kernel command line pixiecore hands the node.
//
// The pixiecore placeholders are expanded before anything else is merged in.
// Everything appended afterwards is operator-supplied or ISO-supplied text,
// and a % in it would otherwise be read as a format verb: fmt replaces it
// with %!(NOVERB) and a stray verb can swallow the option that follows it.
//
// The bundled extensions are read out of the directory holding the squashfs,
// which is the netboot directory in every caller: the deployer builds all
// three paths under it, the netboot manager looks them up there, and
// `auroraboot start-pixie` is handed them. Deriving it rather than taking it
// as an argument is what makes the web dashboard's netboot server, which goes
// through the start-pixie subprocess, behave the same as an in-process CLI
// build.
//
// The default carries install-mode-interactive because a netboot is an
// install. immucore turns that keyword into /run/cos/install_mode_interactive
// (sdk/state.InstallIntentSentinels), and the bundled 52_installer.yaml gates
// kairos-interactive.service on that file. Nothing else on a netbooted node
// runs the install: kairos-agent start stands aside when the config asks for
// an unattended install. The rest of the guard already holds on a netboot,
// because a cmdline carrying netboot is a LiveCD boot to kairos-sdk and
// immucore writes /run/cos/live_mode for it. See kairos-io/kairos#5373.
//
// The interactive installer rather than the plain one, because it is the
// dispatcher: it runs AutoInstall first, so a config carrying install.auto
// still installs unattended, and it offers the go-nodepair pairing flow from
// its welcome screen. That is what the legacy nodepair.enable keyword used to
// ask for, and that keyword starts no installer at all since
// kairos-io/kairos#5464.
//
// An explicit netboot.cmdline replaces the whole default, keeping only the two
// keys netboot has to control, so an operator who wants a live node that does
// not install still has a way to ask for one.
//
// The livecd options are merged in before the extensions are appended. The
// merge rewrites the option list, while the kairos.extensions value is a list
// of pixiecore {{ ID }} templates this build minted and nothing else may
// reorder or deduplicate.
func netbootCmdline(squashFSfile, cloudConfigFile string, grubCfgFileGet valueGetOnCall, nb schema.NetBoot) (string, error) {
	// The two served paths are expanded on their own, before anything else is
	// joined on. Everything appended afterwards is operator-supplied or
	// ISO-supplied text, and a % in it would otherwise be read as a format
	// verb: fmt replaces it with %!(NOVERB) and a stray verb can swallow the
	// option that follows it.
	var line string
	if nb.Cmdline != "" {
		line = fmt.Sprintf(`root=live:{{ ID "%s" }} config_url={{ ID "%s" }}`, squashFSfile, cloudConfigFile) + " " + nb.Cmdline
	} else {
		line = fmt.Sprintf(`rd.live.overlay.overlayfs rd.neednet=1 ip=dhcp rd.cos.disable root=live:{{ ID "%s" }} netboot install-mode-interactive config_url={{ ID "%s" }} console=tty1 console=ttyS0 console=tty0`, squashFSfile, cloudConfigFile)

		// Without an explicit override, take the options the ISO itself boots
		// with instead of leaving this list to drift away from it.
		// See kairos-io/kairos#2573.
		line = withLiveCmdline(line, grubCfgFileGet)
	}

	declaration, err := extensionsCmdline(filepath.Dir(squashFSfile), nb.Cmdline)
	if err != nil {
		return "", err
	}
	if declaration != "" {
		internal.Log.Logger.Info().Str("declaration", declaration).Msg("Declaring the bundled extensions on the netboot command line")
		line += " " + declaration
	}
	return line, nil
}

// withLiveCmdline merges the livecd grub config extracted by ExtractNetboot
// into the netboot cmdline. A grub config that is absent or unreadable is not
// an error: the caller may be netbooting an ISO that has none, and the cmdline
// is then used as it is.
func withLiveCmdline(cmdLine string, grubCfgFileGet valueGetOnCall) string {
	if grubCfgFileGet == nil {
		return cmdLine
	}

	grubCfgFile := grubCfgFileGet()
	if grubCfgFile == "" {
		return cmdLine
	}

	grubCfg, err := os.ReadFile(grubCfgFile)
	if err != nil {
		internal.Log.Logger.Warn().Err(err).Str("grubCfg", grubCfgFile).Msg("Could not read the livecd grub config, netbooting with the default cmdline")
		return cmdLine
	}

	merged := netboot.LiveCmdline(cmdLine, string(grubCfg))
	internal.Log.Logger.Debug().Str("grubCfg", grubCfgFile).Msg("Netboot cmdline taken from the livecd grub config")

	return merged
}

// extensionsCmdline renders the kairos.extensions keyword for the extensions
// bundled in the netboot directory, or "" when there are none.
//
// A netbooted node has no live media to sweep and no build-time URL to put in
// the cloud config, because the config AuroraBoot serves is written before the
// server knows the address a client will reach it on. Pixiecore's
// `{{ ID "<path>" }}` mints a served URL per boot while it expands the
// cmdline template, which is the one channel that works in both netboot modes
// and is already how config_url is delivered. kairos-agent reads the keyword
// (kairos-io/kairos#5073) and treats each entry as it treats an
// install.extensions entry. See kairos-io/kairos#5040.
//
// An operator cmdline that already names the keyword wins outright and this
// adds nothing: kairos-agent accumulates a repeated key, so emitting ours too
// would install the union of two sets neither side asked for. A value the
// agent will refuse is reported here rather than left for the install, since
// the build knows about it and every node that boots this server would hit
// it.
func extensionsCmdline(netbootDir, operatorCmdline string) (string, error) {
	declared, err := extensiontypes.ParseCmdline(operatorCmdline)
	if err != nil {
		return "", fmt.Errorf("the configured netboot cmdline declares extensions the agent will refuse: %w", err)
	}
	if len(declared) > 0 {
		internal.Log.Logger.Info().Str("key", extensiontypes.CmdlineKey).
			Msg("The configured cmdline already declares extensions, leaving it alone")
		return "", nil
	}

	images, err := NetbootExtensionImages(netbootDir)
	if err != nil {
		return "", err
	}
	if len(images) == 0 {
		return "", nil
	}

	ids := make([]string, 0, len(images))
	for _, image := range images {
		ids = append(ids, fmt.Sprintf("{{ ID %q }}", image))
	}
	return extensiontypes.CmdlineKey + "=" + strings.Join(ids, ","), nil
}

// NetbootExtensionImages returns the bundled extension images in a netboot
// directory, in the order they were declared.
//
// ExtractNetboot carries the declaration out of the ISO along with the images,
// so the netboot tree says which of its files are extensions rather than
// leaving a caller to glob for *.raw and mistake an operator's own file for
// one. A directory with no declaration holds no bundled extension and this
// returns nothing.
//
// A declared image the directory does not hold is an error rather than a skip.
// Netbooting a node that silently lacks an extension the same build's ISO
// install gets is the failure this whole path exists to close.
func NetbootExtensionImages(netbootDir string) ([]string, error) {
	declaration := filepath.Join(netbootDir, isoExtensionsConfig)
	if _, err := os.Stat(declaration); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading %s: %w", declaration, err)
	}
	declared, err := declaredExtensionImages(declaration)
	if err != nil {
		return nil, err
	}
	images := make([]string, 0, len(declared))
	for _, name := range declared {
		image := filepath.Join(netbootDir, name)
		if _, err := os.Stat(image); err != nil {
			return nil, fmt.Errorf("%s declares %s but the netboot directory does not carry it: %w", isoExtensionsConfig, name, err)
		}
		images = append(images, image)
	}
	return images, nil
}

// isoRootEntries lists the names of the plain files in the ISO root.
func isoRootEntries(src string) ([]string, error) {
	img, err := diskfs.Open(src, diskfs.WithOpenMode(diskfs.ReadOnly))
	if err != nil {
		return nil, err
	}
	defer img.Close()
	fsys, err := img.GetFilesystem(0)
	if err != nil {
		return nil, err
	}
	// io/fs paths carry no leading slash, so the ISO root is ".".
	entries, err := fsys.ReadDir(".")
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		names = append(names, entry.Name())
	}
	return names, nil
}

// isoName normalizes an ISO directory entry for comparison. A plain ISO 9660
// name carries a ";1" version suffix and is upper case; a Rock Ridge one, which
// is what the Kairos ISOs are built with, does not. Comparing normalized names
// reads both.
func isoName(name string) string {
	if i := strings.LastIndex(name, ";"); i > 0 {
		name = name[:i]
	}
	return strings.ToLower(name)
}

// extractNetbootExtensions carries the bundled system extensions out of the
// ISO root and into the netboot directory.
//
// A resolved extension image lands at the ISO root rather than inside
// rootfs.squashfs: GenISO appends the materialized images to spec.Image, while
// spec.RootFS is what becomes the squashfs. The three paths above are all a
// netboot extraction used to read, so every bundled extension was dropped on
// the way to the netboot artifacts. Refs kairos-io/kairos#5040.
//
// Which root files are extensions is recorded in the extensions.yaml that
// materializeISOExtensions writes next to them, so that declaration is what is
// read here. An ISO carrying no such file carries no bundled extension and
// this is a no-op, and an --overlay-iso that happens to ship a .raw of its own
// is not mistaken for one.
//
// It returns the names it wrote into dst.
func extractNetbootExtensions(src, dst string) ([]string, error) {
	entries, err := isoRootEntries(src)
	if err != nil {
		return nil, fmt.Errorf("listing the root of %s: %w", src, err)
	}
	onISO := make(map[string]string, len(entries))
	for _, entry := range entries {
		onISO[isoName(entry)] = entry
	}
	declaration, ok := onISO[isoName(isoExtensionsConfig)]
	if !ok {
		return nil, nil
	}

	// The declaration lands in the netboot directory next to the images, not
	// in a temp dir, so the tree says which of its files are extensions. That
	// is what StartPixiecore reads back to fill the kairos.extensions keyword
	// (NetbootExtensionImages), and it is equally what an operator serving
	// this tree themselves needs in order to write install.extensions.
	local := filepath.Join(dst, isoExtensionsConfig)
	if err := iso.ExtractFileFromIso("/"+declaration, src, local, &internal.Log); err != nil {
		return nil, fmt.Errorf("extracting %s: %w", isoExtensionsConfig, err)
	}
	declared, err := declaredExtensionImages(local)
	if err != nil {
		return nil, err
	}

	carried := make([]string, 0, len(declared))
	for _, name := range declared {
		entry, ok := onISO[isoName(name)]
		if !ok {
			// AuroraBoot wrote both the declaration and the image, so a
			// declaration naming a file the root does not carry means the ISO
			// is inconsistent. Say so rather than shipping a netboot tree that
			// silently lacks an extension the ISO install gets.
			return carried, fmt.Errorf("%s declares %s but the ISO root does not carry it", isoExtensionsConfig, name)
		}
		if err := iso.ExtractFileFromIso("/"+entry, src, filepath.Join(dst, name), &internal.Log); err != nil {
			return carried, fmt.Errorf("extracting the bundled extension %s: %w", name, err)
		}
		carried = append(carried, name)
	}
	return carried, nil
}

// declaredExtensionImages reads the file names install.extensions points at on
// the live media.
//
// Only an entry sitting directly under the live media directory names a file
// on this ISO, because that is where materializeISOExtensions puts the images
// it resolves: at the root. Everything else is the installed system's to fetch
// rather than this build's to carry, whether it is an oci:// or https:// image
// or a nested live-media path from an extensions.yaml AuroraBoot did not write,
// so it is skipped rather than looked for at a root that does not hold it.
func declaredExtensionImages(path string) ([]string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", isoExtensionsConfig, err)
	}
	var config struct {
		Install struct {
			Extensions extensiontypes.Extensions `yaml:"extensions"`
		} `yaml:"install"`
	}
	if err := yaml.Unmarshal(content, &config); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", isoExtensionsConfig, err)
	}
	images := make([]string, 0, len(config.Install.Extensions))
	for _, extension := range config.Install.Extensions {
		if filepath.Dir(extension.Name) != agentconstants.LiveDir {
			continue
		}
		images = append(images, filepath.Base(extension.Name))
	}
	return images, nil
}
