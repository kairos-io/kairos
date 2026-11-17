package ops

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/kairos-io/AuroraBoot/internal"
	"github.com/kairos-io/AuroraBoot/pkg/constants"
	"github.com/kairos-io/AuroraBoot/pkg/extensions"
	"github.com/kairos-io/AuroraBoot/pkg/schema"
	"github.com/kairos-io/AuroraBoot/pkg/utils"
	"github.com/otiai10/copy"
	"github.com/twpayne/go-vfs/v5"

	"github.com/kairos-io/kairos/v4/agent/pkg/cloudinit"
	agentconstants "github.com/kairos-io/kairos/v4/agent/pkg/constants"
	"github.com/kairos-io/kairos/v4/agent/pkg/elemental"
	"github.com/kairos-io/kairos/v4/agent/pkg/implementations/http"
	"github.com/kairos-io/kairos/v4/agent/pkg/implementations/runner"
	"github.com/kairos-io/kairos/v4/agent/pkg/implementations/syscall"
	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
	extensiontypes "github.com/kairos-io/kairos/v4/sdk/types/extensions"
	imagetypes "github.com/kairos-io/kairos/v4/sdk/types/images"
	"github.com/kairos-io/kairos/v4/sdk/types/logger"
	"github.com/kairos-io/kairos/v4/sdk/types/platform"
	sdkutils "github.com/kairos-io/kairos/v4/sdk/utils"
	"github.com/sanity-io/litter"
	"gopkg.in/yaml.v3"
)

type LiveISO struct {
	RootFS             []*imagetypes.ImageSource `yaml:"rootfs,omitempty" mapstructure:"rootfs"`
	Image              []*imagetypes.ImageSource `yaml:"image,omitempty" mapstructure:"image"`
	Label              string                    `yaml:"label,omitempty" mapstructure:"label"`
	GrubEntry          string                    `yaml:"grub-entry-name,omitempty" mapstructure:"grub-entry-name"`
	BootloaderInRootFs bool                      `yaml:"bootloader-in-rootfs" mapstructure:"bootloader-in-rootfs"`
	// ExtendLiveCmdline is appended to the kernel cmdline when booting from the live/installer ISO.
	ExtendLiveCmdline string `yaml:"extend-live-cmdline,omitempty" mapstructure:"extend-live-cmdline"`
	LiveConsole       string `yaml:"live-console,omitempty" mapstructure:"live-console"`
	// DefaultGrubEntry is the id (`--id`) of the live menu entry grub boots when
	// the timeout expires. Left empty, the ISO boots the installer
	// (constants.LiveGrubEntryInstall). Any value outside
	// constants.LiveGrubEntries fails the build.
	DefaultGrubEntry string `yaml:"default-grub-entry,omitempty" mapstructure:"default-grub-entry"`
}

// BuildConfig represents the config we need for building isos, raw images, artifacts
type BuildConfig struct {
	Date   bool   `yaml:"date,omitempty" mapstructure:"date"`
	Name   string `yaml:"name,omitempty" mapstructure:"name"`
	OutDir string `yaml:"output,omitempty" mapstructure:"output"`

	// 'inline' and 'squash' labels ensure config fields
	// are embedded from a yaml and map PoV
	sdkConfig.Config `yaml:",inline" mapstructure:",squash"`
}

type BuildISOAction struct {
	cfg  *BuildConfig
	spec *LiveISO
	e    *elemental.Elemental
}

type BuildISOActionOption func(a *BuildISOAction)
type GenericOptions func(a *sdkConfig.Config) error

func NewBuildConfig(opts ...GenericOptions) (*BuildConfig, error) {
	c, err := NewConfig(opts...)
	if err != nil {
		return nil, err
	}

	return &BuildConfig{
		Config: *c,
		Name:   constants.BuildImgName,
	}, nil
}

func NewConfig(opts ...GenericOptions) (*sdkConfig.Config, error) {
	arch, err := utils.GolangArchToArch(runtime.GOARCH)
	if err != nil {
		return nil, fmt.Errorf("invalid host arch %s: %w", runtime.GOARCH, err)
	}

	c := &sdkConfig.Config{
		Fs:                    vfs.OSFS,
		Logger:                internal.Log,
		Syscall:               &syscall.RealSyscall{},
		Client:                http.NewClient(),
		Arch:                  arch,
		SquashFsNoCompression: true,
	}
	for _, o := range opts {
		err := o(c)
		if err != nil {
			return nil, fmt.Errorf("applying config option: %w", err)
		}
	}

	// delay runner creation after we have run over the options in case we use WithRunner
	if c.Runner == nil {
		c.Runner = &runner.RealRunner{Logger: &c.Logger}
	}

	// Now check if the runner has a logger inside, otherwise point our logger into it
	// This can happen if we set the WithRunner option as that doesn't set a logger
	if c.Runner.GetLogger() == nil {
		c.Runner.SetLogger(&c.Logger)
	}

	// Delay the yip runner creation, so we set the proper logger instead of blindly setting it to the logger we create
	// at the start of NewRunConfig, as WithLogger can be passed on init, and that would result in 2 different logger
	// instances, on the config.Logger and the other on config.CloudInitRunner
	if c.CloudInitRunner == nil {
		c.CloudInitRunner = cloudinit.NewYipCloudInitRunner(c.Logger, c.Runner, vfs.OSFS)
	}
	litter.Config.HidePrivateFields = false

	return c, nil
}

