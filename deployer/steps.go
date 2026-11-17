package deployer

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/hashicorp/go-multierror"
	"github.com/kairos-io/AuroraBoot/internal/netbootmgr"
	"github.com/kairos-io/AuroraBoot/pkg/constants"

	"github.com/kairos-io/AuroraBoot/pkg/ops"
	"github.com/spectrocloud-labs/herd"
)

// CleanTmpDirs removes the temp rootfs and netboot directories when finished to not leave things around
func (d *Deployer) CleanTmpDirs() error {
	if d.WorkDir != "" {
		// Everything the run created is in WorkDir. Never touch State.
		d.Log.Logger.Debug().Str("workDir", d.WorkDir).Msg("Cleaning up work directory")
		return os.RemoveAll(d.WorkDir)
	}

	var err *multierror.Error
	d.Log.Logger.Debug().Str("tmpRootFs", d.tmpRootFs()).Msg("Cleaning up temp rootfs directory")
	err = multierror.Append(err, os.RemoveAll(d.tmpRootFs()))
	if err.ErrorOrNil() != nil {
		d.Log.Logger.Error().Err(err).Msg("Failed to remove temp rootfs")
	}

	d.Log.Logger.Debug().Str("dstNetboot", d.dstNetboot()).Msg("Cleaning up temp netboot directory")
	err = multierror.Append(err, os.RemoveAll(d.dstNetboot()))

	if _, err2 := os.Stat(d.cloudConfigPath()); err2 == nil {
		d.Log.Logger.Debug().Str("config", d.cloudConfigPath()).Msg("Cleaning up config.yaml")
		err = multierror.Append(err, os.RemoveAll(d.cloudConfigPath()))
	}

	return err.ErrorOrNil()
}

// PrepDirs prepares the destination directory for the rest of the steps.
// This is the first step always executed in the deployer, it creates the destination directory in which other build steps will operate.
func (d *Deployer) PrepDirs() error {
	return d.Add(constants.OpPrepareDirs, herd.WithCallback(func(ctx context.Context) error {
		d.Log.Logger.Debug().Str("destination", d.destination()).Msg("Preparing destination temporal directory")
		if d.destination() == "" {
			d.Log.Logger.Error().Msg("Destination directory is not set, cannot prepare ISO directory")
			return fmt.Errorf("destination directory is not set")
		}
		err := os.MkdirAll(d.destination(), 0755)
		if err != nil {
			d.Log.Logger.Error().Err(err).Msg("Failed to create destination directory")
			return err
		}
		d.Log.Logger.Debug().Str("destination", d.tmpRootFs()).Msg("Preparing temp rootfs directory")
		err = os.RemoveAll(d.tmpRootFs())
		if err != nil {
			d.Log.Logger.Error().Err(err).Msg("Failed to remove temp rootfs")
			return err
		}
		err = os.MkdirAll(d.tmpRootFs(), 0755)
		if err != nil {
			d.Log.Logger.Error().Err(err).Msg("Failed to create temp rootfs directory")
			return err
		}

		d.Log.Logger.Debug().Str("destination", d.dstNetboot()).Msg("Preparing temp netboot directory")
		err = os.RemoveAll(d.dstNetboot())
		if err != nil {
			d.Log.Logger.Error().Err(err).Msg("Failed to remove temp netboot dir")
			return err
		}
		err = os.MkdirAll(d.dstNetboot(), 0755)
		if err != nil {
			d.Log.Logger.Error().Err(err).Msg("Failed to create temp netboot directory")
			return err
		}

		return nil
	}))
}

func (d *Deployer) StepCopyCloudConfig() error {
	return d.Add(constants.OpCopyCloudConfig,
		herd.WithDeps(constants.OpPrepareDirs),
		herd.WithCallback(func(ctx context.Context) error {
			// The cloud-config carries user passwords, join tokens and
			// kcrypt secrets, which is why it is written 0600 below. Keep
			// the document out of the default output: --loglevel defaults
			// to info, so anything logged here lands on stdout and in the
			// CI log of every plain build-iso run. -l debug is an explicit
			// opt-in and still shows it.
			d.Log.Logger.Info().Int("bytes", len(d.Config.CloudConfig)).Msg("Copying cloud config")
			d.Log.Logger.Debug().Str("cloudConfig", d.Config.CloudConfig).Msg("Cloud config contents")
			dir := filepath.Dir(d.cloudConfigPath())
			if _, err := os.Stat(dir); err != nil && os.IsNotExist(err) {
				d.Log.Logger.Error().Err(err).Msg("Destination directory does not exist, creating it")
				if err := os.MkdirAll(dir, 0755); err != nil {
					return err
				}
			}

			return os.WriteFile(d.cloudConfigPath(), []byte(d.Config.CloudConfig), 0600)
		}))
}

