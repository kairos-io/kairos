/*
Copyright © 2022 SUSE LLC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package config

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/kairos-io/kairos-agent/v2/pkg/constants"
	"github.com/kairos-io/kairos-agent/v2/pkg/implementations/imageextractor"
	"github.com/kairos-io/kairos-agent/v2/pkg/implementations/spec"
	fsutils "github.com/kairos-io/kairos-agent/v2/pkg/utils/fs"
	k8sutils "github.com/kairos-io/kairos-agent/v2/pkg/utils/k8s"
	"github.com/kairos-io/kairos-agent/v2/pkg/utils/partitions"
	"github.com/kairos-io/kairos-sdk/collector"
	sdkConstants "github.com/kairos-io/kairos-sdk/constants"
	"github.com/kairos-io/kairos-sdk/ghw"
	sdkConfig "github.com/kairos-io/kairos-sdk/types/config"
	sdkFS "github.com/kairos-io/kairos-sdk/types/fs"
	sdkImages "github.com/kairos-io/kairos-sdk/types/images"
	sdkLogger "github.com/kairos-io/kairos-sdk/types/logger"
	sdkPartitions "github.com/kairos-io/kairos-sdk/types/partitions"
	sdkRunner "github.com/kairos-io/kairos-sdk/types/runner"
	sdkSpec "github.com/kairos-io/kairos-sdk/types/spec"
	yipPlugins "github.com/mudler/yip/pkg/plugins"

	"github.com/go-viper/mapstructure/v2"
	"github.com/sanity-io/litter"
	"github.com/spf13/viper"
	"golang.org/x/sys/unix"
)

const (
	_ = 1 << (10 * iota)
	KiB
	MiB
	GiB
	TiB
)

// resolveTarget will try to resovle a /dev/disk/by-X disk into the final real disk under /dev/X
// We use it to calculate the device on the fly for the Config and the InstallSpec but we leave
// the original value in teh config.Collector so its written down in the final cloud config in the
// installed system, so users can know what parameters it was installed with in case they need to refer
// to it down the line to know what was the original parametes
// If the target is a normal /dev/X we dont do anything and return the original value so normal installs
// should not be affected
func resolveTarget(target string) (string, error) {
	var err error
	target, err = yipPlugins.ResolveScriptDevice(target)
	if err != nil {
		return "", err
	}

	// Accept that the target can be a /dev/disk/by-{label,uuid,path,etc..} and resolve it into a /dev/device
	if strings.HasPrefix(target, "/dev/disk/by-") {
		// we dont accept partitions as target so check and fail earlier for those that are partuuid or parlabel
		if strings.Contains(target, "partlabel") || strings.Contains(target, "partuuid") {
			return "", fmt.Errorf("target contains 'parlabel' or 'partuuid', looks like its a partition instead of a disk: %s", target)
		}
		// Use EvanSymlinks to properly resolve the full path to the target
		device, err := filepath.EvalSymlinks(target)
		if err != nil {
			return "", fmt.Errorf("failed to read device link for %s: %w", target, err)
		}
		if !strings.HasPrefix(device, "/dev/") {
			return "", fmt.Errorf("device %s is not a valid device path", device)
		}
		return device, nil
	}
	// If we don't resolve and don't fail, just return the original target
	return target, nil
}

// NewInstallSpec returns an InstallSpec struct all based on defaults and basic host checks (e.g. EFI vs BIOS)
func NewInstallSpec(cfg *sdkConfig.Config) (*spec.InstallSpec, error) {
	var firmware string
	var recoveryImg, activeImg, passiveImg sdkImages.Image

	recoveryImgFile := filepath.Join(constants.LiveDir, constants.RecoverySquashFile)

	// Check if current host has EFI firmware
	efiExists, _ := fsutils.Exists(cfg.Fs, constants.EfiDevice)
	// Check the default ISO installation media is available
	isoRootExists, _ := fsutils.Exists(cfg.Fs, constants.IsoBaseTree)
	// Check the default ISO recovery installation media is available)
	recoveryExists, _ := fsutils.Exists(cfg.Fs, recoveryImgFile)

	if efiExists {
		firmware = sdkConstants.EFI
	} else {
		firmware = sdkConstants.BIOS
	}

	activeImg.Label = sdkConstants.ActiveLabel
	activeImg.Size = sdkConstants.ImgSize
	activeImg.File = filepath.Join(constants.StateDir, "cOS", constants.ActiveImgFile)
	activeImg.FS = sdkConstants.LinuxImgFs
	activeImg.MountPoint = constants.ActiveDir

	// First try to use the install media source
	if isoRootExists {
		activeImg.Source = sdkImages.NewDirSrc(constants.IsoBaseTree)
	}
	// Then any user provided source
	if cfg.Install.Source != "" {
		activeImg.Source, _ = sdkImages.NewSrcFromURI(cfg.Install.Source)
	}
	// If we dont have any just an empty source so the sanitation fails
	// TODO: Should we directly fail here if we got no source instead of waiting for the Sanitize() to fail?
	if !isoRootExists && cfg.Install.Source == "" {
		activeImg.Source = sdkImages.NewEmptySrc()
	}

	if recoveryExists {
		recoveryImg.Source = sdkImages.NewFileSrc(recoveryImgFile)
		recoveryImg.FS = constants.SquashFs
		recoveryImg.File = filepath.Join(constants.RecoveryDir, "cOS", constants.RecoverySquashFile)
		recoveryImg.Size = sdkConstants.ImgSize
	} else {
		recoveryImg.Source = sdkImages.NewFileSrc(activeImg.File)
		recoveryImg.FS = sdkConstants.LinuxImgFs
		recoveryImg.Label = sdkConstants.SystemLabel
		recoveryImg.File = filepath.Join(constants.RecoveryDir, "cOS", constants.RecoveryImgFile)
		recoveryImg.Size = sdkConstants.ImgSize
	}

	passiveImg = sdkImages.Image{
		File:   filepath.Join(constants.StateDir, "cOS", constants.PassiveImgFile),
		Label:  sdkConstants.PassiveLabel,
		Source: sdkImages.NewFileSrc(activeImg.File),
		FS:     sdkConstants.LinuxImgFs,
		Size:   sdkConstants.ImgSize,
	}

	spec := &spec.InstallSpec{
		Target:    cfg.Install.Device,
		Firmware:  firmware,
		PartTable: sdkConstants.GPT,
		GrubConf:  constants.GrubConf,
		Tty:       constants.DefaultTty,
		Active:    activeImg,
		Recovery:  recoveryImg,
		Passive:   passiveImg,
		NoFormat:  cfg.Install.NoFormat,
		Reboot:    cfg.Install.Reboot,
		PowerOff:  cfg.Install.Poweroff,
	}

	// Honor install.allow-insecure-registries before any image is fetched
	if err := applyAllowInsecureRegistries(cfg, "install"); err != nil {
		return nil, err
	}

	// Get the actual source size to calculate the image size and partitions size
	size, err := GetSourceSize(cfg, spec.Active.Source)
	if err != nil {
		cfg.Logger.Warnf("Failed to infer size for images: %s", err.Error())
	} else {
		cfg.Logger.Infof("Setting image size to %dMiB", size)
		spec.Active.Size = uint(size)
		spec.Passive.Size = uint(size)
		spec.Recovery.Size = uint(size)
	}

	err = unmarshallFullSpec(cfg, "install", spec)
	if err != nil {
		return nil, fmt.Errorf("failed unmarshalling the full spec: %w", err)
	}

	// resolve also the target of the spec so we can partition properly
	spec.Target, err = resolveTarget(spec.Target)
	if err != nil {
		return nil, err
	}

	// Calculate the partitions afterwards so they use the image sizes for the final partition sizes
	spec.Partitions = NewInstallElementalPartitions(cfg.Logger, spec)

	return spec, nil
}

func NewInstallElementalPartitions(log sdkLogger.KairosLogger, spec *spec.InstallSpec) sdkPartitions.ElementalPartitions {
	pt := sdkPartitions.ElementalPartitions{}
	var oemSize uint
	if spec.Partitions.OEM != nil && spec.Partitions.OEM.Size != 0 {
		oemSize = spec.Partitions.OEM.Size
	} else {
		oemSize = sdkConstants.OEMSize
	}
	pt.OEM = &sdkPartitions.Partition{
		FilesystemLabel: sdkConstants.OEMLabel,
		Size:            oemSize,
		Name:            sdkConstants.OEMPartName,
		FS:              sdkConstants.LinuxFs,
		MountPoint:      constants.OEMDir,
		Flags:           []string{},
	}
	log.Infof("Setting OEM partition size to %dMiB", oemSize)
	// Double the space for recovery, as upgrades use the recovery partition to create the transition image for upgrades
	// so we need twice the space to do a proper upgrade
	// Check if the default/user provided values are enough to fit the images sizes
	var recoverySize uint
	if spec.Partitions.Recovery == nil { // This means its not configured by user so use the default
		recoverySize = (spec.Recovery.Size * 2) + 200
	} else {
		if spec.Partitions.Recovery.Size < (spec.Recovery.Size*2)+200 { // Configured by user but not enough space
			// If we had the logger here we could log a message saying that space is not enough and we are auto increasing it
			recoverySize = (spec.Recovery.Size * 2) + 200
			log.Warnf("Not enough space set for recovery partition(%dMiB), increasing it to fit the recovery images(%dMiB)", spec.Partitions.Recovery.Size, recoverySize)
		} else {
			recoverySize = spec.Partitions.Recovery.Size
		}
	}
	log.Infof("Setting recovery partition size to %dMiB", recoverySize)
	pt.Recovery = &sdkPartitions.Partition{
		FilesystemLabel: sdkConstants.RecoveryLabel,
		Size:            recoverySize,
		Name:            sdkConstants.RecoveryPartName,
		FS:              sdkConstants.LinuxFs,
		MountPoint:      constants.RecoveryDir,
		Flags:           []string{},
	}

	// Add 1 Gb to the partition so images can grow a bit, otherwise you are stuck with the smallest space possible and
	// there is no coming back from that
	// Also multiply the space for active, as upgrades use the state partition to create the transition image for upgrades
	// so we need twice the space to do a proper upgrade
	// Check if the default/user provided values are enough to fit the images sizes
	var stateSize uint
	if spec.Partitions.State == nil { // This means its not configured by user so use the default
		stateSize = (spec.Active.Size * 2) + spec.Passive.Size + 1000
	} else {
		if spec.Partitions.State.Size < (spec.Active.Size*2)+spec.Passive.Size+1000 { // Configured by user but not enough space
			stateSize = (spec.Active.Size * 2) + spec.Passive.Size + 1000
			log.Warnf("Not enough space set for state partition(%dMiB), increasing it to fit the state images(%dMiB)", spec.Partitions.State.Size, stateSize)
		} else {
			stateSize = spec.Partitions.State.Size
		}
	}
	log.Infof("Setting state partition size to %dMiB", stateSize)
	pt.State = &sdkPartitions.Partition{
		FilesystemLabel: sdkConstants.StateLabel,
		Size:            stateSize,
		Name:            sdkConstants.StatePartName,
		FS:              sdkConstants.LinuxFs,
		MountPoint:      constants.StateDir,
		Flags:           []string{},
	}
	var persistentSize uint
	if spec.Partitions.Persistent == nil { // This means its not configured by user so use the default
		persistentSize = sdkConstants.PersistentSize
	} else {
		persistentSize = spec.Partitions.Persistent.Size
	}
	pt.Persistent = &sdkPartitions.Partition{
		FilesystemLabel: sdkConstants.PersistentLabel,
		Size:            persistentSize,
		Name:            sdkConstants.PersistentPartName,
		FS:              sdkConstants.LinuxFs,
		MountPoint:      constants.PersistentDir,
		Flags:           []string{},
	}
	log.Infof("Setting persistent partition size to %dMiB", persistentSize)

	// Carry over a user-configured EFI partition size (install.partitions.efi.size).
	// The EFI partition itself is created later during Sanitize() by
	// SetFirmwarePartitions, which preserves a non-zero size we set here and
	// otherwise applies the default. Some platforms (e.g. Jetson Thor) need a
	// larger ESP to stage a UEFI firmware capsule.
	if spec.Partitions.EFI != nil && spec.Partitions.EFI.Size != 0 {
		pt.EFI = &sdkPartitions.Partition{Size: spec.Partitions.EFI.Size}
		log.Infof("Setting EFI partition size to %dMiB", spec.Partitions.EFI.Size)
	}
	return pt
}

// NewUpgradeSpec returns an UpgradeSpec struct all based on defaults and current host state
func NewUpgradeSpec(cfg *sdkConfig.Config) (*spec.UpgradeSpec, error) {
	var recLabel, recFs, recMnt string
	var active, passive, recovery sdkImages.Image

	parts, err := partitions.GetAllPartitions(&cfg.Logger)
	if err != nil {
		return nil, fmt.Errorf("could not read host partitions")
	}
	ep := spec.NewElementalPartitionsFromList(parts)

	if ep.Recovery == nil {
		// We could have recovery in lvm which won't appear in ghw list
		ep.Recovery = partitions.GetPartitionViaDM(cfg.Fs, sdkConstants.RecoveryLabel)
	}

	if ep.OEM == nil {
		// We could have OEM in lvm which won't appear in ghw list
		ep.OEM = partitions.GetPartitionViaDM(cfg.Fs, sdkConstants.OEMLabel)
	}

	if ep.Persistent == nil {
		// We could have persistent encrypted or in lvm which won't appear in ghw list
		ep.Persistent = partitions.GetPartitionViaDM(cfg.Fs, sdkConstants.PersistentLabel)
	}

	if ep.Recovery != nil {
		if ep.Recovery.MountPoint == "" {
			ep.Recovery.MountPoint = constants.RecoveryDir
		}

		squashedRec, err := hasSquashedRecovery(cfg, ep.Recovery)
		if err != nil {
			return nil, fmt.Errorf("failed checking for squashed recovery: %w", err)
		}

		if squashedRec {
			recFs = constants.SquashFs
		} else {
			recLabel = sdkConstants.SystemLabel
			recFs = sdkConstants.LinuxImgFs
			recMnt = constants.TransitionDir
		}

		recovery = sdkImages.Image{
			File:       filepath.Join(ep.Recovery.MountPoint, "cOS", constants.TransitionImgFile),
			Size:       sdkConstants.ImgSize,
			Label:      recLabel,
			FS:         recFs,
			MountPoint: recMnt,
			Source:     sdkImages.NewEmptySrc(),
		}
	}

	if ep.State != nil {
		if ep.State.MountPoint == "" {
			ep.State.MountPoint = constants.StateDir
		}

		active = sdkImages.Image{
			File:       filepath.Join(ep.State.MountPoint, "cOS", constants.TransitionImgFile),
			Size:       sdkConstants.ImgSize,
			Label:      sdkConstants.ActiveLabel,
			FS:         sdkConstants.LinuxImgFs,
			MountPoint: constants.TransitionDir,
			Source:     sdkImages.NewEmptySrc(),
		}

		passive = sdkImages.Image{
			File:   filepath.Join(ep.State.MountPoint, "cOS", constants.PassiveImgFile),
			Label:  sdkConstants.PassiveLabel,
			Size:   sdkConstants.ImgSize,
			Source: sdkImages.NewFileSrc(active.File),
			FS:     active.FS,
		}
	}

	// If we have oem in the system, but it has no mountpoint
	if ep.OEM != nil && ep.OEM.MountPoint == "" {
		// Add the default mountpoint for it in case the chroot stages want to bind mount it
		ep.OEM.MountPoint = constants.OEMPath
	}
	// This is needed if we want to use the persistent as tmpdir for the upgrade images
	// as tmpfs is 25% of the total RAM, we cannot rely on the tmp dir having enough space for our image
	// This enables upgrades on low ram devices
	if ep.Persistent != nil {
		if ep.Persistent.MountPoint == "" {
			ep.Persistent.MountPoint = constants.PersistentDir
		}
	}

	// Deep look to see if upgrade.recovery == true in the config
	// if yes, we set the upgrade spec "Entry" to "recovery"
	entry := ""
	_, ok := cfg.Collector.Values["upgrade"]
	if ok {
		_, ok = cfg.Collector.Values["upgrade"].(collector.ConfigValues)["recovery"]
		if ok {
			if cfg.Collector.Values["upgrade"].(collector.ConfigValues)["recovery"].(bool) {
				entry = constants.BootEntryRecovery
			}
		}
	}

	spec := &spec.UpgradeSpec{
		Entry:      entry,
		Active:     active,
		Recovery:   recovery,
		Passive:    passive,
		Partitions: ep,
	}

	// Unmarshall the config into the spec first so the active/recovery gets filled properly from all sources
	err = unmarshallFullSpec(cfg, "upgrade", spec)
	if err != nil {
		return nil, fmt.Errorf("failed unmarshalling the full spec: %w", err)
	}
	// Honor upgrade.allow-insecure-registries before any image is fetched
	if err := applyAllowInsecureRegistries(cfg, "upgrade"); err != nil {
		return nil, err
	}
	err = setUpgradeSourceSize(cfg, spec)
	if err != nil {
		return nil, fmt.Errorf("failed calculating size: %w", err)
	}

	return spec, nil
}

func setUpgradeSourceSize(cfg *sdkConfig.Config, spec *spec.UpgradeSpec) error {
	var size int64
	var err error
	var originalSize uint

	// Store the default given size in the spec. This includes the user specified values which have already been marshalled in the spec
	if spec.RecoveryUpgrade() {
		originalSize = spec.Recovery.Size
	} else {
		originalSize = spec.Active.Size
	}

	var targetSpec *sdkImages.Image
	if spec.RecoveryUpgrade() {
		targetSpec = &(spec.Recovery)
	} else {
		targetSpec = &(spec.Active)
	}

	if targetSpec.Source != nil && targetSpec.Source.IsEmpty() {
		cfg.Logger.Debugf("No source specified for image, skipping size calculation")
		return nil
	}

	size, err = GetSourceSize(cfg, targetSpec.Source)
	if err != nil {
		cfg.Logger.Warnf("Failed to infer size for images: %s", err.Error())
		return err
	}

	if uint(size) < originalSize {
		cfg.Logger.Debugf("Calculated size (%dMiB) is less than specified/default size (%dMiB)", size, originalSize)
		targetSpec.Size = originalSize
	} else {
		cfg.Logger.Debugf("Calculated size (%dMiB) is higher than specified/default size (%dMiB)", size, originalSize)
		targetSpec.Size = uint(size)
	}

	cfg.Logger.Infof("Setting image size to %dMiB", targetSpec.Size)

	return nil
}

// NewResetSpec returns a ResetSpec struct all based on defaults and current host state
func NewResetSpec(cfg *sdkConfig.Config) (*spec.ResetSpec, error) {
	var imgSource *sdkImages.ImageSource

	//TODO find a way to pre-load current state values such as labels
	if !BootedFrom(cfg.Runner, constants.RecoverySquashFile) &&
		!BootedFrom(cfg.Runner, sdkConstants.SystemLabel) {
		return nil, fmt.Errorf("reset can only be called from the recovery system")
	}

	efiExists, _ := fsutils.Exists(cfg.Fs, constants.EfiDevice)

	parts, err := partitions.GetAllPartitions(&cfg.Logger)
	if err != nil {
		return nil, fmt.Errorf("could not read host partitions")
	}
	ep := spec.NewElementalPartitionsFromList(parts)
	if efiExists {
		if ep.EFI == nil {
			return nil, fmt.Errorf("EFI partition not found")
		}
		if ep.EFI.MountPoint == "" {
			ep.EFI.MountPoint = sdkConstants.EfiDirTransient
		}
		ep.EFI.Name = sdkConstants.EfiPartName
	}

	if ep.State == nil {
		return nil, fmt.Errorf("state partition not found")
	}
	if ep.State.MountPoint == "" {
		ep.State.MountPoint = constants.StateDir
	}
	ep.State.Name = sdkConstants.StatePartName

	if ep.Recovery == nil {
		// We could have recovery in lvm which won't appear in ghw list
		ep.Recovery = partitions.GetPartitionViaDM(cfg.Fs, sdkConstants.RecoveryLabel)
		if ep.Recovery == nil {
			return nil, fmt.Errorf("recovery partition not found")
		}
	}
	if ep.Recovery.MountPoint == "" {
		ep.Recovery.MountPoint = constants.RecoveryDir
	}

	target := ep.State.Disk

	// OEM partition is not a hard requirement for reset unless we have the reset oem flag
	if ep.OEM == nil {
		// We could have oem in lvm which won't appear in ghw list
		ep.OEM = partitions.GetPartitionViaDM(cfg.Fs, sdkConstants.OEMLabel)
	}

	// Persistent partition is not a hard requirement
	if ep.Persistent == nil {
		// We could have persistent encrypted or in lvm which won't appear in ghw list
		ep.Persistent = partitions.GetPartitionViaDM(cfg.Fs, sdkConstants.PersistentLabel)
	}

	recoveryImg := filepath.Join(constants.RunningStateDir, "cOS", constants.RecoveryImgFile)
	recoveryImg2 := filepath.Join(constants.RunningRecoveryStateDir, "cOS", constants.RecoveryImgFile)

	if exists, _ := fsutils.Exists(cfg.Fs, recoveryImg); exists {
		imgSource = sdkImages.NewFileSrc(recoveryImg)
	} else if exists, _ = fsutils.Exists(cfg.Fs, recoveryImg2); exists {
		imgSource = sdkImages.NewFileSrc(recoveryImg2)
	} else if exists, _ = fsutils.Exists(cfg.Fs, constants.IsoBaseTree); exists {
		imgSource = sdkImages.NewDirSrc(constants.IsoBaseTree)
	} else {
		imgSource = sdkImages.NewEmptySrc()
	}

	activeFile := filepath.Join(ep.State.MountPoint, "cOS", constants.ActiveImgFile)
	spec := &spec.ResetSpec{
		Target:           target,
		Partitions:       ep,
		Efi:              efiExists,
		GrubDefEntry:     constants.GrubDefEntry,
		GrubConf:         constants.GrubConf,
		Tty:              constants.DefaultTty,
		FormatPersistent: true,
		Active: sdkImages.Image{
			Label:      sdkConstants.ActiveLabel,
			Size:       sdkConstants.ImgSize,
			File:       activeFile,
			FS:         sdkConstants.LinuxImgFs,
			Source:     imgSource,
			MountPoint: constants.ActiveDir,
		},
		Passive: sdkImages.Image{
			File:   filepath.Join(ep.State.MountPoint, "cOS", constants.PassiveImgFile),
			Label:  sdkConstants.PassiveLabel,
			Size:   sdkConstants.ImgSize,
			Source: sdkImages.NewFileSrc(activeFile),
			FS:     sdkConstants.LinuxImgFs,
		},
	}

	// Get the actual source size to calculate the image size and partitions size
	size, err := GetSourceSize(cfg, spec.Active.Source)
	if err != nil {
		cfg.Logger.Warnf("Failed to infer size for images: %s", err.Error())
	} else {
		cfg.Logger.Infof("Setting image size to %dMiB", size)
		spec.Active.Size = uint(size)
		spec.Passive.Size = uint(size)
	}

	err = unmarshallFullSpec(cfg, "reset", spec)
	if err != nil {
		return nil, fmt.Errorf("failed unmarshalling the full spec: %w", err)
	}

	if ep.OEM == nil && spec.FormatOEM {
		cfg.Logger.Warnf("no OEM partition found, won't format it")
	}

	if ep.Persistent == nil && spec.FormatPersistent {
		cfg.Logger.Warnf("no Persistent partition found, won't format it")
	}

	// If we mount partitions by the /dev/disk/by-label stanza, their mountpoints wont show up due to ghw not
	// accounting for them. Normally this is not an issue but in this case, if we are formatting a given partition we
	// want to unmount it first. Thus we need to make sure that if its mounted we have the mountpoint info
	if ep.Persistent.MountPoint == "" {
		ep.Persistent.MountPoint = partitions.GetMountPointByLabel(ep.Persistent.FilesystemLabel)
	}

	if ep.OEM.MountPoint == "" {
		ep.OEM.MountPoint = partitions.GetMountPointByLabel(ep.OEM.FilesystemLabel)
	}

	return spec, nil
}

// ReadResetSpecFromConfig will return a proper ResetSpec based on an agent Config
func ReadResetSpecFromConfig(c *sdkConfig.Config) (*spec.ResetSpec, error) {
	sp, err := ReadSpecFromCloudConfig(c, "reset")
	if err != nil {
		return &spec.ResetSpec{}, err
	}
	resetSpec := sp.(*spec.ResetSpec)
	return resetSpec, nil
}

func NewUkiResetSpec(cfg *sdkConfig.Config) (*spec.ResetUkiSpec, error) {
	sp := &spec.ResetUkiSpec{
		FormatPersistent: true, // Persistent is formatted by default
		Partitions:       sdkPartitions.ElementalPartitions{},
	}

	_, ukiBootMode := cfg.Fs.Stat("/run/cos/uki_boot_mode")
	if !BootedFrom(cfg.Runner, "rd.immucore.uki") && ukiBootMode == nil {
		return sp, fmt.Errorf("uki reset can only be called from the recovery installed system")
	}

	// Fill persistent partition
	sp.Partitions.Persistent = partitions.GetPartitionViaDM(cfg.Fs, sdkConstants.PersistentLabel)
	sp.Partitions.OEM = partitions.GetPartitionViaDM(cfg.Fs, sdkConstants.OEMLabel)

	// Get EFI partition
	parts, err := partitions.GetAllPartitions(&cfg.Logger)
	if err != nil {
		return sp, fmt.Errorf("could not read host partitions")
	}
	for _, p := range parts {
		if p.FilesystemLabel == sdkConstants.EfiLabel {
			sp.Partitions.EFI = p
			break
		}
	}

	if sp.Partitions.Persistent == nil {
		return sp, fmt.Errorf("persistent partition not found")
	}
	if sp.Partitions.OEM == nil {
		return sp, fmt.Errorf("oem partition not found")
	}
	if sp.Partitions.EFI == nil {
		return sp, fmt.Errorf("efi partition not found")
	}

	// Fill oem partition
	err = unmarshallFullSpec(cfg, "reset", sp)
	return sp, err
}

// ReadInstallSpecFromConfig will return a proper v1.InstallSpec based on an agent Config
func ReadInstallSpecFromConfig(c *sdkConfig.Config) (*spec.InstallSpec, error) {
	sp, err := ReadSpecFromCloudConfig(c, "install")
	if err != nil {
		return &spec.InstallSpec{}, err
	}
	installSpec := sp.(*spec.InstallSpec)

	if (installSpec.Target == "" || installSpec.Target == "auto") && !installSpec.NoFormat {
		installSpec.Target = detectLargestDevice()
	}

	return installSpec, nil
}

// ReadUkiResetSpecFromConfig will return a proper v1.ResetUkiSpec based on an agent Config
func ReadUkiResetSpecFromConfig(c *sdkConfig.Config) (*spec.ResetUkiSpec, error) {
	sp, err := ReadSpecFromCloudConfig(c, "reset-uki")
	if err != nil {
		return &spec.ResetUkiSpec{}, err
	}
	resetSpec := sp.(*spec.ResetUkiSpec)
	return resetSpec, nil
}

func NewUkiInstallSpec(cfg *sdkConfig.Config) (*spec.InstallUkiSpec, error) {
	var activeImg sdkImages.Image

	isoRootExists, _ := fsutils.Exists(cfg.Fs, constants.IsoBaseTree)

	// First try to use the install media source
	if isoRootExists {
		activeImg.Source = sdkImages.NewDirSrc(constants.IsoBaseTree)
	}
	// Then any user provided source
	if cfg.Install.Source != "" {
		activeImg.Source, _ = sdkImages.NewSrcFromURI(cfg.Install.Source)
	}
	// If we dont have any just an empty source so the sanitation fails
	// TODO: Should we directly fail here if we got no source instead of waiting for the Sanitize() to fail?
	if !isoRootExists && cfg.Install.Source == "" {
		activeImg.Source = sdkImages.NewEmptySrc()
	}

	spec := &spec.InstallUkiSpec{
		Target: cfg.Install.Device,
		Active: activeImg,
	}

	// Calculate the partitions afterwards so they use the image sizes for the final partition sizes
	spec.Partitions.EFI = &sdkPartitions.Partition{
		FilesystemLabel: sdkConstants.EfiLabel,
		Size:            sdkConstants.ImgSize * 5, // 15Gb for the EFI partition as default
		Name:            sdkConstants.EfiPartName,
		FS:              sdkConstants.EfiFs,
		MountPoint:      sdkConstants.EfiDirTransient,
		Flags:           []string{"esp"},
	}
	spec.Partitions.OEM = &sdkPartitions.Partition{
		FilesystemLabel: sdkConstants.OEMLabel,
		Size:            sdkConstants.OEMSize,
		Name:            sdkConstants.OEMPartName,
		FS:              sdkConstants.LinuxFs,
		MountPoint:      constants.OEMDir,
		Flags:           []string{},
	}
	spec.Partitions.Persistent = &sdkPartitions.Partition{
		FilesystemLabel: sdkConstants.PersistentLabel,
		Size:            sdkConstants.PersistentSize,
		Name:            sdkConstants.PersistentPartName,
		FS:              sdkConstants.LinuxFs,
		MountPoint:      constants.PersistentDir,
		Flags:           []string{},
	}

	// Honor install.allow-insecure-registries before any image is fetched
	if err := applyAllowInsecureRegistries(cfg, "install"); err != nil {
		return nil, err
	}

	// Get the actual source size to calculate the image size and partitions size
	size, err := GetSourceSize(cfg, spec.Active.Source)
	if err != nil {
		cfg.Logger.Warnf("Failed to infer size for images, leaving it as default size (%dMiB): %s", spec.Partitions.EFI.Size, err.Error())
	} else {
		// Only override if the calculated size is bigger than the default size, otherwise stay with 15Gb minimum
		if uint(size*3) > spec.Partitions.EFI.Size {
			spec.Partitions.EFI.Size = uint(size * 3)
		}
	}

	cfg.Logger.Infof("Setting image size to %dMiB", spec.Partitions.EFI.Size)

	err = unmarshallFullSpec(cfg, "install", spec)

	// Add default values for the skip partitions for our default entries
	spec.SkipEntries = append(spec.SkipEntries, constants.UkiDefaultSkipEntries()...)
	return spec, err
}

// ReadUkiInstallSpecFromConfig will return a proper v1.InstallUkiSpec based on an agent Config
func ReadUkiInstallSpecFromConfig(c *sdkConfig.Config) (*spec.InstallUkiSpec, error) {
	sp, err := ReadSpecFromCloudConfig(c, "install-uki")
	if err != nil {
		return &spec.InstallUkiSpec{}, err
	}
	installSpec := sp.(*spec.InstallUkiSpec)

	if (installSpec.Target == "" || installSpec.Target == "auto") && !installSpec.NoFormat {
		installSpec.Target = detectLargestDevice()
	}

	return installSpec, nil
}

func NewUkiUpgradeSpec(cfg *sdkConfig.Config) (*spec.UpgradeUkiSpec, error) {
	spec := &spec.UpgradeUkiSpec{}
	if err := unmarshallFullSpec(cfg, "upgrade", spec); err != nil {
		return nil, fmt.Errorf("failed unmarshalling full spec: %w", err)
	}
	// Honor upgrade.allow-insecure-registries before any image is fetched
	if err := applyAllowInsecureRegistries(cfg, "upgrade"); err != nil {
		return nil, err
	}
	// Get the actual source size to calculate the image size and partitions size.
	// For a container source this pulls the manifest through the SDK
	// ImageExtractor (which is insecure-aware), so a missing/unreachable image
	// fails fast here - this replaces the old crane.Manifest pre-flight. For
	// other source types a size error is non-fatal and we fall back to a default.
	size, err := GetSourceSize(cfg, spec.Active.Source)
	if err != nil {
		if spec.Active.Source.IsDocker() {
			return nil, fmt.Errorf("could not resolve image %s: %w", spec.Active.Source.Value(), err)
		}
		cfg.Logger.Warnf("Failed to infer size for images: %s", err.Error())
		spec.Active.Size = sdkConstants.ImgSize
	} else {
		cfg.Logger.Infof("Setting image size to %dMiB", size)
		spec.Active.Size = uint(size)
	}

	// Get EFI partition
	spec.EfiPartition, err = partitions.GetEfiPartition(&cfg.Logger)
	if err != nil {
		return spec, fmt.Errorf("could not read host partitions")
	}

	// Get free size of partition in MiB so it can be compared with the image
	// sizes, which are also in MiB.
	var stat unix.Statfs_t
	_ = unix.Statfs(spec.EfiPartition.MountPoint, &stat)
	freeSize := stat.Bfree * uint64(stat.Bsize) / 1024 / 1024
	cfg.Logger.Debugf("Partition on mountpoint %s has %dMiB free", spec.EfiPartition.MountPoint, freeSize)
	// Check if the source is over the free size
	if spec.Active.Size > uint(freeSize) {
		return spec, fmt.Errorf("source size(%d) is bigger than the free space(%d) on the EFI partition(%s)", spec.Active.Size, freeSize, spec.EfiPartition.MountPoint)
	}

	return spec, err
}

// ReadUkiUpgradeSpecFromConfig will return a proper v1.UpgradeUkiSpec based on an agent Config
func ReadUkiUpgradeSpecFromConfig(c *sdkConfig.Config) (*spec.UpgradeUkiSpec, error) {
	sp, err := ReadSpecFromCloudConfig(c, "upgrade-uki")
	if err != nil {
		return &spec.UpgradeUkiSpec{}, err
	}
	upgradeSpec := sp.(*spec.UpgradeUkiSpec)
	return upgradeSpec, nil
}

// getSize will calculate the size of a file or symlink and will do nothing with directories
// fileList: keeps track of the files visited to avoid counting a file more than once if it's a symlink. It could also be used as a way to filter some files
// size: will be the memory that adds up all the files sizes. Meaning it could be initialized with a value greater than 0 if needed.
func getSize(vfs sdkFS.KairosFS, size *int64, fileList map[string]bool, path string, d fs.DirEntry, err error) error {
	if err != nil {
		return err
	}

	if d.IsDir() {
		return nil
	}

	actualFilePath := path
	if d.Type()&fs.ModeSymlink != 0 {
		// If it's a symlink, get its target and calculate its size.
		var err error
		actualFilePath, err = vfs.Readlink(path)
		if err != nil {
			return err
		}

		if !filepath.IsAbs(actualFilePath) {
			// If it's a relative path, join it with the base directory path.
			actualFilePath = filepath.Join(filepath.Dir(path), actualFilePath)
		}
	}

	fileInfo, err := vfs.Stat(actualFilePath)
	if os.IsNotExist(err) || fileList[actualFilePath] {
		return nil
	}
	if err != nil {
		return err
	}

	*size += fileInfo.Size()
	fileList[actualFilePath] = true

	return nil
}

// GetSourceSize will try to gather the actual size of the source
// Useful to create the exact size of images and by side effect the partition size
// This helps adjust the size to be juuuuust right.
// It can still be manually override from the cloud config by setting all values manually
// But by default it should adjust the sizes properly
func GetSourceSize(config *sdkConfig.Config, source *sdkImages.ImageSource) (int64, error) {
	var size int64
	var err error
	var filesVisited map[string]bool

	switch {
	case source.IsDocker():
		// Docker size is uncompressed! So we double it to be sure that we have enough space
		// Otherwise we would need to READ all the layers uncompressed to calculate the image size which would
		// double the download size and slow down everything
		size, err = config.ImageExtractor.GetOCIImageSize(source.Value(), config.Platform.String())
		size = int64(float64(size) * 2.5)
	case source.IsDir():
		filesVisited = make(map[string]bool, 30000) // An Ubuntu system has around 27k files. This improves performance by not having to resize the map for every file visited
		// In kubernetes we use the suc script to upgrade (https://github.com/kairos-io/packages/blob/main/packages/system/suc-upgrade/suc-upgrade.sh)
		// , which mounts the host root into $HOST_DIR
		// we should skip that dir when calculating the size as we would be doubling the calculated size
		// Plus we will hit the usual things when checking a running system. Processes that go away, tmpfiles, etc...
		// This is always set for pods running under kubernetes
		_, underKubernetes := os.LookupEnv("KUBERNETES_SERVICE_HOST")
		// Try to get the HOST_DIR in case we are not using the default one
		hostDir := k8sutils.GetHostDirForK8s()
		config.Logger.Logger.Debug().Bool("status", underKubernetes).Str("hostdir", hostDir).Msg("Kubernetes check")
		err = fsutils.WalkDirFs(config.Fs, source.Value(), func(path string, d fs.DirEntry, err error) error {
			// If its empty we are just not setting it, so probably out of the k8s upgrade path
			if hostDir != "" && strings.HasPrefix(path, hostDir) {
				config.Logger.Logger.Debug().Str("path", path).Str("hostDir", hostDir).Msg("Skipping dir as it is a host directory")
				return fs.SkipDir
			} else if underKubernetes && (strings.HasPrefix(path, "/proc") || strings.HasPrefix(path, "/dev") || strings.HasPrefix(path, "/run")) {
				// If under kubernetes, the upgrade will check the size of / which includes the host dir mounted under /host
				// But it also can bind the host mounts into / so we want to skip those runtime dirs
				// During install or upgrade outside kubernetes, we dont care about those dirs as they are not expected to be in the source dir
				config.Logger.Logger.Debug().Str("path", path).Str("hostDir", hostDir).Msg("Skipping dir as it is a runtime directory under kubernetes (/proc, /dev or /run)")
				return fs.SkipDir
			} else {
				v := getSize(config.Fs, &size, filesVisited, path, d, err)
				return v
			}
		})

	case source.IsOCIFile():
		// For OCI tar files, get the tar file size and apply a multiplier
		// since the tar contains compressed layers that will expand
		file, err := config.Fs.Stat(source.Value())
		if err != nil {
			return size, err
		}
		// Apply a 2x multiplier to account for compression and extraction overhead
		// Similar to Docker images but more conservative since it's a local file
		size = int64(float64(file.Size()) * 2.0)

	case source.IsFile():
		file, err := config.Fs.Stat(source.Value())
		if err != nil {
			return size, err
		}
		size = file.Size()
	}
	// Normalize size to MiB before returning and add 100MiB to round the size from bytes to MiB+extra files like grub stuff
	if size != 0 {
		size = (size / 1024 / 1024) + 100
	}
	return size, err
}

// ReadUpgradeSpecFromConfig will return a proper v1.UpgradeSpec based on an agent Config
func ReadUpgradeSpecFromConfig(c *sdkConfig.Config) (*spec.UpgradeSpec, error) {
	sp, err := ReadSpecFromCloudConfig(c, "upgrade")
	if err != nil {
		return &spec.UpgradeSpec{}, err
	}
	upgradeSpec := sp.(*spec.UpgradeSpec)
	return upgradeSpec, nil
}

// ReadSpecFromCloudConfig returns a v1.Spec for the given spec
func ReadSpecFromCloudConfig(r *sdkConfig.Config, spec string) (sdkSpec.Spec, error) {
	var sp sdkSpec.Spec
	var err error

	switch spec {
	case "install":
		sp, err = NewInstallSpec(r)
	case "upgrade":
		sp, err = NewUpgradeSpec(r)
	case "reset":
		sp, err = NewResetSpec(r)
	case "install-uki":
		sp, err = NewUkiInstallSpec(r)
	case "reset-uki":
		sp, err = NewUkiResetSpec(r)
	case "upgrade-uki":
		sp, err = NewUkiUpgradeSpec(r)
	default:
		return nil, fmt.Errorf("spec not valid: %s", spec)
	}
	if err != nil {
		return nil, fmt.Errorf("failed initializing spec: %v", err)
	}

	err = sp.Sanitize()
	if err != nil {
		return sp, fmt.Errorf("sanitizing the %s spec: %w", spec, err)
	}

	r.Logger.Debugf("Loaded %s spec: %s", spec, litter.Sdump(sp))
	return sp, nil
}

var decodeHook = viper.DecodeHook(
	mapstructure.ComposeDecodeHookFunc(
		UnmarshalerHook(),
		mapstructure.StringToTimeDurationHookFunc(),
		mapstructure.StringToSliceHookFunc(","),
	),
)

type Unmarshaler interface {
	CustomUnmarshal(interface{}) (bool, error)
}

func UnmarshalerHook() mapstructure.DecodeHookFunc {
	return func(from reflect.Value, to reflect.Value) (interface{}, error) {
		// get the destination object address if it is not passed by reference
		if to.CanAddr() {
			to = to.Addr()
		}
		// If the destination implements the unmarshaling interface
		u, ok := to.Interface().(Unmarshaler)
		if !ok {
			return from.Interface(), nil
		}
		// If it is nil and a pointer, create and assign the target value first
		if to.IsNil() && to.Type().Kind() == reflect.Ptr {
			to.Set(reflect.New(to.Type().Elem()))
			u = to.Interface().(Unmarshaler)
		}
		// Call the custom unmarshaling method
		cont, err := u.CustomUnmarshal(from.Interface())
		if cont {
			// Continue with the decoding stack
			return from.Interface(), err
		}
		// Decoding finalized
		return to.Interface(), err
	}
}

func setDecoder(config *mapstructure.DecoderConfig) {
	// Make sure we zero fields before applying them, this is relevant for slices
	// so we do not merge with any already present value and directly apply whatever
	// we got form configs.
	config.ZeroFields = true
}

// BootedFrom will check if we are booting from the given label
func BootedFrom(runner sdkRunner.Runner, label string) bool {
	out, _ := runner.Run("cat", "/proc/cmdline")
	return strings.Contains(string(out), label)
}

// HasSquashedRecovery returns true if a squashed recovery image is found in the system
func hasSquashedRecovery(config *sdkConfig.Config, recovery *sdkPartitions.Partition) (squashed bool, err error) {
	mountPoint := recovery.MountPoint
	if mnt, _ := isMounted(config, recovery); !mnt {
		tmpMountDir, err := fsutils.TempDir(config.Fs, "", "elemental")
		if err != nil {
			config.Logger.Errorf("failed creating temporary dir: %v", err)
			return false, err
		}
		defer config.Fs.RemoveAll(tmpMountDir) // nolint:errcheck
		err = config.Mounter.Mount(recovery.Path, tmpMountDir, "auto", []string{})
		if err != nil {
			config.Logger.Errorf("failed mounting recovery partition: %v", err)
			return false, err
		}
		mountPoint = tmpMountDir
		defer func() {
			err = config.Mounter.Unmount(tmpMountDir)
			if err != nil {
				squashed = false
			}
		}()
	}
	return fsutils.Exists(config.Fs, filepath.Join(mountPoint, "cOS", constants.RecoverySquashFile))
}

func isMounted(config *sdkConfig.Config, part *sdkPartitions.Partition) (bool, error) {
	if part == nil {
		return false, fmt.Errorf("nil partition")
	}

	if part.MountPoint == "" {
		return false, nil
	}
	// Using IsLikelyNotMountPoint seams to be safe as we are not checking
	// for bind mounts here
	notMnt, err := config.Mounter.IsLikelyNotMountPoint(part.MountPoint)
	if err != nil {
		return false, err
	}
	return !notMnt, nil
}

func unmarshallFullSpec(r *sdkConfig.Config, subkey string, sp sdkSpec.Spec) error {
	// Load the config into viper from the raw cloud config string
	ccString, err := r.Collector.String()
	if err != nil {
		return fmt.Errorf("failed initializing spec: %w", err)
	}
	viper.SetConfigType("yaml")
	viper.ReadConfig(strings.NewReader(ccString))
	vp := viper.Sub(subkey)
	if vp == nil {
		vp = viper.New()
	}

	err = vp.Unmarshal(sp, setDecoder, decodeHook)
	if err != nil {
		return fmt.Errorf("error unmarshalling %s Spec: %w", subkey, err)
	}

	// Check for deprecated URI usage and add warnings
	checkDeprecatedURIUsage(r.Logger, sp)

	return nil
}

// applyAllowInsecureRegistries switches the config's ImageExtractor to one that
// allows pulling from registries served over plain HTTP or presenting an
// untrusted/self-signed TLS certificate, when the given cloud-config subkey
// requests it (install.allow-insecure-registries / upgrade.allow-insecure-registries).
// It is called before any image pull or size calculation so the whole flow honors
// the setting. Only the default OCIImageExtractor is swapped, so extractors
// injected by tests or providers are left untouched.
func applyAllowInsecureRegistries(cfg *sdkConfig.Config, subkey string) error {
	allow, err := readAllowInsecureRegistries(cfg, subkey)
	if err != nil {
		return err
	}
	if !allow {
		return nil
	}
	if _, ok := cfg.ImageExtractor.(imageextractor.OCIImageExtractor); ok {
		cfg.Logger.Infof("Allowing insecure registry pulls via %s.allow-insecure-registries", subkey)
		cfg.ImageExtractor = imageextractor.OCIImageExtractor{Insecure: true}
	}
	return nil
}

// readAllowInsecureRegistries peeks the boolean <subkey>.allow-insecure-registries
// value from the merged cloud config. It uses a fresh viper instance so the global
// viper state used by unmarshallFullSpec is not disturbed.
func readAllowInsecureRegistries(cfg *sdkConfig.Config, subkey string) (bool, error) {
	ccString, err := cfg.Collector.String()
	if err != nil {
		return false, err
	}
	v := viper.New()
	v.SetConfigType("yaml")
	if err := v.ReadConfig(strings.NewReader(ccString)); err != nil {
		return false, err
	}
	sub := v.Sub(subkey)
	if sub == nil {
		return false, nil
	}
	return sub.GetBool("allow-insecure-registries"), nil
}

// checkDeprecatedURIUsage checks if any Image structs in the spec have the deprecated URI field set
// and logs a warning if found, also converts URI to Source for backwards compatibility
func checkDeprecatedURIUsage(logger sdkLogger.KairosLogger, sp sdkSpec.Spec) {
	switch s := sp.(type) {
	case *spec.InstallSpec:
		checkImageURI(&s.Active, logger, "install.system")
		checkImageURI(&s.Recovery, logger, "install.recovery-system")
	case *spec.UpgradeSpec:
		checkImageURI(&s.Active, logger, "upgrade.system")
		checkImageURI(&s.Recovery, logger, "upgrade.recovery-system")
	case *spec.ResetSpec:
		checkImageURI(&s.Active, logger, "reset.system")
	case *spec.InstallUkiSpec:
		checkImageURI(&s.Active, logger, "install-uki.system")
	case *spec.UpgradeUkiSpec:
		checkImageURI(&s.Active, logger, "upgrade-uki.system")
	}
}

// checkImageURI checks if an Image has the deprecated URI field set and handles the conversion
func checkImageURI(img *sdkImages.Image, logger sdkLogger.KairosLogger, fieldPath string) {
	if img.URI != "" {
		logger.Warnf("The 'uri' field in %s is deprecated, please use 'source' instead", fieldPath)
		if img.Source == nil || img.Source.IsEmpty() {
			if source, err := sdkImages.NewSrcFromURI(img.URI); err == nil {
				img.Source = source
			}
		}
	}
}

// detectLargestDevice returns the largest disk found
func detectLargestDevice() string {
	preferedDevice := "/dev/sda"
	maxSize := float64(0)

	for _, disk := range ghw.GetDisks(ghw.NewPaths(""), nil) {
		// Skip device-mapper devices (multipath maps, LVM volumes). They are
		// not valid install targets: the agent deactivates them through
		// blkdeactivate right before partitioning, so installing onto one
		// would fail with a missing device. Multipath claims any disk
		// exposing a WWID (even single-path ones), shadowing the real disk
		// with a dm-N node of the exact same size.
		if strings.HasPrefix(disk.Name, "dm-") {
			continue
		}
		size := float64(disk.SizeBytes) / float64(GiB)
		if size > maxSize {
			maxSize = size
			preferedDevice = "/dev/" + disk.Name
		}
	}

	return preferedDevice
}

// DetectPreConfiguredDevice returns a disk that has partitions labeled with
// Kairos labels. It can be used to detect a pre-configured device.
func DetectPreConfiguredDevice(logger sdkLogger.KairosLogger) (string, error) {
	for _, disk := range ghw.GetDisks(ghw.NewPaths(""), &logger) {
		for _, p := range disk.Partitions {
			if p.FilesystemLabel == "COS_STATE" {
				return filepath.Join("/", "dev", disk.Name), nil
			}
		}
	}

	return "", nil
}