// isoBaseName is the base name the generated ISO is written under, without the
// ".iso" suffix and without the optional date stamp GenISO adds on top.
//
// override_name wins, because it is what --override-name sets and a command
// line is a deliberate, one-off override of the configuration. iso.name comes
// next: the schema calls it the final artifact base name, and it is the name
// the netboot artifacts already carry. With neither set the name is derived
// from the rootfs kairos-release file, which is the long-standing default.
func isoBaseName(i schema.ISO, rootfs string) string {
	switch {
	case i.OverrideName != "":
		return i.OverrideName
	case i.Name != "":
		return i.Name
	default:
		return fmt.Sprintf("kairos-%s", utils.NameFromRootfs(rootfs))
	}
}

// GenISO generates an ISO from a rootfs, and stores results in dst.
// The cloud config to embed is read from the path cloudConfigFunc returns.
func GenISO(srcFunc, dstFunc, cloudConfigFunc valueGetOnCall, i schema.ISO, targetArch string, insecure bool) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		dst := dstFunc()
		src := srcFunc()
		tmp, err := os.MkdirTemp("", "geniso")
		if err != nil {
			return err
		}
		defer os.RemoveAll(tmp)

		i.Name = isoBaseName(i, src)

		// We are assuming StepCopyCloudConfig has already run, writing the config to the path cloudConfigFunc returns.
		// Copy the cloud config into the temp dir so it ends up on the ISO root.
		err = copyFileIfExists(cloudConfigFunc(), filepath.Join(tmp, "config.yaml"))
		if err != nil {
			return err
		}

		internal.Log.Logger.Info().Msgf("Generating iso '%s' from '%s' to '%s'", i.Name, src, dst)
		cfg, err := NewBuildConfig(
			WithLogger(internal.Log),
		)
		if err != nil {
			return err
		}
		if arch, err := utils.GetArchFromRootfs(src, internal.Log); err == nil && arch != "" {
			cfg.Arch = arch
		} else if err != nil {
			internal.Log.Logger.Error().Err(err).Msg("failed to detect arch from rootfs; falling back to host arch")
		}
		cfg.Name = i.Name
		cfg.OutDir = dst
		cfg.Date = i.IncludeDate

		extensionArch := targetArch
		if extensionArch == "" {
			extensionArch = cfg.Arch
		}
		if err := materializeISOExtensions(ctx, i, extensionArch, tmp, insecure); err != nil {
			return err
		}

		spec := newLiveISOSpec(src, tmp, i)

		if i.OverlayRootfs != "" {
			spec.RootFS = append(spec.RootFS, imagetypes.NewDirSrc(i.OverlayRootfs))
		}
		if i.OverlayISO != "" {
			// Dereference symlinks / drop kubelet atomic-writer internals so a
			// Secret- or ConfigMap-mounted overlay can't shadow real ISO-root
			// dirs with symlinks (kairos-io/kairos#4324).
			overlay, err := utils.MaterializeOverlay(i.OverlayISO)
			if err != nil {
				return err
			}
			defer os.RemoveAll(overlay)
			spec.Image = append(spec.Image, imagetypes.NewDirSrc(overlay))
		}

		buildISO := NewBuildISOAction(cfg, spec)
		err = buildISO.ISORun()
		if err != nil {
			internal.Log.Logger.Error().Msgf("Failed generating iso '%s' from '%s'. Error: %s", i.Name, src, err.Error())
		}
		return err
	}
}

// newLiveISOSpec carries the ISO options the user gave over to the build spec.
// A field left out here is parsed from the config and then dropped, which the
// build has no way to report.
func newLiveISOSpec(rootfs, isoRoot string, i schema.ISO) *LiveISO {
	return &LiveISO{
		RootFS:             []*imagetypes.ImageSource{imagetypes.NewDirSrc(rootfs)},
		Image:              []*imagetypes.ImageSource{imagetypes.NewDirSrc(isoRoot)},
		Label:              constants.ISOLabel,
		GrubEntry:          constants.LiveGrubEntryInstall,
		BootloaderInRootFs: false,
		ExtendLiveCmdline:  i.ExtendLiveCmdline,
		DefaultGrubEntry:   i.DefaultGrubEntry,
		LiveConsole:        i.LiveConsole,
	}
}

var materializeExtensionArtifacts = extensions.Materialize

// isoExtensionsConfig is the cloud-config written next to the materialized
// extensions in the ISO root. The agent reads every config in the live media
// root, so it merges this one with the user's config.yaml, and a list of maps
// such as install.extensions is concatenated, not replaced.
const isoExtensionsConfig = "extensions.yaml"

func materializeISOExtensions(ctx context.Context, i schema.ISO, architecture, isoRoot string, insecure bool) error {
	if len(i.Extensions) == 0 {
		return nil
	}
	paths, err := materializeExtensionArtifacts(ctx, i.ExtensionsCatalogs, i.Extensions, architecture, isoRoot, insecure)
	if err != nil {
		return err
	}
	return declareISOExtensions(isoRoot, paths)
}

