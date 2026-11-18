// Package uki exposes the `auroraboot build-uki` pipeline as a reusable Go
// API. It mirrors the behavior of the CLI command so external tools (for
// example, auroraboot) can drive UKI builds without shelling out to the
// auroraboot binary.
//
// ISO output still relies on a handful of host binaries being available on
// PATH: dd, mformat, mmd, mcopy and xorriso. The uki and container outputs
// need none of them. Build will return an error up-front if any binary the
// requested output needs is missing.
package uki

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/kairos-io/AuroraBoot/pkg/constants"
	"github.com/kairos-io/AuroraBoot/pkg/extensions"
	"github.com/kairos-io/AuroraBoot/pkg/ops"
	"github.com/kairos-io/AuroraBoot/pkg/utils"
	goukiuki "github.com/kairos-io/go-ukify/pkg/uki"
	"github.com/kairos-io/kairos/v4/agent/pkg/elemental"
	sdkImages "github.com/kairos-io/kairos/v4/sdk/types/images"
	installtypes "github.com/kairos-io/kairos/v4/sdk/types/install"
	"github.com/kairos-io/kairos/v4/sdk/types/logger"
	imageutils "github.com/kairos-io/kairos/v4/sdk/utils/image"
	"github.com/klauspost/compress/zstd"
	"github.com/u-root/u-root/pkg/cpio"
	"golang.org/x/exp/maps"
	"gopkg.in/yaml.v3"
)

// Options configures a UKI build.
type Options struct {
	// Source is the container image source URI (e.g. "docker:kairos/core:vX",
	// "oci:path", "dir:path"). Required.
	Source string

	// OutputDir is the directory where the final artifact(s) will be written.
	// Created by the caller; defaults to "." when empty.
	OutputDir string

	// OutputType is one of constants.DefaultOutput ("uki"), constants.IsoOutput
	// ("iso") or constants.ContainerOutput ("container"). Defaults to
	// constants.DefaultOutput.
	OutputType string

	// Name is the basename of the generated artifact (ignored for the default
	// "uki" output type which produces a directory of files).
	Name string

	// Arch overrides the target architecture. Defaults to the runtime arch
	// resolved by ops.NewConfig.
	Arch string

	// AllowInsecureRegistries allows pulling the source image from a registry
	// served over plain HTTP or presenting an untrusted/self-signed TLS
	// certificate.
	AllowInsecureRegistries bool

	// Extensions are named catalog extensions to place in the ISO root.
	Extensions []extensions.Request

	// ExtensionsCatalogs are the paths or URLs of the extension catalogs,
	// searched in order: the first one publishing a name wins.
	ExtensionsCatalogs []string

	// OverlayRootfs is an optional directory whose contents are copied into
	// the rootfs before the UKI is built.
	OverlayRootfs string

	// OverlayISO is an optional directory whose contents are copied into the
	// ISO rootfs. Only honored for IsoOutput.
	OverlayISO string

	// BootBranding is the title string used for the systemd-boot entries.
	// Defaults to "Kairos" when empty.
	BootBranding string

	// IncludeVersionInConfig adds the OS version to each .conf file.
	IncludeVersionInConfig bool

	// IncludeCmdlineInConfig adds the extra cmdline to each .conf file.
	IncludeCmdlineInConfig bool

	// ExtraCmdlines creates one extra EFI file per cmdline (default + extra).
	// Mutually exclusive with ExtendCmdline.
	ExtraCmdlines []string

	// ExtendCmdline extends the default cmdline used for the single default
	// artifact. Mutually exclusive with ExtraCmdlines.
	ExtendCmdline string

	// SingleEfiCmdlines adds one extra EFI file per entry with a user-chosen
	// title. Syntax: "Title: extra cmdline".
	SingleEfiCmdlines []string

	// PublicKeysDir is the directory containing db.auth, KEK.auth, PK.auth
	// used for Secure Boot auto-enroll. Optional; a warning is logged when
	// empty.
	PublicKeysDir string

	// TPMPCRPrivateKey is the private key used to sign the EFI PCR policy.
	// Required. Can be a PKCS11 URI or a path to a PEM file.
	TPMPCRPrivateKey string

	// SBKey is the private key used to sign the EFI files for Secure Boot.
	// Required. Can be a PKCS11 URI or a path to a PEM file.
	SBKey string

	// SBCert is the certificate paired with SBKey. Required; must be a file
	// path.
	SBCert string

	// SecureBootEnroll is the value for systemd-boot's secure-boot-enroll
	// option. Defaults to "if-safe" when empty. See
	// https://manpages.debian.org/experimental/systemd-boot/loader.conf.5.en.html
	SecureBootEnroll string

	// Splash is an optional path to a BMP splash image embedded in the UKI.
	Splash string

	// CmdLinesV2 enables the systemd-boot 257 multi-profile cmdline format.
	CmdLinesV2 bool

	// SdBootInSource looks for systemd-boot files inside the source rootfs
	// rather than using the bundled ones.
	SdBootInSource bool

	// CloudConfig holds the raw cloud-config YAML.
	CloudConfig string

	// Logger is the kairos-sdk logger used for progress messages. If nil, a
	// default info-level logger is used.
	Logger *logger.KairosLogger
}

// absolutizePaths rewrites every path option to an absolute path. These are
// handed to go-ukify and to host subprocesses (mcopy, xorriso, ...) that run
// with an unspecified working directory, so relative paths (e.g. the web
// server passes paths relative to its working dir like "data/keys/...") must
// be resolved against the caller's working directory up-front to resolve
// reliably. pkcs11 URIs (valid for SBKey and TPMPCRPrivateKey) and empty values are left untouched.
func absolutizePaths(opts *Options) error {
	absIfNeeded := func(p *string) error {
		if *p == "" || filepath.IsAbs(*p) {
			return nil
		}
		abs, err := filepath.Abs(*p)
		if err != nil {
			return fmt.Errorf("resolving path %q: %w", *p, err)
		}
		*p = abs
		return nil
	}

	// Only these options can be PKCS#11 URIs.
	if opts.TPMPCRPrivateKey != "" && !strings.HasPrefix(opts.TPMPCRPrivateKey, "pkcs11:") {
		if err := absIfNeeded(&opts.TPMPCRPrivateKey); err != nil {
			return err
		}
	}
	if opts.SBKey != "" && !strings.HasPrefix(opts.SBKey, "pkcs11:") {
		if err := absIfNeeded(&opts.SBKey); err != nil {
			return err
		}
	}

	for _, p := range []*string{&opts.SBCert, &opts.PublicKeysDir, &opts.Splash, &opts.OverlayRootfs, &opts.OverlayISO, &opts.OutputDir} {
		if err := absIfNeeded(p); err != nil {
			return err
		}
	}
	return nil
}