func (d *Deployer) StepDumpSource() error {
	// Ops to generate from container image
	d.Log.Logger.Debug().Str("arch", d.Config.Arch).Str("image", d.Artifact.ContainerImage).Msg("StepDumpSource: config arch and image")
	return d.Add(constants.OpDumpSource,
		herd.EnableIf(d.fromImage),
		herd.WithDeps(constants.OpPrepareDirs), herd.WithCallback(ops.DumpSource(d.Artifact.ContainerImage, d.tmpRootFs, d.Config.Arch, d.Config.AllowInsecureRegistriesBool())))
}

func (d *Deployer) StepGenISO() error {
	return d.Add(constants.OpGenISO,
		// Only gen the ISO when something actually needs one: the ISO itself
		// is a requested output, or netboot is requested and has to extract
		// its artifacts out of a generated ISO. Building from a container
		// while the caller asked for only raw/tar/uki/cloud-image outputs
		// used to still spend several minutes (and GBs of disk) generating
		// an ISO nobody would read.
		herd.EnableIf(func() bool {
			if !d.fromImage() || d.rawDiskIsSet() {
				return false
			}
			isoRequested := !d.Config.DisableISOboot
			netbootRequested := !d.Config.DisableNetboot
			return isoRequested || netbootRequested
		}),
		herd.WithDeps(constants.OpDumpSource, constants.OpCopyCloudConfig, constants.OpPrepareDirs), herd.WithCallback(ops.GenISO(d.tmpRootFs, d.destination, d.cloudConfigPath, d.Config.ISO, d.Config.Arch, d.Config.AllowInsecureRegistriesBool())))
}

func (d *Deployer) StepDownloadISO() error {
	return d.Add(constants.OpDownloadISO,
		herd.EnableIf(func() bool { return !d.rawDiskIsSet() && d.isoOption() }),
		herd.WithDeps(constants.OpPrepareDirs),
		herd.WithCallback(ops.DownloadArtifact(d.Artifact.ISOUrl(), d.getIsoFile))) // This is okay to call the getIsoFile function here as we want a destination for the ISO file
}

// StepExtractNetboot Extract netboot artifacts from the ISO file
// Should trigger if netboot is enabled and the ISO is generated or downloaded.
// Add conditional dependencies to ensure the ISO is available before extracting netboot files.
// 2 different conditions, one for the ISO download and one for the ISO generation. The check its the same as the one in StepGenISO/StepDownloadISO
func (d *Deployer) StepExtractNetboot() error {
	return d.Add(constants.OpExtractNetboot,
		herd.EnableIf(func() bool { return !d.Config.DisableNetboot && !d.rawDiskIsSet() }),
		herd.ConditionalOption(func() bool { return d.isoOption() }, herd.WithDeps(constants.OpDownloadISO)),
		herd.ConditionalOption(func() bool { return d.fromImage() }, herd.WithDeps(constants.OpGenISO)),
		herd.WithDeps(constants.OpGenISO), herd.WithCallback(ops.ExtractNetboot(d.getIsoFile, d.dstNetboot, d.netbootPrefix())))
}

// StepGenRawDisk Generate the raw disk image.
// Enabled if is explicitly set the disk.efi or disk.vhd or disk.gce as they depend on the efi disk
func (d *Deployer) StepGenRawDisk() error {
	return d.Add(constants.OpGenEFIRawDisk,
		herd.EnableIf(func() bool {
			return d.Config.Disk.EFI || d.Config.Disk.GCE || d.Config.Disk.VHD || d.Config.Disk.Partitions || d.Config.Disk.MAAS
		}),
		herd.WithDeps(constants.OpDumpSource),
		herd.WithCallback(ops.GenEFIRawDisk(d.rawImageParams())))
}

func (d *Deployer) StepGenMBRRawDisk() error {
	return d.Add(constants.OpGenBIOSRawDisk,
		herd.EnableIf(func() bool { return d.Config.Disk.BIOS }),
		herd.WithDeps(constants.OpDumpSource),
		herd.WithCallback(ops.GenBiosRawDisk(d.rawImageParams())))
}