// declareISOExtensions lists the materialized images under install.extensions.
//
// Placing an image on the live media is not enough on a classic install: the
// installer stages only what install.extensions declares, and the image is
// gone with the live media after the first reboot. The UKI installer copies
// every *.sysext.raw of the live media on its own, but that is a different
// build path (pkg/uki), so this only runs for the classic ISO.
func declareISOExtensions(isoRoot string, paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	declared := make(extensiontypes.Extensions, 0, len(paths))
	for _, path := range paths {
		declared = append(declared, extensiontypes.Extension{
			// The ISO root is mounted here when the node boots from it.
			Name: filepath.Join(agentconstants.LiveDir, filepath.Base(path)),
		})
	}
	var config struct {
		Install struct {
			Extensions extensiontypes.Extensions `yaml:"extensions"`
		} `yaml:"install"`
	}
	config.Install.Extensions = declared
	body, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("render the extension declaration: %w", err)
	}
	content := append([]byte("#cloud-config\n"), body...)
	if err := os.WriteFile(filepath.Join(isoRoot, isoExtensionsConfig), content, 0o644); err != nil {
		return fmt.Errorf("write the extension declaration: %w", err)
	}
	return nil
}

func InjectISO(dstFunc, isoFunc valueGetOnCall, i schema.ISO) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		dst := dstFunc()
		isoFile := isoFunc() // call it just on time so we get the latest iso file path

		tmp, err := os.MkdirTemp("", "injectiso")
		if err != nil {
			return err
		}
		defer os.RemoveAll(tmp)

		if i.OverlayISO != "" {
			internal.Log.Logger.Info().Msgf("Adding overlay data in '%s' to '%s'", i.OverlayISO, isoFile)
			// Dereference symlinks / drop kubelet atomic-writer internals
			// (kairos-io/kairos#4324).
			err = copy.Copy(i.OverlayISO, tmp, utils.OverlayCopyOptions(i.OverlayISO))
			if err != nil {
				return err
			}
		}

		// Stage the cloud config only when it exists and is not empty: the
		// deployer always writes config.yaml, as a 0-byte file when no
		// cloud_config is set.
		hasConfig := false
		configFile := filepath.Join(dst, "config.yaml")
		if f, readErr := os.ReadFile(configFile); readErr == nil && len(f) > 0 {
			internal.Log.Logger.Info().Msgf("Adding cloud config file to '%s'", isoFile)
			if err := copy.Copy(configFile, filepath.Join(tmp, "config.yaml")); err != nil {
				return err
			}
			hasConfig = true
		} else {
			internal.Log.Logger.Warn().Msgf("No cloud config file found in '%s', skipping cloud config injection", configFile)
		}

		// An overlay must be injected even without a cloud config
		// (kairos-io/kairos#5778).
		if !hasConfig && i.OverlayISO == "" {
			return nil
		}

		// No os.Chdir here: every path below (isoFile, tmp, and the
		// config.yaml source) is already absolute, and xorriso is given
		// absolute -indev/-outdev/-map paths. Builds run as concurrent
		// goroutines in `web` mode, where a process-global chdir would
		// corrupt sibling builds, so we must not mutate the cwd.
		out, err := sdkutils.SH(fmt.Sprintf("xorriso -indev %s -outdev %s -map %s / -boot_image any replay", isoFile, isoFile, tmp))
		internal.Log.Print(out)
		if err != nil {
			return err
		}

		internal.Log.Logger.Info().Msgf("Wrote '%s' with injected data", isoFile)
		return nil
	}
}

func copyFileIfExists(src, dst string) error {
	if _, err := os.Stat(src); err != nil {
		if os.IsNotExist(err) {
			return nil
		} else {
			return err
		}
	}
	return copy.Copy(src, dst)
}

func NewBuildISOAction(cfg *BuildConfig, spec *LiveISO, opts ...BuildISOActionOption) *BuildISOAction {
	b := &BuildISOAction{
		cfg:  cfg,
		e:    elemental.NewElemental(&cfg.Config),
		spec: spec,
	}
	for _, opt := range opts {
		opt(b)
	}
	return b
}

// ISORun will install the system from a given configuration
func (b *BuildISOAction) ISORun() (err error) {
	cleanup := sdkutils.NewCleanStack()
	defer func() { err = cleanup.Cleanup(err) }()

	isoTmpDir, err := utils.TempDir(b.cfg.Fs, "", "auroraboot-iso")
	if err != nil {
		return err
	}
	cleanup.Push(func() error { return b.cfg.Fs.RemoveAll(isoTmpDir) })

	rootDir := filepath.Join(isoTmpDir, "rootfs")
	err = utils.MkdirAll(b.cfg.Fs, rootDir, constants.DirPerm)
	if err != nil {
		return err
	}

	isoDir := filepath.Join(isoTmpDir, "iso")
	err = utils.MkdirAll(b.cfg.Fs, isoDir, constants.DirPerm)
	if err != nil {
		return err
	}

	if b.cfg.OutDir != "" {
		err = utils.MkdirAll(b.cfg.Fs, b.cfg.OutDir, constants.DirPerm)
		if err != nil {
			b.cfg.Logger.Errorf("Failed creating output folder: %s", b.cfg.OutDir)
			return err
		}
	}

	b.cfg.Logger.Infof("Preparing squashfs root...")
	err = b.applySources(rootDir, b.spec.RootFS...)
	if err != nil {
		b.cfg.Logger.Errorf("Failed installing OS packages: %v", err)
		return err
	}
	err = utils.CreateDirStructure(b.cfg.Fs, rootDir)
	if err != nil {
		b.cfg.Logger.Errorf("Failed creating root directory structure: %v", err)
		return err
	}

	b.cfg.Logger.Infof("Preparing ISO image root tree...")
	err = b.applySources(isoDir, b.spec.Image...)
	if err != nil {
		b.cfg.Logger.Errorf("Failed installing ISO image packages: %v", err)
		return err
	}

	err = b.prepareISORoot(isoDir, rootDir)
	if err != nil {
		b.cfg.Logger.Errorf("Failed preparing ISO's root tree: %v", err)
		return err
	}

	err = b.prepareBootArtifacts(isoDir)
	if err != nil {
		b.cfg.Logger.Errorf("Failed preparing boot artifacts: %v", err)
		return err
	}

	b.cfg.Logger.Infof("Creating ISO image...")
	err = b.burnISO(isoDir)
	if err != nil {
		b.cfg.Logger.Errorf("Failed creating ISO image: %v", err)
		return err
	}

	return err
}