// Build runs the full UKI build pipeline as described by opts.
//
// It validates required fields, checks that the host binaries and bundled
// systemd-boot files are available, extracts the source image, generates the
// EFI files via go-ukify and finally assembles the requested output type.
//
// Build does not mutate the process working directory: every artifact is
// written under a per-build temporary sourceDir using absolute paths, so
// concurrent Build calls (e.g. in `web` mode) are safe to run in parallel.
func Build(opts Options) (err error) {
	if err := opts.validate(); err != nil {
		return err
	}

	// Resolve all path options to absolute up-front so they resolve against the
	// caller's working directory rather than wherever subprocesses happen to
	// run (e.g. the web server passes paths relative to its working dir like
	// "data/keys/...").
	if err := absolutizePaths(&opts); err != nil {
		return err
	}

	log := opts.Logger
	if log == nil {
		l := logger.NewKairosLogger("auroraboot", "info", false)
		log = &l
	}

	config, err := ops.NewConfig(
		ops.WithImageExtractor(imageutils.OCIImageExtractor{Insecure: opts.AllowInsecureRegistries}),
		ops.WithLogger(*log),
	)
	if err != nil {
		return err
	}
	if opts.Arch != "" {
		config.Arch = opts.Arch
	}

	outputType := opts.OutputType
	if outputType == "" {
		outputType = string(constants.DefaultOutput)
	}

	if err := checkBuildUKIDeps(config.Arch, outputType); err != nil {
		return err
	}
	outputDir := opts.OutputDir
	if outputDir == "" {
		outputDir = "."
	}
	bootBranding := opts.BootBranding
	if bootBranding == "" {
		bootBranding = "Kairos"
	}
	secureBootEnroll := opts.SecureBootEnroll
	if secureBootEnroll == "" {
		secureBootEnroll = "if-safe"
	}

	artifactsTempDir, err := os.MkdirTemp("", "auroraboot-build-uki-artifacts-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(artifactsTempDir)

	log.Info("Extracting image to a temporary directory")
	sourceDir, err := os.MkdirTemp("", "auroraboot-build-uki-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(sourceDir)

	// 755 so non-root users can access sockets under sourceDir.
	if err := os.Chmod(sourceDir, 0o755); err != nil {
		return err
	}

	imgSource, err := sdkImages.NewSrcFromURI(opts.Source)
	if err != nil {
		return fmt.Errorf("not a valid rootfs source image argument: %s", opts.Source)
	}

	e := elemental.NewElemental(config)
	if _, err := e.DumpSource(sourceDir, imgSource); err != nil {
		return fmt.Errorf("extracting image source: %w", err)
	}

	if opts.OverlayRootfs != "" {
		log.Infof("Adding files from %s to rootfs", opts.OverlayRootfs)
		overlay, err := sdkImages.NewSrcFromURI(fmt.Sprintf("dir:%s", opts.OverlayRootfs))
		if err != nil {
			return fmt.Errorf("error creating overlay image: %s", err)
		}
		if _, err := e.DumpSource(sourceDir, overlay); err != nil {
			return fmt.Errorf("error copying overlay image: %s", err)
		}
	}

	kairosVersion, err := findKairosVersion(sourceDir)
	if err != nil {
		return err
	}

	outputName := utils.NameFromRootfs(sourceDir)

	log.Info("Creating additional directories in the rootfs")
	if err := setupDirectoriesAndFiles(sourceDir); err != nil {
		return err
	}

	log.Info("Copying kernel")
	if err := copyKernel(sourceDir, artifactsTempDir); err != nil {
		return fmt.Errorf("copying kernel: %w", err)
	}

	// Remove the boot directory now that we've copied the kernel — we don't
	// want the initrd files from the source rootfs to end up inside the uki
	// initramfs.
	if err := os.RemoveAll(filepath.Join(sourceDir, "boot")); err != nil {
		return fmt.Errorf("cleaning up the source directory: %w", err)
	}

	log.Info("Creating an initramfs file")
	if err := createInitramfs(sourceDir, artifactsTempDir); err != nil {
		return err
	}

	// Build the base cmdline from cloud config: enabled install.selinux on a
	// family that supports it replaces the hardcoded selinux=0, otherwise the
	// base stays identical to constants.UkiCmdline.
	baseCmdline, err := selinuxBaseCmdline(log, sourceDir, opts.CloudConfig)
	if err != nil {
		return err
	}

	entries := append(
		getUkiCmdline(baseCmdline, opts.ExtendCmdline, bootBranding, opts.ExtraCmdlines, opts.CmdLinesV2),
		getUkiSingleCmdlines(baseCmdline, bootBranding, opts.SingleEfiCmdlines, *log)...,
	)

	stub, systemdBoot, outputSystemdBootEfi, err := resolveSdBootFiles(sourceDir, config.Arch, opts.SdBootInSource)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		log.Info(fmt.Sprintf("Running ukify for cmdline: %s: %s", entry.Title, entry.Cmdline))
		log.Infof("Generating: %s.efi", entry.FileName)

		if log.GetLevel().String() == "debug" {
			slog.SetLogLoggerLevel(slog.LevelDebug)
		}
		builder := &goukiuki.Builder{
			Arch:          config.Arch,
			Version:       kairosVersion,
			KernelPath:    filepath.Join(artifactsTempDir, "vmlinuz"),
			InitrdPath:    filepath.Join(artifactsTempDir, "initrd"),
			Cmdline:       entry.Cmdline,
			OsRelease:     filepath.Join(sourceDir, "etc/os-release"),
			OutUKIPath:    filepath.Join(sourceDir, entry.FileName+".efi"),
			PCRKey:        opts.TPMPCRPrivateKey,
			SBKey:         opts.SBKey,
			SBCert:        opts.SBCert,
			SdBootPath:    systemdBoot,
			SdStubPath:    stub,
			OutSdBootPath: filepath.Join(sourceDir, outputSystemdBootEfi),
			Splash:        opts.Splash,
		}

		if opts.CmdLinesV2 {
			for _, cmd := range opts.ExtraCmdlines {
				slog.Debug("Expanding extra cmdline with base", "cmdline", cmd, "base", entry.Cmdline)
				builder.ExtraCmdlines = append(builder.ExtraCmdlines, fmt.Sprintf("%s %s", entry.Cmdline, cmd))
			}
		}

		if err := builder.Build(); err != nil {
			return err
		}

		log.Info("Creating kairos and loader conf files")
		log.Info("Creating base config file with profile 0")
		if err := createConfFiles(baseCmdline, sourceDir, entry.Cmdline, entry.Title, entry.FileName, kairosVersion, "0", opts.IncludeVersionInConfig, opts.IncludeCmdlineInConfig); err != nil {
			return err
		}
		if opts.CmdLinesV2 {
			for i, cmd := range opts.ExtraCmdlines {
				log.Info("Creating extra config file for cmdline", "cmdline", cmd)
				profile := fmt.Sprintf("%d", i+1)
				title := fmt.Sprintf("%s (%s)", entry.Title, cmd)

				if err := createConfFiles(baseCmdline, sourceDir, fmt.Sprintf("%s %s", entry.Cmdline, cmd), title, entry.FileName, kairosVersion, profile, opts.IncludeVersionInConfig, opts.IncludeCmdlineInConfig); err != nil {
					return err
				}
			}
		}
	}

	if err := createSystemdConf(sourceDir, secureBootEnroll); err != nil {
		return err
	}

	if err := os.MkdirAll(outputDir, os.ModeDir|os.ModePerm); err != nil {
		return fmt.Errorf("creating output dir %s: %w", outputDir, err)
	}

	switch outputType {
	case string(constants.IsoOutput):
		if err := createISO(e, sourceDir, outputDir, opts.OverlayISO, opts.CloudConfig, opts.PublicKeysDir, outputName, opts.Name, entries, *log, config.Arch, opts.ExtensionsCatalogs, opts.Extensions, opts.AllowInsecureRegistries); err != nil {
			return err
		}
	case string(constants.ContainerOutput):
		temp, err := os.MkdirTemp("", "uki-transient-*")
		if err != nil {
			return err
		}
		defer os.RemoveAll(temp)
		if err := createArtifact(sourceDir, temp, opts.PublicKeysDir, entries, *log, config.Arch); err != nil {
			return err
		}
		if err := createContainer(temp, outputDir, opts.Name, outputName, *log, config.Arch); err != nil {
			return err
		}
	case string(constants.DefaultOutput):
		if err := createArtifact(sourceDir, outputDir, opts.PublicKeysDir, entries, *log, config.Arch); err != nil {
			return err
		}
	}

	log.Infof("Done building %s at: %s", outputType, outputDir)
	return nil
}

func (o Options) validate() error {
	if o.Source == "" {
		return errors.New("uki.Build: Source is required")
	}
	if o.SBKey == "" || o.SBCert == "" {
		return errors.New("uki.Build: SBKey and SBCert are required")
	}
	if o.TPMPCRPrivateKey == "" {
		return errors.New("uki.Build: TPMPCRPrivateKey is required")
	}
	if len(o.ExtraCmdlines) > 0 && o.ExtendCmdline != "" {
		return errors.New("uki.Build: ExtraCmdlines and ExtendCmdline are mutually exclusive")
	}
	if err := o.validateEntryFileNames(); err != nil {
		return err
	}

	outputType := o.OutputType
	if outputType == "" {
		outputType = string(constants.DefaultOutput)
	}
	if outputType != string(constants.DefaultOutput) && outputType != string(constants.IsoOutput) && outputType != string(constants.ContainerOutput) {
		return fmt.Errorf("uki.Build: invalid output type: %s", outputType)
	}
	if len(o.Extensions) > 0 {
		if outputType != string(constants.IsoOutput) {
			return errors.New("extensions are only supported for iso artifacts")
		}
	}

	if o.OverlayRootfs != "" {
		ol, err := os.Stat(o.OverlayRootfs)
		if err != nil {
			return fmt.Errorf("overlay-rootfs directory does not exist: %s", o.OverlayRootfs)
		}
		if !ol.IsDir() {
			return fmt.Errorf("overlay-rootfs is not a directory: %s", o.OverlayRootfs)
		}
	}

	if o.OverlayISO != "" {
		ol, err := os.Stat(o.OverlayISO)
		if err != nil {
			return fmt.Errorf("overlay directory does not exist: %s", o.OverlayISO)
		}
		if !ol.IsDir() {
			return fmt.Errorf("overlay is not a directory: %s", o.OverlayISO)
		}
		if outputType != string(constants.IsoOutput) {
			return errors.New("overlay-iso is only supported for iso artifacts")
		}
	}

	if o.PublicKeysDir != "" {
		if _, err := os.Stat(o.PublicKeysDir); err != nil {
			return fmt.Errorf("keys directory does not exist: %s", o.PublicKeysDir)
		}
		for _, file := range []string{"db.auth", "KEK.auth", "PK.auth"} {
			if _, err := os.Stat(filepath.Join(o.PublicKeysDir, file)); err != nil {
				return fmt.Errorf("keys directory does not contain required file: %s", file)
			}
		}
	}

	if !strings.HasPrefix(o.TPMPCRPrivateKey, "pkcs11:") {
		if _, err := os.Stat(o.TPMPCRPrivateKey); err != nil {
			return fmt.Errorf("tpm-pcr-private-key does not exist: %s", o.TPMPCRPrivateKey)
		}
	}
	if !strings.HasPrefix(o.SBKey, "pkcs11:") {
		if _, err := os.Stat(o.SBKey); err != nil {
			return fmt.Errorf("sb-key does not exist: %s", o.SBKey)
		}
	}
	if _, err := os.Stat(o.SBCert); err != nil {
		return fmt.Errorf("sb-cert does not exist: %s", o.SBCert)
	}
	return nil
}

// validateEntryFileNames rejects boot entries that would be written to the
// same EFI file, as the later one would silently replace the earlier one.
func (o Options) validateEntryFileNames() error {
	bootBranding := o.BootBranding
	if bootBranding == "" {
		bootBranding = "Kairos"
	}
	entries := append(
		getUkiCmdline(constants.UkiCmdline, o.ExtendCmdline, bootBranding, o.ExtraCmdlines, o.CmdLinesV2),
		getUkiSingleCmdlines(constants.UkiCmdline, bootBranding, o.SingleEfiCmdlines, logger.KairosLogger{})...,
	)
	seen := map[string]utils.BootEntry{}
	for _, entry := range entries {
		if prev, ok := seen[entry.FileName]; ok {
			return fmt.Errorf("uki.Build: boot entries %q and %q both produce %s.efi, use distinct titles", prev.Title, entry.Title, entry.FileName)
		}
		seen[entry.FileName] = entry
	}
	return nil
}

func resolveSdBootFiles(sourceDir, arch string, inSource bool) (stub, sdBoot, outEfi string, err error) {
	// The ESP fallback name is the one thing both this function and imageFiles
	// need, so it comes from a single place.
	outEfi, err = utils.EfiFallbackName(arch)
	if err != nil {
		return "", "", "", err
	}

	var stubName, sdBootName, sdBootPath string
	switch {
	case utils.IsAmd64(arch):
		stubName, sdBootName, sdBootPath = constants.UkiSystemdBootStubx86Name, constants.UkiSystemdBootx86Name, constants.UkiSystemdBootx86Path
	case utils.IsArm64(arch):
		stubName, sdBootName, sdBootPath = constants.UkiSystemdBootStubArmName, constants.UkiSystemdBootArmName, constants.UkiSystemdBootArmPath
	default:
		stubName, sdBootName, sdBootPath = constants.UkiSystemdBootStubRiscv64Name, constants.UkiSystemdBootRiscv64Name, constants.UkiSystemdBootRiscv64Path
	}

	if inSource {
		stub, err = FindFirstFileInDir(sourceDir, stubName)
		if err != nil {
			return "", "", "", fmt.Errorf("finding systemd-boot stub in source: %w", err)
		}
		sdBoot, err = FindFirstFileInDir(sourceDir, sdBootName)
		if err != nil {
			return "", "", "", fmt.Errorf("finding systemd-boot in source: %w", err)
		}
		return stub, sdBoot, outEfi, nil
	}

	stub, err = getEfiStub(arch)
	if err != nil {
		return "", "", "", err
	}
	return stub, sdBootPath, outEfi, nil
}

// hostBinaries returns the host binaries Build runs for the given output type.
func hostBinaries(outputType string) []string {
	if outputType == string(constants.IsoOutput) {
		return []string{"dd", "mformat", "mmd", "mcopy", "xorriso"}
	}
	return nil
}

func checkBuildUKIDeps(arch, outputType string) error {
	for _, b := range hostBinaries(outputType) {
		if _, err := exec.LookPath(b); err != nil {
			return err
		}
	}

	neededFiles, err := getEfiNeededFiles(arch)
	if err != nil {
		return err
	}
	for _, b := range neededFiles {
		if _, err := os.Stat(b); err != nil {
			return err
		}
	}
	return nil
}

func getEfiNeededFiles(arch string) ([]string, error) {
	switch {
	case utils.IsAmd64(arch):
		return []string{constants.UkiSystemdBootStubx86Path, constants.UkiSystemdBootx86Path}, nil
	case utils.IsArm64(arch):
		return []string{constants.UkiSystemdBootStubArmPath, constants.UkiSystemdBootArmPath}, nil
	case utils.IsRiscv64(arch):
		return []string{constants.UkiSystemdBootStubRiscv64Path, constants.UkiSystemdBootRiscv64Path}, nil
	default:
		return nil, fmt.Errorf("unsupported arch: %s", arch)
	}
}

func findKairosVersion(sourceDir string) (string, error) {
	if _, err := os.Stat(sourceDir); err != nil {
		return "", fmt.Errorf("source directory does not exist: %s: %w", sourceDir, err)
	}

	var osReleaseBytes []byte
	var err error

	kairosReleasePath := filepath.Join(sourceDir, "etc", "kairos-release")
	osReleaseBytes, err = os.ReadFile(kairosReleasePath)
	if err != nil {
		osReleasePath := filepath.Join(sourceDir, "etc", "os-release")
		osReleaseBytes, err = os.ReadFile(osReleasePath)
		if err != nil {
			etcDir := filepath.Join(sourceDir, "etc")
			if etcInfo, statErr := os.Stat(etcDir); statErr == nil && etcInfo.IsDir() {
				entries, listErr := os.ReadDir(etcDir)
				if listErr == nil {
					var files []string
					for _, entry := range entries {
						files = append(files, entry.Name())
					}
					return "", fmt.Errorf("reading kairos-release or os-release file: %w (found files in etc/: %v)", err, files)
				}
			}
			entries, listErr := os.ReadDir(sourceDir)
			if listErr == nil {
				var dirs []string
				for _, entry := range entries {
					if entry.IsDir() {
						dirs = append(dirs, entry.Name())
					}
				}
				return "", fmt.Errorf("reading kairos-release or os-release file: %w (top-level directories found: %v)", err, dirs)
			}
			return "", fmt.Errorf("reading kairos-release or os-release file: %w", err)
		}
	}

	re := regexp.MustCompile("(?m)^KAIROS_RELEASE=\"(.*)\"")
	match := re.FindStringSubmatch(string(osReleaseBytes))
	if len(match) != 2 {
		return "", fmt.Errorf("unexpected number of matches for KAIROS_RELEASE in os-release: %d", len(match))
	}
	return match[1], nil
}

func setupDirectoriesAndFiles(workDir string) error {
	if err := os.Symlink("/usr/bin/immucore", filepath.Join(workDir, "init")); err != nil {
		return fmt.Errorf("error creating symlink: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(workDir, "oem"), os.ModeDir); err != nil {
		return fmt.Errorf("error creating /oem dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(workDir, "efi"), os.ModeDir); err != nil {
		return fmt.Errorf("error creating /efi dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(workDir, "usr/local/cloud-config"), os.ModeDir); err != nil {
		return fmt.Errorf("error creating /usr/local/cloud-config dir: %w", err)
	}
	return nil
}

func copyKernel(sourceDir, targetDir string) error {
	kernel := filepath.Join(sourceDir, "boot", "vmlinuz")

	linkTarget, err := os.Readlink(kernel)
	if err != nil {
		linkTarget = kernel
	}

	kernelFile := filepath.Base(linkTarget)
	sourceFile, err := os.Open(filepath.Join(sourceDir, "boot", kernelFile))
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destinationFile, err := os.Create(filepath.Join(targetDir, "vmlinuz"))
	if err != nil {
		return err
	}
	defer destinationFile.Close()
	_, err = io.Copy(destinationFile, sourceFile)
	return err
}

// createInitramfs creates a zstd-compressed cpio initramfs named "initrd"
// under artifactsTempDir, from the contents of sourceDir.
func createInitramfs(sourceDir, artifactsTempDir string) error {
	format := "newc"
	archiver, err := cpio.Format(format)
	if err != nil {
		return fmt.Errorf("format %q not supported: %w", format, err)
	}

	cpioFileName := filepath.Join(artifactsTempDir, "initramfs.cpio")
	cpioFile, err := os.Create(cpioFileName)
	if err != nil {
		return fmt.Errorf("creating cpio file: %w", err)
	}
	defer cpioFile.Close()

	rw := archiver.Writer(cpioFile)
	cr := cpio.NewRecorder()

	excludeDirs := map[string]bool{
		"sys":  true,
		"run":  true,
		"dev":  true,
		"tmp":  true,
		"proc": true,
	}

	// Walk sourceDir with absolute paths (no os.Chdir) and record each entry
	// under a path relative to sourceDir, so the initramfs layout is rooted at
	// "." regardless of the process working directory.
	err = filepath.Walk(sourceDir, func(filePath string, fileInfo os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relPath, err := filepath.Rel(sourceDir, filePath)
		if err != nil {
			return fmt.Errorf("resolving %q relative to %q: %w", filePath, sourceDir, err)
		}
		if fileInfo.IsDir() && excludeDirs[relPath] {
			return filepath.SkipDir
		}
		if strings.Contains(relPath, "initramfs.cpio") {
			return nil
		}

		rec, err := cr.GetRecord(filePath)
		if err != nil {
			return fmt.Errorf("getting record of %q failed: %w", filePath, err)
		}

		rec.Name = relPath

		// MakeReproducible currently breaks hardlinks
		// (https://github.com/u-root/u-root/issues/3031); zero out the metadata
		// fields we can safely normalize instead.
		rec.MTime = 0
		rec.UID = 0
		rec.GID = 0
		rec.Dev = 0
		rec.Major = 0
		rec.Minor = 0
		if err := rw.WriteRecord(rec); err != nil {
			return fmt.Errorf("writing record %q failed: %w", filePath, err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("error walking the source dir: %w", err)
	}

	if err := cpio.WriteTrailer(rw); err != nil {
		return fmt.Errorf("error writing trailer record: %w", err)
	}

	if err := ZstdFile(cpioFileName, filepath.Join(artifactsTempDir, "initrd")); err != nil {
		return err
	}
	if err := os.RemoveAll(cpioFileName); err != nil {
		return fmt.Errorf("error deleting cpio file: %w", err)
	}
	return nil
}

// ZstdFile compresses sourcePath into targetPath using zstd best-compression.
func ZstdFile(sourcePath, targetPath string) error {
	inputFile, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("error opening initramfs file: %w", err)
	}
	defer inputFile.Close()

	outputFile, err := os.Create(targetPath)
	if err != nil {
		return fmt.Errorf("error creating compressed initramfs file: %w", err)
	}

	zstdWriter, err := zstd.NewWriter(outputFile, zstd.WithEncoderLevel(zstd.SpeedBestCompression))
	if err != nil {
		_ = outputFile.Close()
		return fmt.Errorf("error creating zstd encoder: %w", err)
	}

	if _, err := io.Copy(zstdWriter, inputFile); err != nil {
		_ = zstdWriter.Close()
		_ = outputFile.Close()
		return fmt.Errorf("error writing data to the compress initramfs file: %w", err)
	}
	if err := zstdWriter.Close(); err != nil {
		_ = outputFile.Close()
		return fmt.Errorf("error finishing the compressed initramfs file: %w", err)
	}
	if err := outputFile.Close(); err != nil {
		return fmt.Errorf("error closing the compressed initramfs file: %w", err)
	}
	return nil
}

func getEfiStub(arch string) (string, error) {
	switch {
	case utils.IsAmd64(arch):
		return constants.UkiSystemdBootStubx86Path, nil
	case utils.IsArm64(arch):
		return constants.UkiSystemdBootStubArmPath, nil
	case utils.IsRiscv64(arch):
		return constants.UkiSystemdBootStubRiscv64Path, nil
	default:
		return "", nil
	}
}

func createConfFiles(baseCmdline, sourceDir, cmdline, title, finalEfiName, version, profile string, includeVersion, includeCmdline bool) error {
	if _, err := os.Stat(filepath.Join(sourceDir, "entries")); os.IsNotExist(err) {
		if err := os.Mkdir(filepath.Join(sourceDir, "entries"), os.ModePerm); err != nil {
			return fmt.Errorf("error creating entries directory: %w", err)
		}
	}

	extraCmdline := strings.TrimSpace(strings.TrimPrefix(cmdline, baseCmdline))

	configData := fmt.Sprintf("title %s\nsort-key %s-%s\nuki /EFI/kairos/%s.efi\nprofile %s\n", title, finalEfiName, profile, finalEfiName, profile)
	if includeVersion {
		configData = fmt.Sprintf("%sversion %s\n", configData, version)
	}
	if includeCmdline {
		configData = fmt.Sprintf("%scmdline %s\n", configData, strings.Trim(extraCmdline, " "))
	}

	confName := finalEfiName + ".conf"
	if profile != "0" {
		confName = fmt.Sprintf("%s-profile%s.conf", finalEfiName, profile)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "entries", confName), []byte(configData), os.ModePerm); err != nil {
		return fmt.Errorf("creating the %s.conf file", finalEfiName)
	}
	return nil
}

func createSystemdConf(dir, secureBootEnroll string) error {
	data := fmt.Sprintf("timeout 5\nconsole-mode max\neditor no\nsecure-boot-enroll %s\n", secureBootEnroll)
	if err := os.WriteFile(filepath.Join(dir, "loader.conf"), []byte(data), os.ModePerm); err != nil {
		return fmt.Errorf("creating the loader.conf file: %s", err)
	}
	return nil
}

func createISO(e *elemental.Elemental, sourceDir, outputDir, overlayISO, cloudConfig, keysDir, outputName, artifactName string, entries []utils.BootEntry, log logger.KairosLogger, arch string, extensionsCatalogs []string, extensionRequests []extensions.Request, insecure bool) error {
	isoDir, err := os.MkdirTemp("", "auroraboot-iso-dir-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(isoDir)

	if err := stageExtensions(context.Background(), extensionsCatalogs, extensionRequests, arch, isoDir, insecure); err != nil {
		return err
	}

	filesMap, err := imageFiles(sourceDir, keysDir, entries, arch)
	if err != nil {
		return err
	}

	log.Info("Calculating the size of the img file")
	artifactSize, err := sumFileSizes(filesMap)
	if err != nil {
		return err
	}

	imgSize := artifactSize + 1
	imgFile := filepath.Join(isoDir, "efiboot.img")
	log.Info(fmt.Sprintf("Creating the img file with size: %dMb", imgSize))
	if err := createImgWithSize(imgFile, imgSize); err != nil {
		return err
	}
	defer os.Remove(imgFile)

	log.Info(fmt.Sprintf("Created image: %s", imgFile))

	log.Info("Creating directories in the img file")
	if err := createImgDirs(imgFile, filesMap); err != nil {
		return err
	}

	log.Info("Copying files in the img file")
	if err := copyFilesToImg(imgFile, filesMap); err != nil {
		return err
	}

	if err := writeISOCloudConfig(isoDir, cloudConfig); err != nil {
		return err
	}

	if overlayISO != "" {
		log.Infof("Overlay dir is set, copying files from %s", overlayISO)
		log.Infof("Adding files from %s to iso", overlayISO)
		// Dereference symlinks / drop kubelet atomic-writer internals so a
		// Secret- or ConfigMap-mounted overlay can't shadow real ISO-root
		// dirs with symlinks (kairos-io/kairos#4324).
		materialized, err := utils.MaterializeOverlay(overlayISO)
		if err != nil {
			log.Errorf("error materializing overlay image: %s", err)
			return err
		}
		defer os.RemoveAll(materialized)
		overlay, err := sdkImages.NewSrcFromURI(fmt.Sprintf("dir:%s", materialized))
		if err != nil {
			log.Errorf("error creating overlay image: %s", err)
			return err
		}
		if _, err := e.DumpSource(isoDir, overlay); err != nil {
			log.Errorf("error copying overlay image: %s", err)
			return err
		}
	}

	isoName := fmt.Sprintf("%s-%s-uki.iso", constants.KairosDefaultArtifactName, outputName)
	if artifactName != "" {
		isoName = fmt.Sprintf("%s.iso", artifactName)
	}

	log.Debugf("Got output name: %s", isoName)

	log.Info("Creating the iso files with xorriso")
	cmd := exec.Command("xorriso", "-as", "mkisofs", "-V", "UKI_ISO_INSTALL", "-isohybrid-gpt-basdat",
		"-e", filepath.Base(imgFile), "-no-emul-boot", "-o", filepath.Join(outputDir, isoName), isoDir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("error creating iso file: %w\n%s", err, string(out))
	}
	return nil
}

// writeISOCloudConfig places the cloud-config at the ISO root as config.yaml,
// where the live system picks it up, same as build-iso does. It runs before
// the overlay is copied, so a config.yaml in the overlay still takes over.
func writeISOCloudConfig(isoDir, cloudConfig string) error {
	if strings.TrimSpace(cloudConfig) == "" {
		return nil
	}
	if err := os.WriteFile(filepath.Join(isoDir, "config.yaml"), []byte(cloudConfig), 0o600); err != nil {
		return fmt.Errorf("writing cloud config to the iso: %w", err)
	}
	return nil
}

var materializeExtensions = extensions.Materialize

func stageExtensions(ctx context.Context, catalogs []string, requests []extensions.Request, arch, destination string, insecure bool) error {
	_, err := materializeExtensions(ctx, catalogs, requests, arch, destination, insecure)
	return err
}

func imageFiles(sourceDir, keysDir string, entries []utils.BootEntry, arch string) (map[string][]string, error) {
	bootfile, err := utils.EfiFallbackName(arch)
	if err != nil {
		return nil, err
	}
	data := map[string][]string{
		"EFI":            {},
		"EFI/BOOT":       {filepath.Join(sourceDir, bootfile)},
		"EFI/kairos":     {},
		"EFI/tools":      {},
		"loader":         {filepath.Join(sourceDir, "loader.conf")},
		"loader/entries": {},
		"loader/keys":    {},
	}
	if keysDir != "" {
		data["loader/keys/auto"] = []string{
			filepath.Join(keysDir, "PK.auth"),
			filepath.Join(keysDir, "KEK.auth"),
			filepath.Join(keysDir, "db.auth"),
		}
	}

	for _, entry := range entries {
		data["EFI/kairos"] = append(data["EFI/kairos"], filepath.Join(sourceDir, entry.FileName+".efi"))
	}
	files, err := os.ReadDir(filepath.Join(sourceDir, "entries"))
	if err != nil {
		return data, fmt.Errorf("reading source dir: %w", err)
	}
	for _, f := range files {
		if strings.HasSuffix(f.Name(), ".conf") {
			data["loader/entries"] = append(data["loader/entries"], filepath.Join(sourceDir, "entries", f.Name()))
		}
	}
	return data, nil
}

func sumFileSizes(filesMap map[string][]string) (int64, error) {
	total := int64(0)
	fileCount := 0

	for _, files := range maps.Values(filesMap) {
		for _, f := range files {
			fileInfo, err := os.Stat(f)
			if err != nil {
				return total, fmt.Errorf("finding file info for file %s: %w", f, err)
			}
			total += fileInfo.Size()
			fileCount++
		}
	}

	// Account for FAT filesystem overhead: per-file directory entries, FAT
	// table + boot sector, and cluster slack from 4KB clusters.
	dirCount := len(filesMap)
	dirEntryOverhead := int64((fileCount + dirCount) * 32)
	fatOverhead := int64(float64(total)*0.02) + 512*1024
	clusterSlack := int64(fileCount * 2048)

	totalWithOverhead := total + dirEntryOverhead + fatOverhead + clusterSlack
	totalInMB := int64(math.Ceil(float64(totalWithOverhead) / (1024 * 1024)))
	return totalInMB, nil
}

func createImgWithSize(imgFile string, size int64) error {
	cmd := exec.Command(
		"dd",
		"if=/dev/zero", fmt.Sprintf("of=%s", imgFile),
		"bs=1M", fmt.Sprintf("count=%d", size),
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("creating the img file: %w\n%s", err, out)
	}

	cmd = exec.Command("mformat", "-i", imgFile, "-F", "::")
	out, err = cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("formating the img file: %w\n%s", err, out)
	}
	return nil
}

func createImgDirs(imgFile string, filesMap map[string][]string) error {
	dirs := maps.Keys(filesMap)
	sort.Strings(dirs)
	for _, dir := range dirs {
		cmd := exec.Command("mmd", "-i", imgFile, fmt.Sprintf("::%s", dir))
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("creating directory %s on the img file: %w\n%s\nThe failed command was: %s", dir, err, string(out), cmd.String())
		}
	}
	return nil
}

func copyFilesToImg(imgFile string, filesMap map[string][]string) error {
	for dir, files := range filesMap {
		for _, f := range files {
			cmd := exec.Command("mcopy", "-i", imgFile, f, filepath.Join(fmt.Sprintf("::%s", dir), filepath.Base(f)))
			out, err := cmd.CombinedOutput()
			if err != nil {
				return fmt.Errorf("copying %s in img file: %w\n%s", f, err, string(out))
			}
		}
	}
	return nil
}

// createArtifact copies the files produced in sourceDir out to outputDir,
// preserving the layout so the result can be used directly as a UKI boot
// directory.
func createArtifact(sourceDir, outputDir, keysDir string, entries []utils.BootEntry, log logger.KairosLogger, arch string) error {
	filesMap, err := imageFiles(sourceDir, keysDir, entries, arch)
	if err != nil {
		return err
	}
	for dir, files := range filesMap {
		log.Debugf("creating dir %s", filepath.Join(outputDir, dir))
		if err := os.MkdirAll(filepath.Join(outputDir, dir), os.ModeDir|os.ModePerm); err != nil {
			log.Errorf("creating dir %s: %s", dir, err)
			return err
		}
		for _, f := range files {
			log.Debugf("copying %s to %s", f, filepath.Join(outputDir, dir, filepath.Base(f)))
			source, err := os.Open(f)
			if err != nil {
				log.Errorf("opening file %s: %s", f, err)
				return err
			}
			destination, err := os.Create(filepath.Join(outputDir, dir, filepath.Base(f)))
			if err != nil {
				source.Close()
				log.Errorf("creating file %s: %s", filepath.Join(outputDir, dir, filepath.Base(f)), err)
				return err
			}
			if _, err := io.Copy(destination, source); err != nil {
				source.Close()
				destination.Close()
				log.Errorf("copying file %s: %s", f, err)
				return err
			}
			if err := source.Close(); err != nil {
				log.Errorf("closing file %s: %s", f, err)
			}
			if err := destination.Close(); err != nil {
				log.Errorf("closing file %s: %s", filepath.Join(outputDir, dir, filepath.Base(f)), err)
			}
		}
	}
	return nil
}

func createContainer(sourceDir, outputDir, artifactName, outputName string, log logger.KairosLogger, arch string) error {
	temp, err := os.CreateTemp("", "image.tar")
	if err != nil {
		return err
	}
	log.Logger.Info().Str("sourceDir", sourceDir).Str("temp", temp.Name()).Msg("Creating tarball")
	if err := utils.Tar(sourceDir, temp); err != nil {
		return err
	}
	_ = temp.Close()
	defer os.RemoveAll(temp.Name())

	finalImage := filepath.Join(outputDir, fmt.Sprintf("%s-%s-uki.tar", constants.KairosDefaultArtifactName, outputName))
	tarName := "kairos.tar"
	if artifactName != "" {
		tarName = fmt.Sprintf("%s.tar", artifactName)
	}
	log.Debugf("Got output name: %s", tarName)
	return utils.CreateTar(log, temp.Name(), finalImage, tarName, arch, "linux")
}

// GetUkiCmdline returns the set of boot entries (one per cmdline variant) used
// to generate UKI EFI files, using the standard UKI base cmdline. Extend mode
// appends to the default cmdline and produces a single entry; extra mode
// produces one entry per extra cmdline.
func GetUkiCmdline(cmdlineExtend, bootBranding string, extraCmdlines []string, cmdLinesV2 bool) []utils.BootEntry {
	return getUkiCmdline(constants.UkiCmdline, cmdlineExtend, bootBranding, extraCmdlines, cmdLinesV2)
}

// getUkiCmdline is GetUkiCmdline with an explicit base cmdline
func getUkiCmdline(baseCmdline, cmdlineExtend, bootBranding string, extraCmdlines []string, cmdLinesV2 bool) []utils.BootEntry {
	defaultCmdLine := baseCmdline

	if cmdlineExtend != "" {
		return []utils.BootEntry{{
			Cmdline:  defaultCmdLine + " " + cmdlineExtend,
			Title:    bootBranding,
			FileName: constants.ArtifactBaseName,
		}}
	}

	result := []utils.BootEntry{{
		Cmdline:  defaultCmdLine,
		Title:    bootBranding,
		FileName: constants.ArtifactBaseName,
	}}

	if !cmdLinesV2 {
		for _, extra := range extraCmdlines {
			cmdline := defaultCmdLine + " " + extra
			result = append(result, utils.BootEntry{
				Cmdline:  cmdline,
				Title:    bootBranding,
				FileName: nameFromCmdline(baseCmdline, constants.ArtifactBaseName, cmdline),
			})
		}
	}
	return result
}

// GetUkiSingleCmdlines returns boot entries for the `single-efi-cmdline` flag
// values, using the standard UKI base cmdline. Each user value may optionally
// include a "Title: cmdline" prefix; a title holds no '=' and no '/'.
func GetUkiSingleCmdlines(bootBranding string, cmdlines []string, l logger.KairosLogger) []utils.BootEntry {
	return getUkiSingleCmdlines(constants.UkiCmdline, bootBranding, cmdlines, l)
}

// getUkiSingleCmdlines is GetUkiSingleCmdlines with an explicit base cmdline.
func getUkiSingleCmdlines(baseCmdline, bootBranding string, cmdlines []string, _ logger.KairosLogger) []utils.BootEntry {
	result := []utils.BootEntry{}
	defaultCmdLine := baseCmdline

	for _, userValue := range cmdlines {
		bootEntry := utils.BootEntry{}
		title, cmdline, hasTitle := cutEntryTitle(userValue)
		if hasTitle {
			bootEntry.Title = fmt.Sprintf("%s (%s)", bootBranding, title)
			bootEntry.Cmdline = defaultCmdLine + " " + cmdline
			bootEntry.FileName = strings.ToLower(strings.ReplaceAll(title, " ", "_"))
		} else {
			bootEntry.Title = bootBranding
			bootEntry.Cmdline = defaultCmdLine + " " + cmdline
			bootEntry.FileName = nameFromCmdline(baseCmdline, "single_entry", cmdline)
		}
		result = append(result, bootEntry)
	}
	return result
}

// cutEntryTitle splits a single-efi-cmdline value into "Title: cmdline".
// The text before the first colon is only a title when it holds no '=' and
// no '/', so untitled values such as ip=a::b:c keep every colon and a title
// never puts a path separator in the EFI file name.
func cutEntryTitle(userValue string) (title, cmdline string, hasTitle bool) {
	before, after, found := strings.Cut(userValue, ":")
	if !found || strings.ContainsAny(before, "=/") {
		return "", userValue, false
	}
	return before, after, true
}

// NameFromCmdline returns a filesystem-safe basename derived from cmdline,
// used for the per-entry EFI and .conf file names, using the standard UKI
// base cmdline.
func NameFromCmdline(basename, cmdline string) string {
	return nameFromCmdline(constants.UkiCmdline, basename, cmdline)
}

// nameFromCmdline is NameFromCmdline with an explicit base cmdline.
func nameFromCmdline(baseCmdline, basename, cmdline string) string {
	cmdlineForEfi := strings.TrimSpace(strings.TrimPrefix(cmdline, baseCmdline))
	allowedChars := regexp.MustCompile(`[^a-zA-Z0-9._-]+`)
	cleanCmdline := allowedChars.ReplaceAllString(cmdlineForEfi, "_")
	name := basename + "_" + cleanCmdline
	return strings.ToLower(strings.TrimSuffix(name, "_"))
}

// selinuxDisabledParam is the token constants.UkiCmdline carries by default,
// and the one the SELinux fragment replaces when SELinux is enabled.
const selinuxDisabledParam = "selinux=0"

// selinuxBaseCmdline resolves the base cmdline every UKI entry is built from.
// SELinux is turned on only when the cloud-config asks for it and the source
// image family can do it; every other case keeps selinux=0.
//
// The family test is not cosmetic. The GRUB path turns SELinux on for the
// redhat and suse families only and pins selinux=0 for every other one, see
// setSelinux in kairos-init/pkg/bundled BootArgsCfg, so without it one
// cloud-config would produce two different kernel command lines for the same
// image. The UKI cmdline also lives in a signed section of the EFI, so an
// artifact built with the wrong token cannot be corrected without building
// and signing it again.
func selinuxBaseCmdline(log *logger.KairosLogger, sourceDir, cloudConfig string) (string, error) {
	enabled, mode, err := parseSelinuxOptions(log, cloudConfig)
	if err != nil {
		return "", err
	}

	supported := false
	if enabled {
		var family string
		supported, family = isSelinuxSupported(sourceDir, log)
		if supported {
			log.Infof("SELinux enabled in cloud-config, mode: %s", mode)
		} else {
			log.Warnf("install.selinux.enabled is set but image family %q does not support SELinux; booting with %s", family, selinuxDisabledParam)
		}
	}

	baseCmdline, err := spliceSelinuxCmdline(constants.UkiCmdline, enabled && supported, mode)
	if err != nil {
		log.Logger.Error().Err(err).Msg("SELinux fragment splice failed")
		return "", err
	}

	return baseCmdline, nil
}

// spliceSelinuxCmdline replaces the selinux=0 token in baseCmdline with the
// SELinux enablement fragment. When enable is false the base is returned
// unchanged.
func spliceSelinuxCmdline(baseCmdline string, enable bool, mode string) (string, error) {
	if !enable {
		return baseCmdline, nil
	}

	params := fmt.Sprintf("security=selinux selinux=1 enforcing=0 rd.cos.selinux=%s", mode)
	patched := strings.Replace(baseCmdline, " "+selinuxDisabledParam, " "+params, 1)
	if patched == baseCmdline {
		return "", fmt.Errorf("SELinux enablement requires the base cmdline to contain ' %s'", selinuxDisabledParam)
	}

	return patched, nil
}

// parseSelinuxOptions extracts install.selinux from a cloud-config.
func parseSelinuxOptions(log *logger.KairosLogger, cloudConfig string) (enabled bool, mode string, err error) {
	if cloudConfig == "" {
		return false, "", nil
	}

	var selinux *installtypes.SelinuxOptions

	decoder := yaml.NewDecoder(bytes.NewReader([]byte(cloudConfig)))
	for {
		var cc struct {
			Install *struct {
				Selinux *installtypes.SelinuxOptions `yaml:"selinux"`
			} `yaml:"install"`
		}

		if err := decoder.Decode(&cc); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return false, "", fmt.Errorf("parsing cloud-config YAML: %w", err)
		}

		if cc.Install == nil || cc.Install.Selinux == nil {
			continue // cc without install.selinux: keep looking
		}

		sel := cc.Install.Selinux
		if sel.Mode != "" && !sel.Enabled {
			log.Warn("install.selinux.mode is set but install.selinux.enabled is false; SELinux stays disabled")
		}

		selinux = sel
	}

	if selinux == nil || !selinux.Enabled {
		return false, "", nil
	}

	mode = strings.TrimSpace(selinux.Mode)
	switch mode {
	case "":
		mode = "permissive"
	case "permissive", "enforcing":
	default:
		log.Warnf("unknown install.selinux.mode %q, using permissive", mode)
		mode = "permissive"
	}
	return true, mode, nil
}

// FindFirstFileInDir walks dir recursively and returns the full path to the
// first entry whose basename matches the given glob pattern.
func FindFirstFileInDir(dir, pattern string) (string, error) {
	var foundFile string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			matched, err := filepath.Match(pattern, d.Name())
			if err != nil {
				return err
			}
			if matched {
				foundFile = path
				return filepath.SkipAll
			}
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if foundFile == "" {
		return "", fmt.Errorf("no file matching pattern %s found in directory %s", pattern, dir)
	}
	return foundFile, nil
}

// isSelinuxSupported reports whether the rootfs image family supports SELinux
func isSelinuxSupported(rootfs string, log *logger.KairosLogger) (bool, string) {
	family, err := utils.GetKairosFamily(rootfs)
	if err != nil {
		log.Logger.Error().Err(err).Msg("failed to get image family")
		return false, ""
	}
	switch strings.ToLower(family) {
	case "redhat", "suse":
		return true, family
	default:
		return false, family
	}
}