func (d *Deployer) rawImageParams() ops.RawImageParams {
	return ops.RawImageParams{
		Source:                   d.tmpRootFs(),
		Output:                   d.rawDiskPath(),
		FinalSize:                d.rawDiskSize(),
		StateSize:                d.rawDiskStateSize(),
		RecoveryImageSize:        d.rawDiskRecoveryImageSize(),
		NoDefaultCloudConfig:     d.Config.NoDefaultCloudConfig,
		SeparatePartitionsImages: d.Config.Disk.Partitions,
		MAAS:                     d.Config.Disk.MAAS,
		BootActive:               d.Config.Disk.BootActive,
		StateSlots:               d.rawDiskStateSlots(),
		NoRecovery:               d.Config.Disk.NoRecovery,
		Extensions:               d.diskExtensions(),
	}
}

func (d *Deployer) StepConvertGCE() error {
	return d.Add(constants.OpConvertGCE,
		herd.EnableIf(func() bool { return d.Config.Disk.GCE }),
		herd.WithDeps(constants.OpGenEFIRawDisk),
		herd.WithCallback(ops.ConvertRawDiskToGCE(d.rawDiskPath())))
}

func (d *Deployer) StepConvertVHD() error {
	return d.Add(constants.OpConvertVHD,
		herd.EnableIf(func() bool { return d.Config.Disk.VHD }),
		herd.WithDeps(constants.OpGenEFIRawDisk),
		herd.WithCallback(ops.ConvertRawDiskToVHD(d.rawDiskPath())))
}

// StepConvertMAAS compresses the raw disk into the ddgz format MAAS expects for
// a custom uploaded image, so the artifact is directly uploadable.
func (d *Deployer) StepConvertMAAS() error {
	return d.Add(constants.OpConvertMAAS,
		herd.EnableIf(func() bool { return d.Config.Disk.MAAS }),
		herd.WithDeps(constants.OpGenEFIRawDisk),
		herd.WithCallback(ops.ConvertRawDiskToMAAS(d.rawDiskPath())))
}

func (d *Deployer) StepInjectCC() error {
	return d.Add(constants.OpInjectCC,
		herd.EnableIf(func() bool { return !d.rawDiskIsSet() && d.isoOption() }),
		herd.WithDeps(constants.OpCopyCloudConfig),
		herd.ConditionalOption(d.isoOption, herd.WithDeps(constants.OpDownloadISO)),
		herd.ConditionalOption(d.fromImage, herd.WithDeps(constants.OpGenISO)),
		herd.WithCallback(ops.InjectISO(d.destination, d.getIsoFile, d.Config.ISO)))
}

func (d *Deployer) StepStartHTTPServer() error {
	return d.Add(constants.OpStartHTTPServer,
		herd.Background,
		herd.EnableIf(func() bool { return !d.Config.DisableISOboot && !d.Config.DisableHTTPServer }),
		herd.IfElse(
			d.fromImage(),
			herd.WithDeps(constants.OpGenISO, constants.OpCopyCloudConfig, constants.OpInjectCC),
			herd.WithDeps(constants.OpDownloadISO, constants.OpCopyCloudConfig, constants.OpInjectCC),
		),
		herd.WithCallback(ops.ServeArtifacts(d.listenAddr(), d.destination)),
	)
}

func (d *Deployer) StepStartNetboot() error {
	return d.Add(constants.OpStartNetboot,
		herd.EnableIf(d.netbootOption),
		herd.Background,
		herd.WithDeps(constants.OpExtractNetboot, constants.OpCopyCloudConfig),
		herd.WithCallback(func(ctx context.Context) error {
			// The server threads its netbootmgr.Manager through the context
			// (see internal/cmd/web.go); the CLI never does. When present,
			// route through it so this build-triggered start shares the same
			// state as the dashboard's Start/Stop/Status endpoints -- without
			// this, a build with the netboot output enabled silently starts a
			// server the UI has no way to see or stop, and a later manual
			// "Start Netboot" collides with it on the same port instead of
			// reporting "already running".
			if mgr := netbootmgr.FromContext(ctx); mgr != nil {
				return mgr.StartWithPaths(filepath.Base(d.destination()), d.cloudConfigPath(), d.squashFSfile(), d.initrdFile(), d.kernelFile(), d.netbootGrubCfgFile())
			}
			return ops.StartPixiecore(d.cloudConfigPath(), d.netBootListenAddr(), d.netbootPort(), d.squashFSfile, d.initrdFile, d.kernelFile, d.netbootGrubCfgFile, d.Config.NetBoot)(ctx)
		}),
	)
}

func (d *Deployer) fromImage() bool {
	return d.Artifact.ContainerImage != ""
}