// resolveDefaultGrubEntry checks the entry id a build named against the ids
// the template defines, and returns the installer entry when the build named
// none.
//
// Grub resolves `set default` against the ids and silently falls back to the
// first entry when it matches nothing, so a typo is not a boot failure a user
// can diagnose, it is a boot that lands somewhere else. The id also goes
// inside a quoted grub assignment, where a value ending in a backslash
// escapes the closing quote and swallows the lines that follow. Both cases
// are the same mistake, and the build can only report it before it writes
// the config.
func resolveDefaultGrubEntry(defaultEntry string) (string, error) {
	defaultEntry = strings.TrimSpace(defaultEntry)
	if defaultEntry == "" {
		return constants.LiveGrubEntryInstall, nil
	}
	if slices.Contains(constants.LiveGrubEntries, defaultEntry) {
		return defaultEntry, nil
	}
	return "", fmt.Errorf(
		"unknown default live grub entry %q: expected one of %s, or an empty value for %s",
		defaultEntry, strings.Join(constants.LiveGrubEntries, ", "), constants.LiveGrubEntryInstall)
}

// applyGrubTemplate replaces placeholders in the grub config template.
func applyGrubTemplate(cfg []byte, nomodeset, extendCmdline, liveConsole, defaultEntry string) ([]byte, error) {
	liveConsole = strings.NewReplacer("\n", "", "\r", "").Replace(strings.TrimSpace(liveConsole))
	if liveConsole == "" {
		liveConsole = "console=ttyS0 console=tty1"
	}
	defaultEntry, err := resolveDefaultGrubEntry(defaultEntry)
	if err != nil {
		return nil, err
	}
	out := strings.ReplaceAll(string(cfg), "{{NOMODESET}}", nomodeset)
	out = strings.ReplaceAll(out, "{{EXTEND_CMDLINE}}", extendCmdline)
	out = strings.ReplaceAll(out, "{{LIVE_CONSOLE}}", liveConsole)
	out = strings.ReplaceAll(out, "{{DEFAULT_ENTRY}}", defaultEntry)
	return []byte(out), nil
}

// prepareBootArtifacts will write the needed artifacts for BIOS cd boot into the isoDir
// so xorriso can use those to build the bootable iso file
func (b *BuildISOAction) prepareBootArtifacts(isoDir string) error {
	err := os.WriteFile(filepath.Join(isoDir, constants.IsoBootFile), constants.Eltorito, constants.FilePerm)
	if err != nil {
		return err
	}
	err = os.WriteFile(filepath.Join(isoDir, constants.IsoHybridMBR), constants.BootHybrid, constants.FilePerm)
	if err != nil {
		return err
	}
	err = os.MkdirAll(filepath.Join(isoDir, constants.GrubPrefixDir), constants.DirPerm)
	if err != nil {
		return err
	}
	if _, exist := os.Stat(filepath.Join(isoDir, constants.GrubPrefixDir, constants.GrubCfg)); os.IsNotExist(exist) {
		nomodeset := ""
		if b.cfg.Arch == constants.ArchAmd64 || b.cfg.Arch == constants.Archx86 {
			nomodeset = " nomodeset"
		}
		extendCmdline := ""
		if b.spec != nil {
			extendCmdline = b.spec.ExtendLiveCmdline
		}
		if extendCmdline != "" {
			// Strip any newlines or carriage returns to prevent corruption of the grub config
			extendCmdline = strings.NewReplacer("\n", "", "\r", "").Replace(strings.TrimSpace(extendCmdline))
			extendCmdline = " " + extendCmdline
		}
		liveConsole := ""
		if b.spec != nil {
			liveConsole = b.spec.LiveConsole
		}
		defaultEntry := ""
		if b.spec != nil {
			defaultEntry = b.spec.DefaultGrubEntry
		}
		grubCfg, err := applyGrubTemplate(constants.GrubLiveBiosCfg, nomodeset, extendCmdline, liveConsole, defaultEntry)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(isoDir, constants.GrubPrefixDir, constants.GrubCfg), grubCfg, constants.FilePerm)
	} else {
		b.cfg.Logger.Logger.Warn().Msgf("Grub config already exists at %s, skipping using default one", filepath.Join(isoDir, constants.GrubPrefixDir, constants.GrubCfg))
	}
	return nil
}

