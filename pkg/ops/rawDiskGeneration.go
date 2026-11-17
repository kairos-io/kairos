package ops

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/diskfs/go-diskfs"
	fileBackend "github.com/diskfs/go-diskfs/backend/file"
	"github.com/diskfs/go-diskfs/partition"
	"github.com/diskfs/go-diskfs/partition/gpt"
	"github.com/gofrs/uuid"
	"github.com/kairos-io/AuroraBoot/internal"
	"github.com/kairos-io/AuroraBoot/pkg/constants"
	"github.com/kairos-io/AuroraBoot/pkg/utils"
	"github.com/kairos-io/kairos/v4/agent/pkg/config"
	agentConstants "github.com/kairos-io/kairos/v4/agent/pkg/constants"
	"github.com/kairos-io/kairos/v4/agent/pkg/elemental"
	agentUtils "github.com/kairos-io/kairos/v4/agent/pkg/utils"
	fsutils "github.com/kairos-io/kairos/v4/agent/pkg/utils/fs"
	sdkConstants "github.com/kairos-io/kairos/v4/sdk/constants"
	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
	sdkImage "github.com/kairos-io/kairos/v4/sdk/types/images"
	sdkUtils "github.com/kairos-io/kairos/v4/sdk/utils"
	yipSchema "github.com/mudler/yip/pkg/schema"
	"github.com/twpayne/go-vfs/v5"
	"golang.org/x/sys/unix"
)

// What it does
// Create images for all three partitions
// EFI, OEM, Recovery
// EFI is a fat32 partition with the grub artifacts
// OEM is an ext4 partition with the cloud config
// Recovery is an ext4 partition with the rootfs in an image file called recovery.img under cOS/
// Mount them with loop devices and copy the artifacts to them
// Once the images are created, they are concatenated together with a GPT partition table
// The final image is then truncated to the nearest sector size
// TODO: Add testing

type RawImage struct {
	CloudConfig          string               // cloud config to copy to the oem partition, if none provided a default one will be created with the kairos user
	NoDefaultCloudConfig bool                 // if true, no default cloud config will be created if CloudConfig is not provided
	Source               string               // Source image to copy the artifacts from, which will be the rootfs in the final image
	Output               string               // Output image destination dir. Final image name will be based on the contents of the source /etc/kairos-release file
	FinalSize            uint64               // Final size of the disk image in MB
	StateSize            int64                // Size of the state partition in MB
	RecoveryImageSize    int64                // Size of the recovery image in MB
	tmpDir               string               // A temp dir to do all work on
	elemental            *elemental.Elemental // Elemental instance to use for the operations
	efi                  bool                 // If the image should be EFI or BIOS
	config               *sdkConfig.Config    // config to use for the operations
	// SeparatePartitionsImages, when true, emits each partition as a separate
	// image file (efi.img, oem.img, recovery_partition.img) into Output and
	// skips merging them into a single .raw disk. Used by flashing workflows
	// such as Nvidia Jetson AGX Orin. Only valid together with an EFI build.
	SeparatePartitionsImages bool
	maas                     bool // if true, add the curtin-landing partition (COS_CURTIN) carrying /curtin/curtin-hooks (MAAS deploy)
	// ExtensionFiles are paths to already downloaded .sysext.raw images to bake
	// into the disk. They are staged in the OEM partition and installed onto
	// the persistent one by the first-boot reset, because the disk is assembled
	// here rather than installed, so the agent's install hooks never run.
	ExtensionFiles []string
	// BootActive, when true, adds a COS_STATE partition with active.img so the disk boots straight into active.
	BootActive bool
	// StateSlots is how many system images COS_STATE is sized for on boot-active disks, 0 for the default; 3 (the default) also ships passive.img.
	StateSlots int
	// NoRecovery skips the COS_RECOVERY partition on boot-active disks.
	NoRecovery        bool
	systemImageSizeMB uint // size of recovery.img and active.img
}

// RawImageParams holds the inputs for building a raw disk image.
type RawImageParams struct {
	Source                   string // rootfs dir used for the recovery and active images
	Output                   string // output dir for the final image
	CloudConfig              string // cloud config copied to the OEM partition
	FinalSize                uint64 // final disk size in MB
	StateSize                int64  // state partition size in MB
	RecoveryImageSize        int64  // recovery image size in MB
	NoDefaultCloudConfig     bool
	SeparatePartitionsImages bool
	MAAS                     bool
	BootActive               bool
	StateSlots               int
	NoRecovery               bool
	// Extensions are resolved to ExtensionFiles right before the build.
	Extensions DiskExtensions
}

// NewEFIRawImage creates a new RawImage struct
// config is initialized with a default config to use the standard logger
func NewEFIRawImage(params RawImageParams) *RawImage {
	return newRawImage(params, true)
}

func NewBiosRawImage(params RawImageParams) *RawImage {
	return newRawImage(params, false)
}

func newRawImage(params RawImageParams, efi bool) *RawImage {
	cfg := config.NewConfig(config.WithLogger(internal.Log))
	return &RawImage{
		efi:                      efi,
		config:                   cfg,
		Source:                   params.Source,
		Output:                   params.Output,
		elemental:                elemental.NewElemental(cfg),
		CloudConfig:              params.CloudConfig,
		FinalSize:                params.FinalSize,
		StateSize:                params.StateSize,
		RecoveryImageSize:        params.RecoveryImageSize,
		NoDefaultCloudConfig:     params.NoDefaultCloudConfig,
		SeparatePartitionsImages: params.SeparatePartitionsImages,
		maas:                     params.MAAS,
		BootActive:               params.BootActive,
		StateSlots:               params.StateSlots,
		NoRecovery:               params.NoRecovery,
	}
}

// createOemPartitionImage creates an OEM partition image with the given cloud config
func (r *RawImage) createOemPartitionImage(recoveryImagePath string) (string, error) {
	// Create a temp dir for copying the files to
	tmpDirOem := filepath.Join(r.TempDir(), "oem")
	err := fsutils.MkdirAll(r.config.Fs, tmpDirOem, 0755)
	defer r.config.Fs.RemoveAll(tmpDirOem)

	// This is where the oem partition will be mounted to copy the files to
	tmpDirOemMount := filepath.Join(r.TempDir(), "oem-mount")
	err = fsutils.MkdirAll(r.config.Fs, tmpDirOemMount, 0755)
	defer r.config.Fs.RemoveAll(tmpDirOemMount)

	// Everything the partition carries is written into tmpDirOem first, so the
	// image below is sized against the contents it has to hold.
	stagedExtensions, err := r.stageOemContents(tmpDirOem, recoveryImagePath)
	if err != nil {
		return "", err
	}

	OemPartitionImage := r.oemPartitionImage(tmpDirOem, tmpDirOemMount, stagedExtensions)

	// Deploy the source to the image
	_, err = r.elemental.DeployImageNodirs(&OemPartitionImage, false)
	if err != nil {
		internal.Log.Logger.Error().Err(err).Str("source", r.Source).Interface("image", OemPartitionImage).Msg("failed to create oem image")
		return "", err
	}

	// return the created image file
	return OemPartitionImage.File, nil
}

