package ops

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/kairos-io/AuroraBoot/internal"
	"github.com/kairos-io/AuroraBoot/pkg/schema"
	"github.com/kairos-io/AuroraBoot/pkg/utils"
	agentConstants "github.com/kairos-io/kairos/v4/agent/pkg/constants"
	fsutils "github.com/kairos-io/kairos/v4/agent/pkg/utils/fs"
	sdkConstants "github.com/kairos-io/kairos/v4/sdk/constants"
	sdkImage "github.com/kairos-io/kairos/v4/sdk/types/images"
	"github.com/kairos-io/kairos/v4/sdk/types/platform"
)

const (
	stateImagesDir   = "cOS"
	stateGrubDir     = "grub2"
	stateGrubCfgFile = "grub.cfg"
	grubEfiDirSuffix = "-efi"
	grubFontsDir     = "fonts"

	// Boot assessment files normally written to COS_STATE by the after-install/after-reset hooks of kairos-init's 08_grub.yaml.
	stateGrubCustomFile     = "grubcustom"
	stateBootAssessmentFile = "grub_boot_assessment"
	stateGrubMenuFile       = "grubmenu"
	rootfsBrandingGrubMenu  = "etc/kairos/branding/grubmenu.cfg"
	stateSizeOverheadMB     = 100
	// stateFsOverheadPercent leaves room for ext4 reserved blocks (5%), the journal and inode tables.
	stateFsOverheadPercent  = 10
	stateSystemImageSlots   = schema.MaxStateSlots // default: active, passive and the transition image during upgrades
	statePassiveMinSlots    = schema.MaxStateSlots // active, passive and the upgrade transition image
	stateTempDirName        = "state"
	stateMountTempDirName   = "state-mount"
	activeMountTempDirName  = "active-mount"
	statePartitionImageName = "state.img"
)

// grubCustomBootAssessmentHook matches the "Hook boot assessment grub configuration" step of 08_grub.yaml.
const grubCustomBootAssessmentHook = `set bootfile="/grub_boot_assessment"
search --no-floppy --file --set=bootfile_loc "${bootfile}"
if [ "${bootfile_loc}" ]; then
   source "(${bootfile_loc})${bootfile}"
fi
`

// grubBootAssessment matches the "Add boot assessment grub configuration" step of 08_grub.yaml.
const grubBootAssessment = `set boot_assessment="/boot_assessment"
search --no-floppy --file --set=boot_assessment_blk "${boot_assessment}"
if [ "${boot_assessment_blk}" ]; then
  load_env -f "(${boot_assessment_blk})${boot_assessment}"
fi
if [ "${enable_boot_assessment}" = "yes" -o "${enable_boot_assessment_always}" = "yes" ]; then
  if [ -z "${selected_entry}" ]; then
    if [ "${boot_assessment_tentative}" = "yes" ]; then
      set default="fallback"
      set extra_passive_cmdline="upgrade_failure"
    else
      set boot_assessment_tentative="yes"
      save_env -f "(${boot_assessment_blk})${boot_assessment}" boot_assessment_tentative
    fi
  fi
fi
`

// createStatePartitionImage builds the COS_STATE partition image that a reset would otherwise create on first boot.
func (r *RawImage) createStatePartitionImage() (string, error) {
	if r.systemImageSizeMB == 0 {
		return "", fmt.Errorf("system image size unknown: Build must calculate it before creating the state partition")
	}

	tmpDirState := filepath.Join(r.TempDir(), stateTempDirName)
	if err := fsutils.MkdirAll(r.config.Fs, filepath.Join(tmpDirState, stateImagesDir), 0755); err != nil {
		return "", err
	}
	defer r.config.Fs.RemoveAll(tmpDirState)

	tmpDirActiveMount := filepath.Join(r.TempDir(), activeMountTempDirName)
	if err := fsutils.MkdirAll(r.config.Fs, tmpDirActiveMount, 0755); err != nil {
		return "", err
	}
	defer r.config.Fs.RemoveAll(tmpDirActiveMount)

	activeImage := &sdkImage.Image{
		File:       filepath.Join(tmpDirState, stateImagesDir, agentConstants.ActiveImgFile),
		FS:         sdkConstants.LinuxImgFs,
		Label:      sdkConstants.ActiveLabel,
		Size:       r.systemImageSizeMB,
		Source:     sdkImage.NewDirSrc(r.Source),
		MountPoint: tmpDirActiveMount,
	}
	if _, err := r.elemental.DeployImage(activeImage, false); err != nil {
		internal.Log.Logger.Error().Err(err).Str("source", r.Source).Interface("image", activeImage).Msg("failed to create active image")
		return "", err
	}

	// Same passive image a kairos-agent install writes: a copy of active.img relabelled COS_PASSIVE
	if statePassiveEnabled(r.StateSlots) {
		passiveImage := &sdkImage.Image{
			File:   filepath.Join(tmpDirState, stateImagesDir, agentConstants.PassiveImgFile),
			FS:     sdkConstants.LinuxImgFs,
			Label:  sdkConstants.PassiveLabel,
			Source: sdkImage.NewFileSrc(activeImage.File),
		}
		if _, err := r.elemental.DeployImage(passiveImage, false); err != nil {
			internal.Log.Logger.Error().Err(err).Interface("image", passiveImage).Msg("failed to create passive image")
			return "", err
		}
	}

	if err := r.writeStateGrubFiles(tmpDirState); err != nil {
		return "", err
	}
	if err := r.elemental.SetDefaultGrubEntry(tmpDirState, r.Source, ""); err != nil {
		internal.Log.Logger.Error().Err(err).Msg("failed to set default grub entry")
		return "", err
	}

	tmpDirStateMount := filepath.Join(r.TempDir(), stateMountTempDirName)
	if err := fsutils.MkdirAll(r.config.Fs, tmpDirStateMount, 0755); err != nil {
		return "", err
	}
	defer r.config.Fs.RemoveAll(tmpDirStateMount)

	statePartitionImage := &sdkImage.Image{
		File:       filepath.Join(r.TempDir(), statePartitionImageName),
		FS:         sdkConstants.LinuxFs,
		Label:      sdkConstants.StateLabel,
		Size:       statePartitionSize(r.systemImageSizeMB, r.StateSlots, r.StateSize),
		Source:     sdkImage.NewDirSrc(tmpDirState),
		MountPoint: tmpDirStateMount,
	}
	if _, err := r.elemental.DeployImageNodirs(statePartitionImage, false); err != nil {
		internal.Log.Logger.Error().Err(err).Interface("image", statePartitionImage).Msg("failed to create state image")
		return "", err
	}
	return statePartitionImage.File, nil
}