func (b BuildISOAction) prepareISORoot(isoDir string, rootDir string) error {
	kernel, initrd, err := b.e.FindKernelInitrd(rootDir)
	if err != nil {
		b.cfg.Logger.Error("Could not find kernel and/or initrd")
		return err
	}
	err = utils.MkdirAll(b.cfg.Fs, filepath.Join(isoDir, "boot"), constants.DirPerm)
	if err != nil {
		return err
	}
	//TODO document boot/kernel and boot/initrd expectation in bootloader config
	b.cfg.Logger.Debugf("Copying Kernel file %s to iso root tree", kernel)
	err = utils.CopyFile(b.cfg.Fs, kernel, filepath.Join(isoDir, constants.IsoKernelPath))
	if err != nil {
		return err
	}

	b.cfg.Logger.Debugf("Copying initrd file %s to iso root tree", initrd)
	err = utils.CopyFile(b.cfg.Fs, initrd, filepath.Join(isoDir, constants.IsoInitrdPath))
	if err != nil {
		return err
	}

	b.cfg.Logger.Info("Creating EFI image...")
	err = b.createEFI(rootDir, isoDir)
	if err != nil {
		return err
	}

	b.cfg.Logger.Info("Creating squashfs...")
	err = utils.CreateSquashFS(b.cfg.Runner, b.cfg.Logger, rootDir, filepath.Join(isoDir, constants.IsoRootFile), constants.GetDefaultSquashfsOptions())
	if err != nil {
		return err
	}

	return nil
}

// createEFI creates the EFI image that is used for booting
// it searches the rootfs for the shim/grub.efi file and copies it into a directory with the proper EFI structure
// then it generates a grub.cfg that chainloads into the grub.cfg of the livecd (which is the normal livecd grub config from luet packages)
// then it calculates the size of the EFI image based on the files copied and creates the image
func (b BuildISOAction) createEFI(rootdir string, isoDir string) error {
	var err error

	// rootfs /efi dir
	img := filepath.Join(isoDir, constants.IsoEFIPath)
	// Temp dir is where we will build the EFI image from
	temp, _ := utils.TempDir(b.cfg.Fs, "", "auroraboot-iso")
	err = utils.MkdirAll(b.cfg.Fs, filepath.Join(temp, constants.EfiBootPath), constants.DirPerm)
	if err != nil {
		b.cfg.Logger.Errorf("Failed creating temp efi dir: %v", err)
		return err
	}
	err = utils.MkdirAll(b.cfg.Fs, filepath.Join(isoDir, constants.EfiBootPath), constants.DirPerm)
	if err != nil {
		b.cfg.Logger.Errorf("Failed creating iso efi dir: %v", err)
		return err
	}

	err = b.copyShim(temp, rootdir)
	if err != nil {
		return err
	}

	err = b.copyGrub(temp, rootdir)
	if err != nil {
		return err
	}

	// Generate grub cfg that chainloads into the default livecd grub under /boot/grub2/grub.cfg
	// Its read from the root of the livecd, so we need to copy it into /EFI/BOOT/grub.cfg
	// This is due to the hybrid bios/efi boot mode of the livecd
	// the uefi.img is loaded into memory and run, but grub only sees the livecd root
	// WARNING: If we load the image into an usb stick, grub will not use the livecd root as the grub root, so it cannot find the grub.cfg
	// So we copy in 2 places, into the livecd and into the img

	// This is used when booting from usb
	if err = b.writeDefaultGrubEfiCfg(temp); err != nil {
		b.cfg.Logger.Errorf("Failed writing grub.cfg: %v", err)
		return err
	}
	// This is used when booting from cdrom
	if err = b.writeDefaultGrubEfiCfg(isoDir); err != nil {
		b.cfg.Logger.Errorf("Failed writing grub.cfg: %v", err)
		return err
	}
	// The CD grub (gcd*.efi.signed) has its prefix baked to /boot/grub, so
	// also drop a grub.cfg there. Placed both in the EFI image (USB case,
	// where $root is the FAT ESP) and in the ISO root (CD case, where $root
	// is the ISO9660 device).
	if err = b.writeCdGrubEfiCfg(temp); err != nil {
		b.cfg.Logger.Errorf("Failed writing CD grub.cfg: %v", err)
		return err
	}
	if err = b.writeCdGrubEfiCfg(isoDir); err != nil {
		b.cfg.Logger.Errorf("Failed writing CD grub.cfg: %v", err)
		return err
	}

	// For RISC-V, create startup.nsh to auto-boot since some UEFI implementations
	// don't automatically load the fallback boot path (BOOTRISCV64.EFI)
	arch, err := utils.GetArchFromRootfs(rootdir, b.cfg.Logger)
	if err != nil {
		return err
	}
	if utils.IsRiscv64(arch) {
		startupScript := "\\EFI\\BOOT\\bootriscv64.efi\n"
		if err = b.cfg.Fs.WriteFile(filepath.Join(temp, "startup.nsh"), []byte(startupScript), constants.FilePerm); err != nil {
			b.cfg.Logger.Errorf("Failed writing startup.nsh: %v", err)
			return err
		}
		if err = b.cfg.Fs.WriteFile(filepath.Join(isoDir, "startup.nsh"), []byte(startupScript), constants.FilePerm); err != nil {
			b.cfg.Logger.Errorf("Failed writing startup.nsh: %v", err)
			return err
		}
		b.cfg.Logger.Debugf("Created startup.nsh for RISC-V auto-boot")
	}
	// Ubuntu efi searches for the grub.cfg file under /EFI/ubuntu/grub.cfg while we store it under /boot/grub2/grub.cfg
	// workaround this by copying it there as well
	// read the kairos-release from the rootfs to know if we are creating a ubuntu based iso
	var flavor string
	flavor, err = sdkutils.OSRelease("FLAVOR", filepath.Join(rootdir, "etc/kairos-release"))
	if err != nil {
		// fallback to os-release
		flavor, err = sdkutils.OSRelease("FLAVOR", filepath.Join(rootdir, "etc/os-release"))
		if err != nil {
			b.cfg.Logger.Warnf("Failed reading os-release from %s and %s: %v", filepath.Join(rootdir, "etc/kairos-release"), filepath.Join(rootdir, "etc/os-release"), err)
			return err
		}
	}
	b.cfg.Logger.Infof("Detected Flavor: %s", flavor)
	if strings.Contains(strings.ToLower(flavor), "ubuntu") {
		b.cfg.Logger.Infof("Ubuntu based ISO detected, copying grub.cfg to /EFI/ubuntu/grub.cfg")

		if err = b.writeUbuntuGrubEfiCfg(temp); err != nil {
			b.cfg.Logger.Errorf("Failed writing grub.cfg: %v", err)
			return err
		}
		if err = b.writeUbuntuGrubEfiCfg(isoDir); err != nil {
			b.cfg.Logger.Errorf("Failed writing grub.cfg: %v", err)
			return err
		}
	}

	// Calculate EFI image size based on artifacts
	efiSize, err := utils.DirSize(b.cfg.Fs, temp)
	if err != nil {
		return err
	}
	// align efiSize to the next 4MB slot
	align := int64(4 * 1024 * 1024)
	efiSizeMB := (efiSize/align*align + align) / (1024 * 1024)
	// Create the actual efi image
	err = b.e.CreateFileSystemImage(&imagetypes.Image{
		File:  img,
		Size:  uint(efiSizeMB),
		FS:    constants.EfiFs,
		Label: constants.EfiLabel,
	})
	if err != nil {
		return err
	}
	b.cfg.Logger.Debugf("EFI image created at %s", img)
	// copy the files from the temporal efi dir into the EFI image
	files, err := b.cfg.Fs.ReadDir(temp)
	if err != nil {
		return err
	}

	for _, f := range files {
		// This copies the efi files into the efi img used for the boot
		b.cfg.Logger.Debugf("Copying %s to %s", filepath.Join(temp, f.Name()), img)
		_, err = b.cfg.Runner.Run("mcopy", "-s", "-i", img, filepath.Join(temp, f.Name()), "::")
		if err != nil {
			b.cfg.Logger.Errorf("Failed copying %s to %s: %v", filepath.Join(temp, f.Name()), img, err)
			return err
		}
	}

	return nil
}