// stageOemContents writes everything the OEM partition carries into tmpDirOem:
// the cloud config, the grubenv that sends the first boot into recovery, the
// cloud config that expands the disk layout and resets, and the extensions the
// build bundled in. It returns the bytes the extensions take, so the caller
// can size the partition against what has to fit in it.
func (r *RawImage) stageOemContents(tmpDirOem, recoveryImagePath string) (int64, error) {
	// Copy the cloud config to the oem partition if there is any
	ccContent, err := r.config.Fs.ReadFile(r.CloudConfig)
	if err != nil {
		internal.Log.Logger.Error().Err(err).Str("source", r.CloudConfig).Msg("failed to read cloud config")
		return 0, err
	}
	if r.CloudConfig != "" && len(ccContent) > 0 {
		internal.Log.Logger.Debug().Str("source", r.CloudConfig).Str("target", filepath.Join(tmpDirOem, customCloudInitFile)).Str("content", string(ccContent)).Msg("Copying cloud config to oem partition")
		err = fsutils.Copy(r.config.Fs, r.CloudConfig, filepath.Join(tmpDirOem, customCloudInitFile))
		if err != nil {
			internal.Log.Logger.Error().Err(err).Str("source", r.CloudConfig).Str("target", filepath.Join(tmpDirOem, customCloudInitFile)).Msg("failed to copy cloud config")
			return 0, err
		}
	} else if !r.NoDefaultCloudConfig {
		// Create a default cloud config yaml with at least a user
		internal.Log.Logger.Debug().Str("target", filepath.Join(tmpDirOem, customCloudInitFile)).Msg("Creating default cloud config")
		err = r.config.Fs.WriteFile(filepath.Join(tmpDirOem, customCloudInitFile), []byte(constants.DefaultCloudConfig), 0o644)
		if err != nil {
			internal.Log.Logger.Error().Err(err).Str("target", filepath.Join(tmpDirOem, customCloudInitFile)).Msg("failed to write cloud config")
			return 0, err
		}
	}

	firstBootFile, firstBootConfig, err := r.firstBootConfig(recoveryImagePath)
	if err != nil {
		return 0, err
	}
	if !r.BootActive {
		// Set the grubenv to boot into recovery so the first boot can autoreset
		err = agentUtils.SetPersistentVariables(filepath.Join(tmpDirOem, grubOEMEnvFile), map[string]string{grubNextEntryKey: grubRecoveryEntry}, r.config)
		if err != nil {
			return 0, err
		}
	}

	internal.Log.Logger.Debug().Str("target", filepath.Join(tmpDirOem, firstBootFile)).Msg("Creating first boot cloud config")
	err = r.config.Fs.WriteFile(filepath.Join(tmpDirOem, firstBootFile), []byte(firstBootConfig.ToString()), 0o644)
	if err != nil {
		internal.Log.Logger.Error().Err(err).Str("target", filepath.Join(tmpDirOem, firstBootFile)).Msg("failed to write cloud config")
		return 0, err
	}

	// Stage the bundled extensions last, so the partition is sized against
	// everything that has to fit in it.
	stagedExtensions, err := r.stageBundledExtensions(tmpDirOem)
	if err != nil {
		internal.Log.Logger.Error().Err(err).Msg("failed to stage the bundled extensions")
		return 0, err
	}

	return stagedExtensions, nil
}

// firstBootConfig returns the OEM file name and the yip config that finishes the disk layout on first boot.
func (r *RawImage) firstBootConfig(recoveryImagePath string) (string, *yipSchema.YipConfig, error) {
	if r.BootActive {
		return layoutCloudInitFile, bootActiveFirstBootConfig(), nil
	}
	stateSize, err := r.resetStateSizeMB(recoveryImagePath)
	if err != nil {
		return "", nil, err
	}
	return resetCloudInitFile, resetFirstBootConfig(stateSize), nil
}

// resetStateSizeMB returns the configured state size or one derived from the recovery partition image.
func (r *RawImage) resetStateSizeMB(recoveryImagePath string) (int64, error) {
	if r.StateSize > 0 {
		return r.StateSize, nil
	}
	info, err := r.config.Fs.Stat(recoveryImagePath)
	if err != nil {
		internal.Log.Logger.Error().Err(err).Str("source", recoveryImagePath).Msg("failed to stat recovery image")
		return 0, err
	}
	stateSize := (info.Size()*3 + 100*1024*1024) / (1024 * 1024)
	internal.Log.Logger.Debug().Int64("size", stateSize).Msg("calculated state partition size")
	return stateSize, nil
}

const (
	curtinLabel    = "COS_CURTIN"
	curtinPartName = "curtin"
)

// createCurtinLandingPartitionImage builds a tiny ext2 partition that curtin
// will select as its target (it holds /curtin). It carries a static busybox
// (so the chroot needs no libc), stub cloud-init/netplan (so MAAS's in-target
// validation passes), and the MAAS curtin-hook. It is never booted.
func (r *RawImage) createCurtinLandingPartitionImage() (string, error) {
	staging := filepath.Join(r.TempDir(), "curtin-landing")
	mountp := filepath.Join(r.TempDir(), "curtin-landing-mount")

	// static busybox from the auroraboot runtime (added to the image via dnf)
	bb, err := r.config.Fs.ReadFile("/usr/sbin/busybox")
	if err != nil {
		return "", fmt.Errorf("reading busybox (is it dnf-installed in the auroraboot image?): %w", err)
	}
	if err := stageCurtinLanding(staging, bb); err != nil {
		return "", fmt.Errorf("staging curtin-landing directory: %w", err)
	}

	img := sdkImage.Image{
		File:       filepath.Join(r.TempDir(), "curtin-landing.img"),
		FS:         sdkConstants.LinuxImgFs,
		Label:      curtinLabel,
		Size:       64, // MB; busybox ~1.2MB + stubs, 64 matches OEM and is safe
		Source:     sdkImage.NewDirSrc(staging),
		MountPoint: mountp,
	}
	// Use DeployImage (not DeployImageNodirs) so CreateDirStructure creates the
	// /proc /sys /dev /run /tmp mountpoints in the image. curtin's
	// ChrootableTarget bind-mounts those before the in-target validation; empty
	// dirs staged by hand do not survive the deploy, so the dir-creating variant
	// is required here.
	if _, err := r.elemental.DeployImage(&img, false); err != nil {
		return "", fmt.Errorf("creating curtin-landing image: %w", err)
	}
	return img.File, nil
}