// tmpRootFs is the directory where the container image is unpacked before it is
// turned into an ISO or raw disk. It deliberately lives on the local filesystem
// (os.TempDir(), i.e. /tmp inside the container) rather than under state_dir.
//
// state_dir is frequently a host bind mount, and on Docker Desktop for macOS
// that mount is backed by VirtioFS, which cannot faithfully represent all the
// Linux metadata (ownership + restrictive modes) that containerd's tar-apply
// sets while unpacking a rootfs. Since containerd started chmod'ing files
// through the open descriptor (containerd 61c2733f, first shipped in v1.7.31),
// unpacking a read-only root-owned file such as /etc/sudoers.d/* onto that mount
// fails with "failed to Lchown ... permission denied". Keeping the intermediate
// rootfs on a native filesystem sidesteps the limitation entirely; only the
// finished artifacts (ISO/raw disk) are written to state_dir. See
// https://github.com/kairos-io/kairos/issues/3922.
//
// The directory name is derived from state_dir so that concurrent in-process
// builds (the internal builder gives each build a distinct state_dir and runs
// them in parallel) get distinct unpack directories, exactly as they did when
// temp-rootfs lived under state_dir. Only the location changes here, not the
// per-build uniqueness, so PrepDirs' RemoveAll for one build can never clobber
// another build's rootfs.
//
// When WorkDir is set (build-iso), the rootfs lives in it instead. WorkDir is
// created per run under os.TempDir(), so the same guarantees hold, and two runs
// that share one output directory no longer share an unpack directory.
func (d *Deployer) tmpRootFs() string {
	if d.WorkDir != "" {
		return filepath.Join(d.WorkDir, "rootfs")
	}
	// Hash the normalized state_dir (StateDir cleans the path, so "/output" and
	// "/output/" collapse to the same key) and use the full digest so distinct
	// state_dirs can never collide onto the same unpack directory.
	sum := sha256.Sum256([]byte(d.Config.StateDir()))
	return filepath.Join(os.TempDir(), fmt.Sprintf("auroraboot-temp-rootfs-%x", sum[:]))
}

func (d *Deployer) destination() string {
	return d.Config.State
}

// getIsoFile returns the path to the ISO file.
// It first checks for the default name in the destination directory,
// and if not found, it searches for any ISO file in the destination directory.
func (d *Deployer) getIsoFile() string {
	defaultIsoPath := filepath.Join(d.destination(), "kairos.iso")
	// This is to look for the ISO file in the destination directory
	if _, err := os.Stat(defaultIsoPath); err == nil {
		d.Log.Logger.Info().Str("isoFile", defaultIsoPath).Msg("Found existing ISO file")
		return defaultIsoPath
	}
	// If its not the default name, we search for the ISO file in the destination directory
	files, err := os.ReadDir(d.destination())
	if err != nil {
		d.Log.Logger.Debug().Msg("Failed to read destination directory, falling back to default ISO name")
		return defaultIsoPath // fallback to the default name
	}
	for _, file := range files {
		if file.IsDir() {
			continue
		}
		if filepath.Ext(file.Name()) == ".iso" {
			d.Log.Logger.Info().Str("isoFile", file.Name()).Msg("Found existing ISO file")
			return filepath.Join(d.destination(), file.Name())
		}
	}
	d.Log.Logger.Info().Str("isoFile", defaultIsoPath).Msg("No ISO file found, falling back to default ISO name")
	return defaultIsoPath
}

func (d *Deployer) dstNetboot() string {
	if d.WorkDir != "" {
		return filepath.Join(d.WorkDir, "netboot")
	}
	return d.Config.StateDir("netboot")
}

// Returns true if any of the options for raw disk is set
func (d *Deployer) rawDiskIsSet() bool {
	return d.Config.Disk.VHD || d.Config.Disk.EFI || d.Config.Disk.GCE || d.Config.Disk.BIOS || d.Config.Disk.Partitions || d.Config.Disk.MAAS
}

func (d *Deployer) netbootReleaseOption() bool {
	return !d.Config.DisableNetboot && !d.fromImage()
}

// The three helpers below name the files StepExtractNetboot wrote, so they go
// through the same function the extraction does. They used to hardcode the
// kairos prefix, which is correct only while iso.name is unset.
func (d *Deployer) initrdFile() string {
	_, _, initrd := ops.NetbootArtifactPaths(d.dstNetboot(), d.netbootPrefix())
	return initrd
}

func (d *Deployer) kernelFile() string {
	_, kernel, _ := ops.NetbootArtifactPaths(d.dstNetboot(), d.netbootPrefix())
	return kernel
}