// writeDefaultGrubEfiCfg writes the default grub.cfg for the EFI image in teh given path
func (b *BuildISOAction) writeDefaultGrubEfiCfg(path string) error {
	calculatedPath := filepath.Join(path, constants.EfiBootPath, constants.GrubCfg)
	return b.cfg.Fs.WriteFile(calculatedPath, []byte(constants.GrubEfiCfg), constants.FilePerm)
}

// writeCdGrubEfiCfg writes grub.cfg at /boot/grub/grub.cfg to match the prefix
// baked into gcdx64.efi.signed / gcdaa64.efi.signed. Without this, the CD grub
// (which we prefer for ISO builds) drops to the rescue prompt on firmwares
// where $root resolves to the ISO9660 root at boot.
func (b *BuildISOAction) writeCdGrubEfiCfg(path string) error {
	calculatedPath := filepath.Join(path, constants.CdGrubPrefixDir, constants.GrubCfg)
	if err := utils.MkdirAll(b.cfg.Fs, filepath.Dir(calculatedPath), constants.DirPerm); err != nil {
		b.cfg.Logger.Errorf("Failed creating base dir for CD grub.cfg: %v", err)
		return err
	}
	return b.cfg.Fs.WriteFile(calculatedPath, []byte(constants.GrubEfiCfg), constants.FilePerm)
}

func (b *BuildISOAction) writeUbuntuGrubEfiCfg(path string) error {
	calculatedPath := filepath.Join(path, "EFI/ubuntu/", constants.GrubCfg)
	err := utils.MkdirAll(b.cfg.Fs, filepath.Dir(calculatedPath), constants.DirPerm)
	if err != nil {
		b.cfg.Logger.Errorf("Failed creating base dir for grub.cfg: %v", err)
		return err
	}
	return b.cfg.Fs.WriteFile(calculatedPath, []byte(constants.GrubEfiCfg), constants.FilePerm)
}