// stageCurtinLanding populates dir with the curtin-landing partition contents:
// bin/busybox, bin/sh -> busybox (symlink), usr/bin/cloud-init, usr/bin/netplan,
// curtin/curtin-hooks. It uses the real OS filesystem and is designed to be
// tested without a full image build.
func stageCurtinLanding(dir string, busybox []byte) error {
	// Payload dirs only. The chroot mountpoints (/proc /sys /dev /run /tmp) are
	// created at image-build time by CreateDirStructure via DeployImage (see
	// createCurtinLandingPartitionImage); empty dirs staged here would not
	// survive the deploy.
	for _, d := range []string{"bin", "usr/bin", "curtin"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
			return err
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "bin", "busybox"), busybox, 0o755); err != nil {
		return err
	}
	// sh/ash are real busybox applets, so symlinks work. They stay in /bin
	// because the bash wrapper below references /bin/sh (its shebang) and
	// /bin/busybox.
	for _, link := range []string{"sh", "ash"} {
		if err := os.Symlink("busybox", filepath.Join(dir, "bin", link)); err != nil {
			return err
		}
	}
	// bash is NOT a busybox applet (busybox errors "applet not found" when
	// invoked as bash), so it is a wrapper script that forwards to busybox sh.
	// curtin validates the custom image with `chroot target bash`, resolving
	// bash via PATH. The MAAS ephemeral env is merged-usr (/bin -> /usr/bin) so
	// its PATH is /usr/sbin:/usr/bin:/sbin and omits /bin; the wrapper therefore
	// lives in /usr/bin (where curtin looks), not /bin. Without it, chroot
	// reports "failed to run command 'bash': No such file or directory".
	bashWrapper := []byte("#!/bin/sh\nexec /bin/busybox sh \"$@\"\n")
	if err := os.WriteFile(filepath.Join(dir, "usr/bin", "bash"), bashWrapper, 0o755); err != nil {
		return err
	}
	stub := []byte("#!/bin/sh\nexit 0\n")
	for _, s := range []string{"cloud-init", "netplan"} {
		if err := os.WriteFile(filepath.Join(dir, "usr/bin", s), stub, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(filepath.Join(dir, "curtin", "curtin-hooks"), maasCurtinHook, 0o750)
}

// createRecoveryPartitionImage creates a recovery partition image with the given source
// The source expects to be a directory with the rootfs to generate a squashfs from
// This generates a recovery.img with the rootfs on in under /cOS/
// It also contains the final grub.cfg and grubenv_first
func (r *RawImage) createRecoveryPartitionImage() (string, error) {
	// Create a temp dir for mounting the image to
	tmpDirRecoveryImage := filepath.Join(r.TempDir(), "recovery-img")
	err := fsutils.MkdirAll(r.config.Fs, tmpDirRecoveryImage, 0755)
	if err != nil {
		internal.Log.Logger.Error().Err(err).Str("target", tmpDirRecoveryImage).Msg("failed to create temp dir")
		return "", err
	}
	defer r.config.Fs.RemoveAll(tmpDirRecoveryImage)

	// Create a dir to store the recovery partition contents
	tmpDirRecovery := filepath.Join(r.TempDir(), "recovery")
	err = fsutils.MkdirAll(r.config.Fs, tmpDirRecovery, 0755)
	defer r.config.Fs.RemoveAll(tmpDirRecovery)

	err = fsutils.MkdirAll(r.config.Fs, filepath.Join(tmpDirRecovery, "cOS"), 0755)

	recoveryImage := &sdkImage.Image{
		File:       filepath.Join(tmpDirRecovery, "cOS", agentConstants.RecoveryImgFile),
		FS:         agentConstants.LinuxImgFs,
		Label:      agentConstants.SystemLabel,
		Source:     sdkImage.NewDirSrc(r.Source),
		MountPoint: tmpDirRecoveryImage,
	}
	recoveryImage.Size = r.systemImageSizeMB

	_, err = r.elemental.DeployImage(recoveryImage, false)
	// Create recovery.squash from the rootfs into the recovery partition under cOS/
	if err != nil {
		internal.Log.Logger.Error().Err(err).Str("source", r.Source).Interface("image", recoveryImage).Msg("failed to create recovery image")
		return "", err
	}

	// TODO: Copy the grub artifacts to the recovery partition under grub2/ so they can be used by the EFI/BIOS grub
	// contents come form https://github.com/kairos-io/packages/blob/main/packages/static/grub-config/files/grub.cfg

	// Copy the grub.cfg from the rootfs into the recovery partition
	internal.Log.Logger.Debug().Str("source", r.Source).Str("target", filepath.Join(tmpDirRecovery, filepath.Dir(agentConstants.GrubConf))).Msg("Copying grub.cfg")
	err = fsutils.MkdirAll(r.config.Fs, filepath.Join(tmpDirRecovery, filepath.Dir(agentConstants.GrubConf)), 0755)
	if err != nil {
		internal.Log.Logger.Error().Err(err).Str("target", tmpDirRecovery).Msg("failed to create grub dir")
		return "", err
	}
	_, err = r.config.Fs.Stat(filepath.Join(r.Source, agentConstants.GrubConf))
	if err != nil {
		internal.Log.Logger.Error().Err(err).Str("target", r.Source).Msg("failed to stat grub.cfg")
		return "", err
	}
	grubCfg, err := r.config.Fs.ReadFile(filepath.Join(r.Source, agentConstants.GrubConf))
	if err != nil {
		internal.Log.Logger.Error().Err(err).Str("target", r.Source).Msg("failed to read grub.cfg")
		return "", err
	}
	err = r.config.Fs.WriteFile(filepath.Join(tmpDirRecovery, agentConstants.GrubConf), grubCfg, 0o644)
	if err != nil {
		internal.Log.Logger.Error().Err(err).Str("target", tmpDirRecovery).Msg("failed to write grub.cfg")
		return "", err
	}

	// Now we create an image for the recovery partition
	// We use the dir we created with the image above, which contains the recovery.img and the grub.cfg stuff
	recoverPartitionImage := r.recoveryPartitionImage(tmpDirRecovery, tmpDirRecoveryImage, recoveryImage.Size)

	_, err = r.elemental.DeployImageNodirs(recoverPartitionImage, false)
	// Create recovery.squash from the rootfs into the recovery partition under cOS/
	if err != nil {
		internal.Log.Logger.Error().Err(err).Str("source", r.Source).Interface("image", recoverPartitionImage).Msg("failed to create recovery image")
		return "", err
	}

	return recoverPartitionImage.File, nil

}

// systemImageSize returns the size in MB of the recovery and active images built from the rootfs.
func (r *RawImage) systemImageSize() (uint, error) {
	size, _ := config.GetSourceSize(r.config, sdkImage.NewDirSrc(r.Source))
	imageSize, err := recoveryImageSize(uint64(size), r.RecoveryImageSize)
	if err != nil {
		return 0, err
	}
	if r.RecoveryImageSize > 0 {
		internal.Log.Logger.Info().Int64("size", r.RecoveryImageSize).Msg("Using configured system image size")
	}
	return imageSize, nil
}

func recoveryImageSize(sourceSize uint64, configuredSize int64) (uint, error) {
	if configuredSize > 0 {
		if uint64(configuredSize) < sourceSize {
			return 0, fmt.Errorf("configured recovery image size %d MB is smaller than source size %d MB", configuredSize, sourceSize)
		}
		return uint(configuredSize), nil
	}
	return uint(sourceSize + 200), nil
}

func recoveryPartitionSize(recoveryImageSize uint) uint {
	// The partition must hold both the recovery image and a transition image
	// during recovery upgrades, plus space for copied boot artifacts.
	return recoveryImageSize*2 + 150
}

// oemPartitionImage describes the OEM partition image of a raw disk.
//
// OEM is a partition, not one of the active/passive/recovery image files, so
// it takes the filesystem a regular install gives it (LinuxFs, ext4) and not
// the journal-less one those image files use (LinuxImgFs, ext2). /oem is
// written on every install and by the cloud config stages, so an unclean
// shutdown has to be replayable.
//
// History: #233 moved every raw partition from ext4 to ext2 in March 2025,
// because old GRUB builds died on ext4 feature flags rather than ignoring
// them. That reason is real, and OEM and recovery are the two partitions it
// applies to: the grub.cfg on the ESP finds its real config by COS_RECOVERY
// label and chainloads it, and that config loads grubenv from COS_OEM, so GRUB
// parses both filesystems before a kernel exists. mkfs runs as
// "mkfs.ext4 -L <label> <dev>" with no -O, so the feature set is whatever the
// surrounding e2fsprogs defaults to, and the raw builder's is not the
// installer's. The raw-bootable e2e test boots the built image on BIOS and
// EFI, which is what keeps this honest.
func (r *RawImage) oemPartitionImage(stagingDir, mountPoint string, stagedExtensions int64) sdkImage.Image {
	return sdkImage.Image{
		File:       filepath.Join(r.TempDir(), "oem.img"),
		FS:         sdkConstants.LinuxFs,
		Label:      sdkConstants.OEMLabel,
		Size:       oemPartitionSize(stagedExtensions),
		Source:     sdkImage.NewDirSrc(stagingDir),
		MountPoint: mountPoint,
	}
}

// recoveryPartitionImage describes the recovery partition image of a raw disk.
//
// Like OEM this is a partition, so it takes LinuxFs. The recovery.img it
// carries keeps LinuxImgFs, which is what that constant is for.
func (r *RawImage) recoveryPartitionImage(stagingDir, mountPoint string, recoveryImgSize uint) *sdkImage.Image {
	return &sdkImage.Image{
		File:       filepath.Join(r.TempDir(), "recovery.img"),
		FS:         sdkConstants.LinuxFs,
		Label:      agentConstants.RecoveryLabel,
		Size:       recoveryPartitionSize(recoveryImgSize),
		Source:     sdkImage.NewDirSrc(stagingDir),
		MountPoint: mountPoint,
	}
}

// createEFIPartitionImage creates an EFI partition image with the given source
func (r *RawImage) createEFIPartitionImage() (string, error) {
	// Create a temp dir for copying the files to
	tmpDirEfi := filepath.Join(r.TempDir(), "efi")
	err := fsutils.MkdirAll(r.config.Fs, tmpDirEfi, 0755)
	if err != nil {
		internal.Log.Logger.Error().Err(err).Str("target", tmpDirEfi).Msg("failed to create temp dir")
		return "", err
	}
	defer r.config.Fs.RemoveAll(tmpDirEfi)

	// This is where the oem partition will be mounted to copy the files to
	tmpDirEfiMount := filepath.Join(r.TempDir(), "efi-mount")
	err = fsutils.MkdirAll(r.config.Fs, tmpDirEfiMount, 0755)
	if err != nil {
		internal.Log.Logger.Error().Err(err).Str("target", tmpDirEfiMount).Msg("failed to create temp dir")
		return "", err
	}
	defer r.config.Fs.RemoveAll(tmpDirEfiMount)

	// Go over the grub folder and copy everything to the image making dirs as needed and going deeper into them
	// Copy the grub.cfg
	// Create dirs as needed
	err = fsutils.MkdirAll(r.config.Fs, filepath.Join(tmpDirEfi, "EFI", "BOOT"), 0755)
	if err != nil {
		internal.Log.Logger.Error().Err(err).Str("target", tmpDirEfi).Msg("failed to create boot dir")
		return "", err
	}

	model, flavor, err := r.GetModelAndFlavor()

	if err != nil {
		internal.Log.Logger.Error().Err(err).Msg("failed to get flavor or model")
		return "", err
	}

	efiGrubCfg := r.efiGrubCfg()
	if strings.Contains(flavor, "ubuntu") {
		err = fsutils.MkdirAll(r.config.Fs, filepath.Join(tmpDirEfi, "EFI", "ubuntu"), 0755)
		if err != nil {
			internal.Log.Logger.Error().Err(err).Str("target", tmpDirEfi).Msg("failed to create ubuntu dir")
			return "", err
		}
		err = r.config.Fs.WriteFile(filepath.Join(tmpDirEfi, "EFI", "ubuntu", "grub.cfg"), []byte(efiGrubCfg), 0o644)
		if err != nil {
			internal.Log.Logger.Error().Err(err).Str("target", tmpDirEfi).Msg("failed to write grub.cfg")
			return "", err
		}
	} else {
		err = r.config.Fs.WriteFile(filepath.Join(tmpDirEfi, "EFI", "BOOT", "grub.cfg"), []byte(efiGrubCfg), 0o644)
		if err != nil {
			internal.Log.Logger.Error().Err(err).Str("target", tmpDirEfi).Msg("failed to write grub.cfg")
			return "", err
		}
	}

	// Now search for the grubARCH.efi and copy it to the efi partition from the rootfs
	err = r.copyShimOrGrub(tmpDirEfi, "shim")
	if err != nil {
		internal.Log.Logger.Error().Err(err).Msg("failed to copy shim")
		return "", err
	}

	err = r.copyShimOrGrub(tmpDirEfi, "grub")
	if err != nil {
		internal.Log.Logger.Error().Err(err).Msg("failed to copy grub")
		return "", err
	}

	// Copy RPI firmware, should be valid for rpi3,4,5 and beyond if the rpi firmware files are updated
	if strings.HasPrefix(model, "rpi") {
		err = copyFirmwareRpi(tmpDirEfi)
		if err != nil {
			internal.Log.Logger.Error().Err(err).Msg("failed to copy rpi firmware")
			return "", err
		}
	}

	efiPartitionImage := sdkImage.Image{
		File:       filepath.Join(r.TempDir(), "efi.img"),
		FS:         sdkConstants.EfiFs,
		Label:      sdkConstants.EfiLabel,
		Size:       sdkConstants.EfiSize,
		Source:     sdkImage.NewDirSrc(tmpDirEfi),
		MountPoint: tmpDirEfiMount,
	}

	// Deploy the source to the image
	_, err = r.elemental.DeployImageNodirs(&efiPartitionImage, false)
	if err != nil {
		internal.Log.Logger.Error().Err(err).Str("source", r.Source).Interface("image", efiPartitionImage).Msg("failed to create efi image")
		return "", err
	}

	return efiPartitionImage.File, nil
}

// efiGrubCfg returns the EFI grub.cfg that chainloads either the state or the recovery grub config.
func (r *RawImage) efiGrubCfg() string {
	if r.BootActive {
		return constants.GrubEfiState
	}
	return constants.GrubEfiRecovery
}

// createBiosPartitionImage creates a BIOS partition image
// This is an empty image as grub-install will then write the core.img contents to it directly so nothing special.
func (r *RawImage) createBiosPartitionImage() (string, error) {
	f, err := r.config.Fs.Create(filepath.Join(r.TempDir(), "bios.img"))
	if err != nil {
		internal.Log.Logger.Error().Err(err).Str("target", filepath.Join(r.TempDir(), "bios.img")).Msg("failed to create bios image")
		return "", err
	}
	err = f.Truncate(int64(sdkConstants.BiosSize * 1024 * 1024))
	if err != nil {
		internal.Log.Logger.Error().Err(err).Str("target", filepath.Join(r.TempDir(), "bios.img")).Msg("failed to truncate bios image")
		return "", err
	}
	err = f.Close()
	if err != nil {
		internal.Log.Logger.Error().Err(err).Str("target", filepath.Join(r.TempDir(), "bios.img")).Msg("failed to close bios image")
		return "", err
	}

	return filepath.Join(r.TempDir(), "bios.img"), err
}

func (r *RawImage) TempDir() string {
	if r.tmpDir == "" {
		r.tmpDir, _ = fsutils.TempDir(r.config.Fs, "", "auroraboot-raw-image-")
	}
	return r.tmpDir
}

func (r *RawImage) Build() error {
	var bootImagePath string
	var err error

	defer r.config.Fs.RemoveAll(r.TempDir())

	// Get the artifact version from the rootfs
	outputName := fmt.Sprintf("%s-%s.raw", constants.KairosDefaultArtifactName, utils.NameFromRootfs(r.Source))
	internal.Log.Logger.Debug().Str("name", outputName).Msg("Got output name")

	if r.NoRecovery && !r.BootActive {
		return fmt.Errorf("skipping the recovery partition requires a boot-active disk")
	}
	r.systemImageSizeMB, err = r.systemImageSize()
	if err != nil {
		internal.Log.Logger.Error().Err(err).Msg("failed to calculate system image size")
		return err
	}

	var recoveryImagePath string
	if r.NoRecovery {
		internal.Log.Logger.Info().Msg("Skipping RECOVERY image")
	} else {
		internal.Log.Logger.Info().Msg("Creating RECOVERY image")
		recoveryImagePath, err = r.createRecoveryPartitionImage()
		if err != nil {
			internal.Log.Logger.Error().Err(err).Msg("failed to create recovery partition")
			return err
		}
		defer r.config.Fs.Remove(recoveryImagePath)
		internal.Log.Logger.Info().Msg("Created RECOVERY image")
	}

	internal.Log.Logger.Info().Msg("Creating BOOT image")
	if r.efi {
		// Create the EFI partition
		bootImagePath, err = r.createEFIPartitionImage()
		if err != nil {
			internal.Log.Logger.Error().Err(err).Msg("failed to create efi partition")
			return err
		}
		defer r.config.Fs.Remove(bootImagePath)
	} else {
		// Create the BIOS partition AFTER the recovery partition, as it needs the recovery image to install grub
		bootImagePath, err = r.createBiosPartitionImage()
		if err != nil {
			internal.Log.Logger.Error().Err(err).Msg("failed to create bios partition")
			return err
		}

		defer r.config.Fs.Remove(bootImagePath)
	}
	internal.Log.Logger.Info().Msg("Created BOOT image")

	// Oem after recovery, as it needs the recovery image to calculate the size of the state partition
	internal.Log.Logger.Info().Msg("Creating OEM image")
	// Create the OEM partition
	oemImagePath, err := r.createOemPartitionImage(recoveryImagePath)
	if err != nil {
		internal.Log.Logger.Error().Err(err).Msg("failed to create oem partition")
		return err
	}
	defer r.config.Fs.Remove(oemImagePath)
	internal.Log.Logger.Info().Msg("Created OEM image")

	// Partition-image mode: emit each partition as its own file and skip the
	// merge/truncate/finalize steps. The flashing workflow (e.g. Nvidia Jetson
	// AGX Orin) consumes the individual images, and state/persistent are created
	// on first boot by the reset/expand cloud-init baked into the OEM partition.
	if r.SeparatePartitionsImages {
		internal.Log.Logger.Info().Str("target", r.Output).Msg("Emitting individual partition images")
		if err = r.emitPartitionImages(bootImagePath, oemImagePath, recoveryImagePath); err != nil {
			internal.Log.Logger.Error().Err(err).Msg("failed to emit partition images")
			return err
		}
		internal.Log.Logger.Info().Str("target", r.Output).Msg("Emitted individual partition images")
		return nil
	}

	// State goes last so recovery keeps its index and persistent is appended after state on first boot
	var stateImagePath string
	if r.BootActive {
		internal.Log.Logger.Info().Msg("Creating STATE image")
		stateImagePath, err = r.createStatePartitionImage()
		if err != nil {
			internal.Log.Logger.Error().Err(err).Msg("failed to create state partition")
			return err
		}
		defer r.config.Fs.Remove(stateImagePath)
		internal.Log.Logger.Info().Msg("Created STATE image")
	}
	parts := diskParts(oemImagePath, recoveryImagePath, stateImagePath)
	if r.maas {
		landing, err := r.createCurtinLandingPartitionImage()
		if err != nil {
			internal.Log.Logger.Error().Err(err).Msg("failed to create curtin-landing partition")
			return err
		}
		defer r.config.Fs.Remove(landing)
		parts = append([]diskPart{{img: landing, name: curtinPartName, guidLabel: curtinLabel}}, parts...)
	}

	// Create the final disk image
	internal.Log.Logger.Info().Str("target", filepath.Join(r.Output, outputName)).Msg("Assembling final disk image")
	err = r.createDiskImage(filepath.Join(r.Output, outputName), bootImagePath, parts)
	if err != nil {
		internal.Log.Logger.Error().Err(err).Msg("failed to create disk image")
		return err
	}
	if err = r.finishDiskImage(filepath.Join(r.Output, outputName)); err != nil {
		return err
	}
	internal.Log.Logger.Info().Str("target", filepath.Join(r.Output, outputName)).Msg("Assembled final disk image")

	return nil
}

// finishDiskImage grows the assembled image to FinalSize and runs the board
// specific FinalizeImage step. A failure here, such as a missing bootloader
// blob, fails the build instead of leaving an image that cannot boot.
func (r *RawImage) finishDiskImage(image string) error {
	info, err := r.config.Fs.Stat(image)
	if err != nil {
		internal.Log.Logger.Error().Err(err).Msg("failed to stat final image")
		return err
	}
	// truncate the image to desired size
	if r.FinalSize > 0 && uint64(info.Size()) < r.FinalSize*1024*1024 {
		internal.Log.Logger.Info().Int64("size", info.Size()).Msg("Truncating final image")
		err = os.Truncate(image, int64(r.FinalSize*1024*1024))
		if err != nil {
			internal.Log.Logger.Error().Err(err).Msg("failed to truncate final image")
			return err
		}

	}

	// Do some final adjustments for boards
	return r.FinalizeImage(image)
}

// emitPartitionImages copies the already-built partition images out of the temp
// dir into the output directory, naming them to match the existing Nvidia Orin
// flashing convention (efi.img, oem.img, recovery_partition.img). It is used in
// partitions mode instead of merging the partitions into a single .raw disk.
func (r *RawImage) emitPartitionImages(bootImagePath, oemImagePath, recoveryImagePath string) error {
	outputs := []struct {
		src, dst string
	}{
		{bootImagePath, filepath.Join(r.Output, "efi.img")},
		{oemImagePath, filepath.Join(r.Output, "oem.img")},
		{recoveryImagePath, filepath.Join(r.Output, "recovery_partition.img")},
	}

	for _, o := range outputs {
		if _, err := r.config.Fs.Stat(o.src); err != nil {
			internal.Log.Logger.Error().Err(err).Str("source", o.src).Msg("partition image not found")
			return fmt.Errorf("partition image %s not found: %w", o.src, err)
		}
		internal.Log.Logger.Debug().Str("source", o.src).Str("target", o.dst).Msg("Copying partition image")
		if err := utils.CopyFile(r.config.Fs, o.src, o.dst); err != nil {
			return fmt.Errorf("copying partition image to %s: %w", o.dst, err)
		}
		// Make the image usable outside the build container, which runs as root.
		if err := r.config.Fs.Chmod(o.dst, 0o777); err != nil {
			return fmt.Errorf("chmod partition image %s: %w", o.dst, err)
		}
	}

	return nil
}

// diskPart is a partition image placed after the boot partition in the final disk.
type diskPart struct {
	img       string
	name      string
	guidLabel string
}

// diskParts returns the partitions after the boot partition in disk order, leaving out images that were not built.
func diskParts(oemImagePath, recoveryImagePath, stateImagePath string) []diskPart {
	candidates := []diskPart{
		{img: oemImagePath, name: sdkConstants.OEMPartName, guidLabel: sdkConstants.OEMLabel},
		{img: recoveryImagePath, name: agentConstants.RecoveryImgName, guidLabel: sdkConstants.RecoveryLabel},
		{img: stateImagePath, name: sdkConstants.StatePartName, guidLabel: sdkConstants.StateLabel},
	}
	parts := make([]diskPart, 0, len(candidates))
	for _, p := range candidates {
		if p.img == "" {
			continue
		}
		parts = append(parts, p)
	}
	return parts
}

// createDiskImage creates the final image by truncating the image with the proper size and
// concatenating the contents of the given partitions.
func (r *RawImage) createDiskImage(rawDiskFile, bootImg string, extraParts []diskPart) error {
	var initDiskFile, endDiskFile string
	var err error
	var partFiles []string
	var size uint64
	var table partition.Table
	var parts []*gpt.Partition

	internal.Log.Logger.Debug().Str("disk", rawDiskFile).Str("boot", bootImg).Interface("parts", extraParts).Msg("Creating disk image")

	// Create disk image, 1Mb for alignment and GPT header, 2MB for bios boot partition
	// Then concat all partition images
	// Then add 1MB of free space at the end of the disk for gpt backup headers

	// Create the start and end images
	initDiskFile = filepath.Join(r.TempDir(), "init.raw")
	endDiskFile = filepath.Join(r.TempDir(), "end.raw")
	// THIS SIZE MARKS THE START SECTOR FOR THE PARTITIONS BELOW!
	// So this mean we have an empty 2048 sectors at the start of the disk, partitions then start at that point
	init, err := fileBackend.CreateFromPath(filepath.Join(r.TempDir(), "init.raw"), 1*1024*1024)
	if err != nil {
		internal.Log.Logger.Error().Err(err).Str("target", initDiskFile).Msg("failed to create init disk")
		return err
	}
	end, err := fileBackend.CreateFromPath(filepath.Join(r.TempDir(), "end.raw"), 1*1024*1024)
	if err != nil {
		internal.Log.Logger.Error().Err(err).Str("target", endDiskFile).Msg("failed to create end disk")
		return err
	}
	err = init.Close()
	if err != nil {
		internal.Log.Logger.Error().Err(err).Str("target", initDiskFile).Msg("failed to close init disk")
		return err
	}
	err = end.Close()
	if err != nil {
		internal.Log.Logger.Error().Err(err).Str("target", endDiskFile).Msg("failed to close end disk")
		return err
	}

	// List and concatenate all image files
	partFiles = append(partFiles, initDiskFile, bootImg)
	for _, p := range extraParts {
		partFiles = append(partFiles, p.img)
	}
	partFiles = append(partFiles, endDiskFile)
	err = utils.ConcatFiles(vfs.OSFS, partFiles, rawDiskFile)
	if err != nil {
		return err
	}

	// Add the partition table
	finalDisk, err := diskfs.Open(rawDiskFile)
	if err != nil {
		internal.Log.Logger.Error().Err(err).Str("target", rawDiskFile).Msg("failed to open final disk")
		return err
	}
	defer finalDisk.Close()

	// Truncate file to multiple of sector size
	stat, _ := os.Stat(rawDiskFile)
	size = roundToNearestSector(stat.Size(), finalDisk.LogicalBlocksize)
	err = os.Truncate(rawDiskFile, int64(size))
	if err != nil {
		internal.Log.Logger.Error().Err(err).Str("target", rawDiskFile).Msg("failed to truncate final disk")
		return err
	}

	// Create a GPT partition table
	// Bit 0: System partition This indicates the system that this is a required partition and to not mess with it.
	// Bit 2: Legacy BIOS bootable This indicates that this partition is bootable by legacy BIOS.
	if r.efi {
		// EFI
		stat, err = os.Stat(bootImg)
		if err != nil {
			internal.Log.Logger.Error().Err(err).Str("target", bootImg).Msg("failed to stat efi partition")
			return err
		}
		size = roundToNearestSector(stat.Size(), finalDisk.LogicalBlocksize)
		parts = append(parts, &gpt.Partition{
			Index:      1,
			Start:      2048,
			End:        getSectorEndFromSize(2048, size, finalDisk.LogicalBlocksize),
			Type:       gpt.EFISystemPartition,
			Size:       size,
			Name:       sdkConstants.EfiPartName,
			GUID:       uuid.NewV5(uuid.NamespaceURL, sdkConstants.EfiLabel).String(),
			Attributes: 1 << 0, // Sets bit 0
		})
	} else {
		size = roundToNearestSector(int64(sdkConstants.BiosSize*1024*1024), finalDisk.LogicalBlocksize)
		parts = append(parts, &gpt.Partition{
			Index:      1,
			Start:      2048,
			End:        getSectorEndFromSize(2048, size, finalDisk.LogicalBlocksize),
			Type:       gpt.BIOSBoot,
			Size:       size,
			Name:       sdkConstants.BiosPartName,
			GUID:       uuid.NewV5(uuid.NamespaceURL, sdkConstants.EfiLabel).String(), // Same name as EFI, COS_GRUB usually
			Attributes: (1 << 0) | (1 << 2),                                           // Sets bits 0 and 2
		})
	}

	idx := 2
	for _, p := range extraParts {
		st, err := os.Stat(p.img)
		if err != nil {
			internal.Log.Logger.Error().Err(err).Str("target", p.img).Msg("failed to stat partition")
			return err
		}
		psize := roundToNearestSector(st.Size(), finalDisk.LogicalBlocksize)
		start := parts[len(parts)-1].End + 1
		parts = append(parts, &gpt.Partition{
			Index: idx,
			Start: start,
			End:   getSectorEndFromSize(start, psize, finalDisk.LogicalBlocksize),
			Type:  gpt.LinuxFilesystem,
			Size:  psize,
			Name:  p.name,
			GUID:  uuid.NewV5(uuid.NamespaceURL, p.guidLabel).String(),
		})
		idx++
	}

	table = &gpt.Table{
		ProtectiveMBR:      true,
		GUID:               agentConstants.DiskUUID, // Set know predictable UUID
		Partitions:         parts,
		LogicalSectorSize:  int(finalDisk.LogicalBlocksize),
		PhysicalSectorSize: int(finalDisk.PhysicalBlocksize),
	}
	err = finalDisk.Partition(table)
	if err != nil {
		internal.Log.Logger.Error().Err(err).Str("target", rawDiskFile).Msg("failed to partition final disk")
		return err
	}

	// If its not efi, we need to install grub to the disk device directly
	if !r.efi {
		err = r.installGrubToDisk(rawDiskFile)
		if err != nil {
			internal.Log.Logger.Error().Err(err).Str("target", rawDiskFile).Msg("failed to install grub to final disk")
			return err
		}
	}

	internal.Log.Logger.Info().Str("disk", rawDiskFile).Msg("Created disk image")

	return nil
}

// Helper function to calculate the end sector for a given start and size based on the sector size
func getSectorEndFromSize(start, size uint64, sectorSize int64) uint64 {
	return (size / uint64(sectorSize)) + start - 1
}

// Helper function to round size to the nearest multiple of the sector size
func roundToNearestSector(size, sector int64) uint64 {
	if size%sector == 0 {
		return uint64(size)
	}
	return uint64(size + sector - (size % sector))
}

// copyShim copies the shim/grub file to the EFI partition
// It searches for the shim/grub file in the rootfs and copies it to the EFI partition given in target
func (r *RawImage) copyShimOrGrub(target, which string) error {
	var searchFiles []string
	var copyDone bool
	var arch string

	arch, err := utils.GetArchFromRootfs(r.Source, r.config.Logger)
	if err != nil {
		return err
	}
	if arch == "" {
		return fmt.Errorf("no arch found in rootfs")
	}

	if which == "shim" {
		searchFiles = sdkUtils.GetEfiShimFiles(arch)
	} else if which == "grub" {
		searchFiles = sdkUtils.GetEfiGrubFiles(arch)
	} else {
		return fmt.Errorf("invalid which value: %s", which)
	}

	// Alpine does not provide a shim nor a grub file, so we need to copy the shim from the docker image that we ship as fallback artifact
	if _, flavor, err := r.GetModelAndFlavor(); flavor == "alpine" && err == nil {
		return r.CopyAlpineShimAndGrub(arch, target)
	}

	// Some architectures have no shim/secure-boot convention at all yet --
	// kairos-sdk's GetEfiShimFiles returns an empty list for them (riscv64
	// today). That is not a missing file, it is the architecture having
	// nothing to chain from, so treat it as a no-op rather than a failure.
	// The grub copy below writes to the removable-media fallback path
	// instead, since grub itself is what firmware boots directly there.
	noShimForArch := len(sdkUtils.GetEfiShimFiles(arch)) == 0
	if which == "shim" && noShimForArch {
		return nil
	}
	writeFallback := which == "shim" || (which == "grub" && noShimForArch)

	for _, f := range searchFiles {
		_, err := r.config.Fs.Stat(filepath.Join(r.Source, f))
		if err != nil {
			r.config.Logger.Debugf("skip copying %s: not found", filepath.Join(r.Source, f))
			continue
		}
		_, name := filepath.Split(f)
		// remove the .signed suffix if present
		name = strings.TrimSuffix(name, ".signed")
		// remove the .dualsigned suffix if present
		name = strings.TrimSuffix(name, ".dualsigned")
		fileWriteName := filepath.Join(target, constants.EfiBootPath, name)
		r.config.Logger.Debugf("Copying %s to %s", f, fileWriteName)

		// Try to find the paths give until we succeed
		fileContent, err := r.config.Fs.ReadFile(filepath.Join(r.Source, f))

		if err != nil {
			r.config.Logger.Warnf("error reading %s: %s", filepath.Join(r.Source, f), err)
			continue
		}
		err = r.config.Fs.WriteFile(fileWriteName, fileContent, agentConstants.FilePerm)
		if err != nil {
			return fmt.Errorf("error writing %s: %s", fileWriteName, err)
		}
		copyDone = true

		// Copy the shim (or, on a no-shim architecture, grub) content to
		// the fallback name so the system boots from fallback. This means
		// that we do not create any bootloader entries, so our recent
		// installation has the lower priority if something else is on the
		// bootloader.
		if writeFallback {
			writeShim := agentConstants.GetFallBackEfi(arch)
			err = r.config.Fs.WriteFile(filepath.Join(target, constants.EfiBootPath, writeShim), fileContent, agentConstants.FilePerm)
			if err != nil {
				return fmt.Errorf("could not write file %s at dir %s", writeShim, target)
			}
		}
		break
	}
	if !copyDone {
		r.config.Logger.Debugf("List of files searched for: %s", searchFiles)
		return fmt.Errorf("could not find any %s file to copy", which)
	}
	return nil
}

func (r *RawImage) installGrubToDisk(image string) error {
	internal.Log.Logger.Debug().Str("backingFile", image).Msg("Attaching file to loop device")
	// Create a dir to store the recovery partition contents
	tmpDirRecovery := filepath.Join(r.TempDir(), "recovery")
	err := fsutils.MkdirAll(r.config.Fs, tmpDirRecovery, 0755)
	defer r.config.Fs.RemoveAll(tmpDirRecovery)

	// TODO: move this to a function
	// TODO hardcore edition: Make this working pure golang
	// doable but complex:
	// mount device into a loop device via syscalls
	// refresh the partition table
	// create the disk nodes to map to the partitions
	// then do it in reverse to cleanup
	// I tried but the devices didnt appear properly dur to it being in a container
	// so I went with the easy way
	// Attach the image to the next free loop device. We deliberately do NOT run
	// `losetup -D` here: that detaches EVERY loop device on the host, which on a
	// shared CI runner or the fleet `web` server would rip loop devices out from
	// under concurrent builds, active mounts, and the host OS itself. `-f --show`
	// allocates a single free device, and the deferred cleanup below detaches
	// exactly that one.
	loopDevice, err := exec.Command("losetup", "-f", "--show", image).CombinedOutput()
	internal.Log.Logger.Debug().Str("output", string(loopDevice)).Msg("Attaching image to loop device")
	if err != nil {
		return fmt.Errorf("failed to attach file to loop device: %w", err)
	}

	// clean loop device, trim spaces and such
	loopDevice = bytes.TrimSpace(loopDevice)

	// Run kpartx
	out, err := exec.Command("kpartx", "-av", string(loopDevice)).CombinedOutput()
	internal.Log.Logger.Debug().Str("output", string(out)).Msg("Running kpartx")
	if err != nil {
		internal.Log.Logger.Error().Str("output", string(out)).Msg("kpartx output")
		return fmt.Errorf("failed to run kpartx: %w", err)
	}

	defer func() {
		// TODO: move this to a function
		out, err := exec.Command("kpartx", "-dv", string(loopDevice)).CombinedOutput()
		if err != nil {
			internal.Log.Logger.Error().Err(err).Str("device", string(loopDevice)).Str("out", string(out)).Msg("failed to detach loop device")
			return
		}
		out, err = exec.Command("losetup", "-d", string(loopDevice)).CombinedOutput()
		if err != nil {
			internal.Log.Logger.Error().Err(err).Str("device", string(loopDevice)).Str("out", string(out)).Msg("failed to detach loop device")
			return
		}
	}()

	internal.Log.Logger.Debug().Str("device", string(loopDevice)).Str("image", image).Msg("Attached image to loop device")

	// While grub is installed to disk + bios_boot partition, it still needs to have the config and mod files available
	// so, the grub files are stored in the recovery partition
	// Get only the loop device without the /dev/ prefix
	cleanLoopDevice := string(loopDevice)[5:]
	recoveryLoop := fmt.Sprintf("/dev/mapper/%s%s", cleanLoopDevice, "p3")
	// The type passed here has to be the one the partition was created with:
	// an ext4 filesystem has incompatible features the ext2 driver refuses, so
	// mounting it as ext2 fails outright rather than falling back.
	err = unix.Mount(recoveryLoop, tmpDirRecovery, sdkConstants.LinuxFs, 0, "")
	if err != nil {
		internal.Log.Logger.Error().Err(err).Str("device", recoveryLoop).Str("mountpoint", tmpDirRecovery).Msg("failed to mount recovery partition")
		return err
	}

	defer unix.Unmount(tmpDirRecovery, 0)

	// Unfortunately this is the only way to install grub to a disk
	// I tried to manually do it but grub does way too much stuff
	// If someone is braver than me, here are some tips.
	// boot.img goes to the first sector of the disk, sector 0 (512 bytes)
	// core.img goes to the partition identified by bios_grub flag
	// boot.img needs to have the LBA where core.img is located
	// No idea where is that hardcoded
	// core.img is not provided by grub packages, it has to be generated beforehand
	args := []string{
		"--target=i386-pc",
		"--force", string(loopDevice),
		fmt.Sprintf("--boot-directory=%s", tmpDirRecovery),
	}
	internal.Log.Logger.Debug().Strs("args", args).Msg("Running grub2-install")
	out, err = exec.Command("grub2-install", args...).CombinedOutput()
	if err != nil {
		internal.Log.Logger.Error().Err(err).Str("device", string(loopDevice)).Str("output", string(out)).Msg("failed to install grub")
		return err
	}
	// Copy recovery grub.cfg to /mnt/recovery/grub2/grub.cfg so its picked up by the grub which then chainloads the rest
	err = r.config.Fs.WriteFile(filepath.Join(tmpDirRecovery, "grub2", "grub.cfg"), []byte(constants.GrubEfiRecovery), 0o644)
	if err != nil {
		internal.Log.Logger.Error().Err(err).Str("device", string(loopDevice)).Msg("failed to write grub.cfg")
		return err
	}

	return nil
}

// GetModelAndFlavor returns the model and flavor of the source rootfs
func (r *RawImage) GetModelAndFlavor() (string, string, error) {
	var flavor string
	var model string
	var err error

	if _, ok := r.config.Fs.Stat(filepath.Join(r.Source, "etc/kairos-release")); ok == nil {
		flavor, err = sdkUtils.OSRelease("FLAVOR", filepath.Join(r.Source, "etc/kairos-release"))
		if err != nil {
			internal.Log.Logger.Error().Err(err).Msg("failed to get flavor")
			return "", "", err
		}
		model, err = sdkUtils.OSRelease("MODEL", filepath.Join(r.Source, "etc/kairos-release"))
		if err != nil {
			internal.Log.Logger.Error().Err(err).Msg("failed to get model")
			return "", "", err
		}
	} else {
		// Fallback to /etc/os-release for older images
		flavor, err = sdkUtils.OSRelease("FLAVOR", filepath.Join(r.Source, "etc/os-release"))
		if err != nil {
			internal.Log.Logger.Error().Err(err).Msg("failed to get flavor")
			return "", "", err
		}
		model, err = sdkUtils.OSRelease("MODEL", filepath.Join(r.Source, "etc/os-release"))
		if err != nil {
			internal.Log.Logger.Error().Err(err).Msg("failed to get model")
			return "", "", err
		}
	}

	if flavor == "" || model == "" {
		internal.Log.Logger.Error().Msg("failed to get flavor or model")
		return "", "", fmt.Errorf("failed to get flavor or model")
	}

	internal.Log.Logger.Debug().Str("flavor", flavor).Msg("got flavor")
	internal.Log.Logger.Debug().Str("model", model).Msg("got model")

	return model, flavor, nil
}

// FinalizeImage does some final adjustments to the image
func (r *RawImage) FinalizeImage(image string) error {
	var err error

	// Get the model
	model, _, err := r.GetModelAndFlavor()
	if err != nil {
		internal.Log.Logger.Error().Err(err).Msg("failed to get flavor or model")
		return err
	}

	// Do board specific stuff
	switch model {
	case "rpi4", "rpi3":
		// RPI firmware is done in the EFI partition
		internal.Log.Logger.Debug().Str("model", model).Msg("Running on RPI.")
	case "odroid-c2":
		internal.Log.Logger.Debug().Str("model", model).Msg("Running on Odroid-C2.")
		err = utils.DD("/arm/odroid-c2/bl1.bin.hardkernel", image, 1, 442, 0, 0)
		if err != nil {
			internal.Log.Logger.Error().Err(err).Msg("failed to dd bl1.bin.hardkernel")
			return err
		}
		err = utils.DD("/arm/odroid-c2/bl1.bin.hardkernel", image, 512, 0, 1, 1)
		if err != nil {
			internal.Log.Logger.Error().Err(err).Msg("failed to dd bl1.bin.hardkernel")
			return err
		}
		err = utils.DD("/arm/odroid-c2/u-boot.odroidc2", image, 512, 0, 0, 97)
		if err != nil {
			internal.Log.Logger.Error().Err(err).Msg("failed to dd u-boot.odroidc2")
			return err
		}
	case "pinebookpro":
		internal.Log.Logger.Debug().Str("model", model).Msg("Running on Pinebook Pro.")
		err = utils.DD("/arm/pinebookpro/idbloader.img", image, 64, 0, 0, 0)
		if err != nil {
			internal.Log.Logger.Error().Err(err).Msg("failed to dd idbloader.img")
			return err
		}
		err = utils.DD("/arm/pinebookpro/u-boot.itb", image, 16384, 0, 0, 0)
		if err != nil {
			internal.Log.Logger.Error().Err(err).Msg("failed to dd u-boot.itb")
			return err
		}
	}

	// Set the final image to be used by all as we run inside a container and the image is owned by root otherwise
	err = r.config.Fs.Chmod(image, 0777)
	if err != nil {
		internal.Log.Logger.Error().Err(err).Msg("failed to chmod final image")
		return err
	}
	return nil
}

// CopyAlpineShimAndGrub copies the grub.efi to the shim place and also to the grub place
// As alpine does not provide a shim, we need to copy the grub.efi to the shim place
// so we have the same behaviour as in other flavors
func (r *RawImage) CopyAlpineShimAndGrub(arch, target string) error {
	var err error
	grubSrc := filepath.Join("/amd/raw/grubartifacts", constants.EfiBootPath, "grub.efi")
	grubTarget := filepath.Join(target, constants.EfiBootPath, "bootx64.efi")

	if arch == "arm64" {
		grubSrc = filepath.Join("/arm/raw/grubartifacts", constants.EfiBootPath, "grub.efi")
		grubTarget = filepath.Join(target, constants.EfiBootPath, "bootaa64.efi")
	}

	err = utils.CopyFile(
		r.config.Fs,
		grubSrc,
		grubTarget,
	)
	if err != nil {
		return fmt.Errorf("could not copy file %s from %s", grubSrc, grubTarget)
	}
	r.config.Logger.Logger.Debug().Str("source", grubSrc).Str("target", grubTarget).Msg("Copied")
	// Also copy it into the grub name to have the same files as in other flavors
	err = utils.CopyFile(
		r.config.Fs,
		grubSrc,
		grubTarget,
	)
	if err != nil {
		return fmt.Errorf("could not copy file %s from %s", grubSrc, grubTarget)
	}
	r.config.Logger.Logger.Debug().Str("source", grubSrc).Str("target", grubTarget).Msg("Copied")

	return err
}