func (d *Deployer) squashFSfile() string {
	squashFS, _, _ := ops.NetbootArtifactPaths(d.dstNetboot(), d.netbootPrefix())
	return squashFS
}

// netbootPrefix is the base name the netboot artifacts carry. It is the only
// input ExtractNetboot and the server that serves its output share, so both
// sides of the pipeline read it from here.
func (d *Deployer) netbootPrefix() string {
	return d.Config.ISO.Name
}

// netbootGrubCfgFile is where ExtractNetboot leaves the livecd grub config. It
// goes through the same function the extraction does, so a build that sets
// iso.name reads back the file that run actually wrote.
func (d *Deployer) netbootGrubCfgFile() string {
	return ops.NetbootGrubCfgPath(d.dstNetboot(), d.netbootPrefix())
}

func (d *Deployer) isoOption() bool {
	return !d.fromImage()
}

func (d *Deployer) cloudConfigPath() string {
	if d.WorkDir != "" {
		return filepath.Join(d.WorkDir, "config.yaml")
	}
	return filepath.Join(d.destination(), "config.yaml")
}

// Return only the path to the output dir, the image name is generated based on the rootfs
func (d *Deployer) rawDiskPath() string {
	return d.destination()
}

func (d *Deployer) diskImgPath() string {
	return filepath.Join(d.destination(), "disk.img")
}

func (d *Deployer) listenAddr() string {
	listenAddr := ":8080"
	if d.Config.ListenAddr != "" {
		listenAddr = d.Config.ListenAddr
	}

	return listenAddr
}

func (d *Deployer) netbootPort() string {
	netbootPort := "8090"
	if d.Config.NetBootHTTPPort != "" {
		netbootPort = d.Config.NetBootHTTPPort
	}

	return netbootPort
}

func (d *Deployer) netBootListenAddr() string {
	address := "0.0.0.0"
	if d.Config.NetBootListenAddr != "" {
		address = d.Config.NetBootListenAddr
	}

	return address
}

func (d *Deployer) netbootOption() bool {
	// squashfs, kernel, and initrd names are tied to the output of /netboot.sh (op.ExtractNetboot)
	return !d.Config.DisableNetboot
}

// diskExtensions is the extension payload a raw-disk build bakes into the
// image. A build names its extensions once, under `iso.extensions`, and the
// artifact type decides where they land: the ISO root for an ISO, the OEM
// partition and then the persistent one here.
func (d *Deployer) diskExtensions() ops.DiskExtensions {
	return ops.DiskExtensions{
		Requests:     d.Config.ISO.Extensions,
		Catalogs:     d.Config.ISO.ExtensionsCatalogs,
		Architecture: d.Config.Arch,
		Insecure:     d.Config.AllowInsecureRegistriesBool(),
	}
}

func (d *Deployer) rawDiskSize() uint64 {
	// parse the string into a uint64
	// the size is in Mb
	if d.Config.Disk.Size == "" {
		return 0
	}
	sizeInt, err := strconv.ParseUint(d.Config.Disk.Size, 10, 64)
	if err != nil {
		d.Log.Logger.Error().Err(err).Str("arg", d.Config.Disk.Size).Msg("Failed to parse disk size, setting value to 0")
		return 0
	}
	return sizeInt
}

func (d *Deployer) rawDiskStateSize() int64 {
	if d.Config.Disk.StateSize == "" {
		return 0
	}
	sizeInt, err := strconv.ParseInt(d.Config.Disk.StateSize, 10, 64)
	if err != nil {
		d.Log.Logger.Error().Err(err).Str("arg", d.Config.Disk.StateSize).Msg("Failed to parse disk state size, setting value to 0")
		return 0
	}
	return sizeInt
}

// rawDiskStateSlots returns disk.state_slots, 0 (the default) when unset or invalid; Validate rejects invalid values first.
func (d *Deployer) rawDiskStateSlots() int {
	slots, err := d.Config.Disk.StateSlotsCount()
	if err != nil {
		d.Log.Logger.Error().Err(err).Msg("Failed to parse disk state slots, using the default")
		return 0
	}
	return slots
}

func (d *Deployer) rawDiskRecoveryImageSize() int64 {
	if d.Config.Disk.RecoveryImageSize == "" {
		return 0
	}
	sizeInt, err := strconv.ParseInt(d.Config.Disk.RecoveryImageSize, 10, 64)
	if err != nil {
		d.Log.Logger.Error().Err(err).Str("arg", d.Config.Disk.RecoveryImageSize).Msg("Failed to parse disk recovery image size, setting value to 0")
		return 0
	}
	return sizeInt
}