// copyShim copies the shim files into the EFI partition
// tempdir is the temp dir where the EFI image is generated from
// rootdir is the rootfs where the shim files are searched for
func (b BuildISOAction) copyShim(tempdir, rootdir string) error {
	var fallBackShim string
	var err error
	var arch string
	// Get possible shim file paths
	arch, err = utils.GetArchFromRootfs(rootdir, b.cfg.Logger)
	if err != nil {
		return err
	}

	// RISC-V doesn't use shim - it boots grub directly via EFI
	if utils.IsRiscv64(arch) {
		b.cfg.Logger.Debugf("Skipping shim copy for riscv64 - will use grub directly")
		return nil
	}

	shimFiles := sdkutils.GetEfiShimFiles(arch)
	// Calculate shim path based on arch
	var shimDest string
	switch arch {
	case constants.ArchAmd64, constants.Archx86:
		shimDest = filepath.Join(tempdir, constants.ShimEfiDest)
		fallBackShim = filepath.Join("/amd/raw/grubartifacts", constants.EfiBootPath, "grub.efi")
	case constants.ArchArm64:
		shimDest = filepath.Join(tempdir, constants.ShimEfiArmDest)
		fallBackShim = filepath.Join("/arm/raw/grubartifacts", constants.EfiBootPath, "grub.efi")
	default:
		return fmt.Errorf("not supported architecture: %v", arch)
	}
	var shimDone bool
	for _, f := range shimFiles {
		_, err := b.cfg.Fs.Stat(filepath.Join(rootdir, f))
		if err != nil {
			b.cfg.Logger.Debugf("skip copying %s: not found", filepath.Join(rootdir, f))
			continue
		}
		b.cfg.Logger.Debugf("Copying %s to %s", filepath.Join(rootdir, f), shimDest)
		err = utils.CopyFile(
			b.cfg.Fs,
			filepath.Join(rootdir, f),
			shimDest,
		)
		if err != nil {
			b.cfg.Logger.Warnf("error reading %s: %s", filepath.Join(rootdir, f), err)
			continue
		}
		shimDone = true
		break
	}
	if !shimDone {
		// All failed...maybe we are on alpine which doesnt provide shim/grub.efi ?
		// In that case, we can just use the luet packaged artifacts
		err = utils.CopyFile(
			b.cfg.Fs,
			fallBackShim,
			shimDest,
		)
		if err != nil {
			b.cfg.Logger.Debugf("List of shim files searched for in %s: %s", rootdir, shimFiles)
			return fmt.Errorf("could not find any shim file to copy")
		}
		b.cfg.Logger.Debugf("Using fallback shim file %s", fallBackShim)
		// Also copy the shim.efi file into the rootfs so the installer can find it. Side effect of
		// alpine not providing shim/grub.efi and we not providing it from packages anymore
		_ = utils.MkdirAll(b.cfg.Fs, filepath.Join(rootdir, filepath.Dir(shimFiles[0])), constants.DirPerm)
		err = utils.CopyFile(
			b.cfg.Fs,
			fallBackShim,
			filepath.Join(rootdir, shimFiles[0]),
		)
		if err != nil {
			b.cfg.Logger.Debugf("Could not copy fallback shim into rootfs from %s to %s", fallBackShim, filepath.Join(rootdir, shimFiles[0]))
			return fmt.Errorf("could not copy fallback shim into rootfs from %s to %s", fallBackShim, filepath.Join(rootdir, shimFiles[0]))
		}
	}
	return err
}

// getEfiGrubFilesForArch returns the possible grub EFI file paths for the given
// architecture, ordered by preference for building a live ISO.
//
// The SDK owns the distro-specific live-media ordering. AuroraBoot only adds
// the openSUSE RISC-V path that is specific to its ISO builder.
func getEfiGrubFilesForArch(arch string) []string {
	sdkPaths := sdkutils.GetEfiLiveGrubFiles(arch)
	if utils.IsRiscv64(arch) {
		return append([]string{"/usr/share/efi/riscv64/grub.efi"}, sdkPaths...)
	}
	return sdkPaths
}

// copyGrub copies the shim files into the EFI partition
// tempdir is the temp dir where the EFI image is generated from
// rootdir is the rootfs where the shim files are searched for
func (b BuildISOAction) copyGrub(tempdir, rootdir string) error {
	var fallBackGrub string
	var err error

	arch, err := utils.GetArchFromRootfs(rootdir, b.cfg.Logger)
	if err != nil {
		return err
	}

	switch arch {
	case constants.ArchAmd64, constants.Archx86:
		fallBackGrub = filepath.Join("/amd/raw/grubartifacts", constants.EfiBootPath, "grub.efi")
	case constants.ArchArm64:
		fallBackGrub = filepath.Join("/arm/raw/grubartifacts", constants.EfiBootPath, "grub.efi")
	case constants.ArchRiscv64:
		fallBackGrub = filepath.Join("/riscv64/raw/grubartifacts", constants.EfiBootPath, "grub.efi")
	default:
		return fmt.Errorf("not supported architecture: %v", arch)
	}

	// Get possible grub file paths
	grubFiles := getEfiGrubFilesForArch(arch)
	var grubDone bool
	for _, f := range grubFiles {
		stat, err := b.cfg.Fs.Stat(filepath.Join(rootdir, f))
		if err != nil {
			b.cfg.Logger.Debugf("skip copying %s: not found", filepath.Join(rootdir, f))
			continue
		}
		// For riscv64, copy grub to the EFI fallback location since there's no shim
		var nameDest string
		if utils.IsRiscv64(arch) {
			nameDest = filepath.Join(tempdir, constants.ShimEfiRiscv64Dest)
		} else {
			// Same name as the source, shim looks for that name. We need to remove the .signed suffix
			nameDest = filepath.Join(tempdir, "EFI/BOOT", cleanupGrubName(stat.Name()))
		}
		b.cfg.Logger.Debugf("Copying %s to %s", filepath.Join(rootdir, f), nameDest)

		err = utils.CopyFile(
			b.cfg.Fs,
			filepath.Join(rootdir, f),
			nameDest,
		)
		if err != nil {
			b.cfg.Logger.Warnf("error reading %s: %s", filepath.Join(rootdir, f), err)
			continue
		}
		grubDone = true
		break
	}
	if !grubDone {
		if fallBackGrub == "" {
			b.cfg.Logger.Debugf("List of grub files searched for: %v", grubFiles)
			return fmt.Errorf("could not find any grub efi file to copy for %s", arch)
		}
		// All failed...maybe we are on alpine which doesnt provide shim/grub.efi ?
		// In that case, we can just use the luet packaged artifacts
		err = utils.CopyFile(
			b.cfg.Fs,
			fallBackGrub,
			filepath.Join(tempdir, "EFI/BOOT/grub.efi"),
		)
		if err != nil {
			b.cfg.Logger.Debugf("List of grub files searched for: %v", grubFiles)
			return fmt.Errorf("could not find any grub efi file to copy")
		}
		b.cfg.Logger.Debugf("Using fallback grub file %s", fallBackGrub)
		// Also copy the grub.efi file into the rootfs so the installer can find it. Side effect of
		// alpine not providing shim/grub.efi and we not providing it from packages anymore
		utils.MkdirAll(b.cfg.Fs, filepath.Join(rootdir, filepath.Dir(grubFiles[0])), constants.DirPerm)
		err = utils.CopyFile(
			b.cfg.Fs,
			fallBackGrub,
			filepath.Join(rootdir, grubFiles[0]),
		)
		if err != nil {
			b.cfg.Logger.Debugf("Could not copy fallback grub into rootfs from %s to %s", fallBackGrub, filepath.Join(rootdir, grubFiles[0]))
			return fmt.Errorf("could not copy fallback shim into rootfs from %s to %s", fallBackGrub, filepath.Join(rootdir, grubFiles[0]))
		}
	}
	return err
}