// statePassiveEnabled reports whether STATE gets a passive.img; an upgrade then still needs room for its transition image.
func statePassiveEnabled(slots int) bool {
	return slots <= 0 || slots >= statePassiveMinSlots
}

// statePartitionSize returns the configured state size, or room for the given number of system images (0 means the default).
func statePartitionSize(systemImageSizeMB uint, slots int, configuredSizeMB int64) uint {
	if configuredSizeMB > 0 {
		return uint(configuredSizeMB)
	}
	if slots <= 0 {
		slots = stateSystemImageSlots
	}
	payloadMB := systemImageSizeMB*uint(slots) + stateSizeOverheadMB
	usablePercent := uint(100 - stateFsOverheadPercent)
	return (payloadMB*100 + usablePercent - 1) / usablePercent
}

// writeStateGrubFiles mirrors what grub.Install and the boot assessment hooks leave on COS_STATE.
func (r *RawImage) writeStateGrubFiles(stateDir string) error {
	grubCfg, err := r.config.Fs.ReadFile(filepath.Join(r.Source, agentConstants.GrubConf))
	if err != nil {
		internal.Log.Logger.Error().Err(err).Str("source", r.Source).Msg("failed to read grub.cfg")
		return err
	}
	files := map[string][]byte{
		filepath.Join(stateGrubDir, stateGrubCfgFile): grubCfg,
		stateGrubCustomFile:                           []byte(grubCustomBootAssessmentHook),
		stateBootAssessmentFile:                       []byte(grubBootAssessment),
	}
	branding, err := r.config.Fs.ReadFile(filepath.Join(r.Source, rootfsBrandingGrubMenu))
	if err == nil {
		files[stateGrubMenuFile] = branding
	}
	for name, content := range files {
		if err := r.writeFileMkdir(filepath.Join(stateDir, name), content); err != nil {
			return err
		}
	}
	return r.copyGrubEfiFiles(stateDir)
}

// copyGrubEfiFiles copies the grub modules and fonts from the rootfs into grub2/<arch>-efi on the state dir.
func (r *RawImage) copyGrubEfiFiles(stateDir string) error {
	arch, err := r.grubArch()
	if err != nil {
		return err
	}
	efiDir := filepath.Join(stateDir, stateGrubDir, arch+grubEfiDirSuffix)
	modules := map[string]bool{}
	targets := map[string]string{}
	for _, m := range agentConstants.GetGrubModules() {
		modules[m] = true
		targets[m] = efiDir
	}
	for _, f := range agentConstants.GetGrubFonts() {
		targets[f] = filepath.Join(efiDir, grubFontsDir)
	}

	copied := map[string]bool{}
	err = fsutils.WalkDirFs(r.config.Fs, r.Source, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		target, wanted := targets[d.Name()]
		if !wanted || d.IsDir() || copied[d.Name()] {
			return nil
		}
		// Modules are arch specific, fonts are not
		if modules[d.Name()] && !strings.Contains(path, arch) {
			return nil
		}
		content, err := r.config.Fs.ReadFile(path)
		if err != nil {
			return fmt.Errorf("error reading %s: %w", path, err)
		}
		copied[d.Name()] = true
		return r.writeFileMkdir(filepath.Join(target, d.Name()), content)
	})
	if err != nil {
		return err
	}

	for name := range targets {
		if copied[name] {
			continue
		}
		if modules[name] {
			return fmt.Errorf("did not find grub module %s for %s under %s", name, arch, r.Source)
		}
		internal.Log.Logger.Warn().Str("font", name).Str("rootfs", r.Source).Msg("did not find grub font")
	}
	return nil
}

// grubArch returns the grub platform arch (x86_64, arm64, ...) of the rootfs.
func (r *RawImage) grubArch() (string, error) {
	arch, err := utils.GetArchFromRootfs(r.Source, r.config.Logger)
	if err != nil {
		return "", err
	}
	p, err := platform.NewPlatformFromArch(arch)
	if err != nil {
		return "", err
	}
	return p.Arch, nil
}

func (r *RawImage) writeFileMkdir(path string, content []byte) error {
	if err := fsutils.MkdirAll(r.config.Fs, filepath.Dir(path), agentConstants.DirPerm); err != nil {
		return err
	}
	if err := r.config.Fs.WriteFile(path, content, agentConstants.FilePerm); err != nil {
		internal.Log.Logger.Error().Err(err).Str("target", path).Msg("failed to write file")
		return err
	}
	return nil
}