func (b BuildISOAction) burnISO(root string) error {
	cmd := "xorriso"
	var outputFile string
	var isoFileName string

	if b.cfg.Date {
		currTime := time.Now()
		isoFileName = fmt.Sprintf("%s.%s.iso", b.cfg.Name, currTime.Format("20060102"))
	} else {
		isoFileName = fmt.Sprintf("%s.iso", b.cfg.Name)
	}

	internal.Log.Logger.Debug().Str("name", isoFileName).Msg("Got output name")

	outputFile = isoFileName
	if b.cfg.OutDir != "" {
		outputFile = filepath.Join(b.cfg.OutDir, outputFile)
	}

	if exists, _ := utils.Exists(b.cfg.Fs, outputFile); exists {
		b.cfg.Logger.Warnf("Overwriting already existing %s", outputFile)
		err := b.cfg.Fs.Remove(outputFile)
		if err != nil {
			return err
		}
	}

	args := []string{
		"-volid", b.spec.Label, "-joliet", "on", "-padding", "0",
		"-outdev", outputFile, "-map", root, "/", "-chmod", "0755", "--",
	}
	args = append(args, constants.GetXorrisoBooloaderArgs(root)...)

	out, err := b.cfg.Runner.Run(cmd, args...)
	b.cfg.Logger.Debugf("Xorriso: %s", string(out))
	if err != nil {
		return err
	}

	checksum, err := utils.CalcFileChecksum(b.cfg.Fs, outputFile)
	if err != nil {
		return fmt.Errorf("checksum computation failed: %w", err)
	}
	err = b.cfg.Fs.WriteFile(fmt.Sprintf("%s.sha256", outputFile), []byte(fmt.Sprintf("%s %s\n", checksum, isoFileName)), 0644)
	if err != nil {
		return fmt.Errorf("cannot write checksum file: %w", err)
	}

	return nil
}

func (b BuildISOAction) applySources(target string, sources ...*imagetypes.ImageSource) error {
	for _, src := range sources {
		_, err := b.e.DumpSource(target, src)
		if err != nil {
			return err
		}
	}
	return nil
}

// cleanupGrubName normalises a grub EFI filename so shim can chainload it.
//
// Two things happen here:
//  1. Signing suffixes (.signed, .dualsigned, .signed.latest) are stripped,
//     because shim looks up its grub payload by the unsuffixed name.
//  2. The CD-media grub (gcdx64.efi / gcdaa64.efi) is renamed to the
//     corresponding grub*.efi name. Ubuntu's shim is compiled to load
//     grub{x64,aa64}.efi from the same directory, so we cannot leave the
//     "gcd" name on disk or Secure Boot chainloading fails.
func cleanupGrubName(name string) string {
	clean := strings.TrimSuffix(name, ".signed.latest")
	clean = strings.TrimSuffix(clean, ".dualsigned")
	clean = strings.TrimSuffix(clean, ".signed")
	switch clean {
	case "gcdx64.efi":
		return "grubx64.efi"
	case "gcdaa64.efi":
		return "grubaa64.efi"
	}
	return clean
}

func WithLogger(logger logger.KairosLogger) func(r *sdkConfig.Config) error {
	return func(r *sdkConfig.Config) error {
		r.Logger = logger
		return nil
	}
}

func WithImageExtractor(extractor imagetypes.ImageExtractor) func(r *sdkConfig.Config) error {
	return func(r *sdkConfig.Config) error {
		r.ImageExtractor = extractor
		return nil
	}
}

func WithArch(arch string) func(r *sdkConfig.Config) error {
	return func(r *sdkConfig.Config) error {
		if arch != "" {
			convertedArch, err := utils.GolangArchToArch(arch)
			if err != nil {
				return fmt.Errorf("invalid architecture %s: %w", arch, err)
			}
			r.Arch = convertedArch
			// Also set Platform since DumpSource uses Platform.String() not Arch
			// The SDK's platform package doesn't support riscv64, so we handle it manually
			if utils.IsRiscv64(arch) {
				r.Platform = &platform.Platform{
					OS:         "linux",
					Arch:       arch,
					GolangArch: arch,
				}
			} else {
				p, err := platform.NewPlatformFromArch(arch)
				if err != nil {
					return fmt.Errorf("invalid architecture for platform %s: %w", arch, err)
				}
				r.Platform = p
			}
		}
		return nil
	}
}
