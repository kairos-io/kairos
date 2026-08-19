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

package constants

import (
	"errors"
	"os"
	"strings"

	"github.com/gofrs/uuid"
)

const (
	GrubConf                     = "/etc/cos/grub.cfg"
	GrubOEMEnv                   = "grub_oem_env"
	GrubEnv                      = "grubenv" // grubenv is a file used by grub to store environment variables. Goes into OEM so its runtime modifiable
	GrubDefEntry                 = "Kairos"
	DefaultTty                   = "tty1"
	BiosPartName                 = "bios"
	EfiLabel                     = "COS_GRUB"
	EfiPartName                  = "efi"
	ActiveLabel                  = "COS_ACTIVE"
	PassiveLabel                 = "COS_PASSIVE"
	SystemLabel                  = "COS_SYSTEM"
	RecoveryLabel                = "COS_RECOVERY"
	RecoveryPartName             = "recovery"
	StateLabel                   = "COS_STATE"
	StatePartName                = "state"
	PersistentLabel              = "COS_PERSISTENT"
	PersistentPartName           = "persistent"
	OEMLabel                     = "COS_OEM"
	OEMPartName                  = "oem"
	MountBinary                  = "/usr/bin/mount"
	EfiDevice                    = "/sys/firmware/efi"
	LinuxImgFs                   = "ext2"
	SquashFs                     = "squashfs"
	HTTPTimeout                  = 60
	LiveDir                      = "/run/initramfs/live"
	RecoveryDir                  = "/run/cos/recovery"
	StateDir                     = "/run/cos/state"
	OEMDir                       = "/run/cos/oem"
	PersistentDir                = "/run/cos/persistent"
	ActiveDir                    = "/run/cos/active"
	TransitionDir                = "/run/cos/transition"
	EfiDir                       = "/run/cos/efi"
	RecoverySquashFile           = "recovery.squashfs"
	IsoRootFile                  = "rootfs.squashfs"
	ActiveImgFile                = "active.img"
	PassiveImgFile               = "passive.img"
	RecoveryImgFile              = "recovery.img"
	IsoBaseTree                  = "/run/rootfsbase"
	AfterInstallChrootHook       = "after-install-chroot"
	AfterInstallHook             = "after-install"
	BeforeInstallHook            = "before-install"
	AfterResetChrootHook         = "after-reset-chroot"
	AfterResetHook               = "after-reset"
	BeforeResetHook              = "before-reset"
	AfterUpgradeChrootHook       = "after-upgrade-chroot"
	AfterUpgradeHook             = "after-upgrade"
	BeforeUpgradeHook            = "before-upgrade"
	TransitionImgFile            = "transition.img"
	RunningStateDir              = "/run/initramfs/cos-state" // TODO: converge this constant with StateDir/RecoveryDir in dracut module from cos-toolkit
	RunningRecoveryStateDir      = "/run/initramfs/isoscan"   // TODO: converge this constant with StateDir/RecoveryDir in dracut module from cos-toolkit
	FailInstallationFileSentinel = "/run/cos/fail_installation"
	ActiveImgName                = "active"
	PassiveImgName               = "passive"
	RecoveryImgName              = "recovery"
	StateResetImgName            = "statereset"
	UsrLocalPath                 = "/usr/local"
	OEMPath                      = "/oem"
	BootEntryRecovery            = "recovery"
	BootEntryActive              = "cos"

	// SELinux targeted policy paths
	SELinuxTargetedPath        = "/etc/selinux/targeted"
	SELinuxTargetedContextFile = SELinuxTargetedPath + "/contexts/files/file_contexts"
	SELinuxTargetedPolicyPath  = SELinuxTargetedPath + "/policy"

	// Default directory and file fileModes
	DirPerm        = os.ModeDir | os.ModePerm
	FilePerm       = 0666
	ConfigPerm     = 0640 // Used for config files that contain secrets or other sensitive data
	NoWriteDirPerm = 0555 | os.ModeDir
	TempDirPerm    = os.ModePerm | os.ModeSticky | os.ModeDir

	// Eject script
	EjectScript = "#!/bin/sh\n/usr/bin/eject -rmF"

	ArchAmd64   = "amd64"
	Archx86     = "x86_64"
	ArchArm64   = "arm64"
	ArchRiscv64 = "riscv64"
	SignedShim  = "shim.efi"
	Rsync       = "rsync"

	UkiEfiDir     = "/efi"
	UkiMaxEntries = 3

	// Boot labeling
	PassiveBootSuffix    = " (fallback)"
	RecoveryBootSuffix   = " recovery"
	StateResetBootSuffix = " state reset (auto)"

	// Error
	UpgradeNoSourceError           = "could not find a proper source for the upgrade.\nThis can be configured in the cloud config files under the 'upgrade.system.uri' key or via cmdline using the '--source' flag"
	UpgradeRecoveryNoSourceError   = "could not find a proper source for the recovery upgrade.\nThis can be configured in the cloud config files under the 'upgrade.recovery-system.uri' key or via cmdline using the '--source' flag"
	MultipleEntriesAssessmentError = "multiple boot entries found for %s"
	NoBootAssessmentWarning        = "No boot assessment found in current boot entry config file"

	DefaultWebUIListenAddress = ":8080"
)

func UkiDefaultMenuEntries() []string {
	return []string{"cos", "fallback", "recovery", "statereset"}
}

func UkiDefaultSkipEntries() []string {
	return []string{"interactive-install", "install-mode-interactive"}
}

func GetCloudInitPaths() []string {
	return []string{"/system/oem", "/oem/", "/usr/local/cloud-config/"}
}

// GetDefaultSquashfsOptions returns the default options to use when creating a squashfs
func GetDefaultSquashfsOptions() []string {
	return []string{"-b", "1024k"}
}

func GetDefaultSquashfsCompressionOptions() []string {
	return []string{"-comp", "gzip"}
}

func GetFallBackEfi(arch string) string {
	switch arch {
	case ArchArm64:
		return "bootaa64.efi"
	case ArchRiscv64:
		return "BOOTRISCV64.EFI"
	default:
		return "bootx64.efi"
	}
}

// GetGrubFonts returns the default font files for grub
func GetGrubFonts() []string {
	return []string{"ascii.pf2", "euro.pf2", "unicode.pf2"}
}

// GetGrubModules returns the default module files for grub
func GetGrubModules() []string {
	return []string{"loopback.mod", "squash4.mod", "xzio.mod", "gzio.mod", "regexp.mod"}
}

// GetYipConfigDirs returns all the directories where "yip" configuration might
// live. These include the directories were we store our system overlay files:
// https://github.com/kairos-io/packages/tree/main/packages/static/kairos-overlay-files/files/system/oem
func GetYipConfigDirs() []string {
	return append(GetUserConfigDirs(), "/system/oem")
}

// GetUserConfigDirs returns all the directories that might have configuration
// supplied by the user. They are in an order which allows the users to override
// baked-in configuration (e.g. in livecd, under /run/initramfs/live) with
// configuration coming from datasource (e.g. a datasource cdrom, written under /oem).
// That's why writable paths are last.
func GetUserConfigDirs() []string {
	return []string{
		"/run/initramfs/live",
		"/etc/kairos", // Default system configuration file https://github.com/kairos-io/kairos/issues/2221
		"/usr/local/cloud-config",
		"/oem",
	}
}

func BaseBootTitle(title string) string {
	if strings.HasSuffix(title, RecoveryBootSuffix) {
		return strings.TrimSuffix(title, RecoveryBootSuffix)
	} else if strings.HasSuffix(title, PassiveBootSuffix) {
		return strings.TrimSuffix(title, PassiveBootSuffix)
	} else if strings.HasSuffix(title, StateResetBootSuffix) {
		return strings.TrimSuffix(title, StateResetBootSuffix)
	}
	return title
}

func BootTitleForRole(role, title string) (string, error) {
	switch role {
	case ActiveImgName:
		return BaseBootTitle(title), nil
	case PassiveImgName:
		return BaseBootTitle(title) + PassiveBootSuffix, nil
	case RecoveryImgName:
		return BaseBootTitle(title) + RecoveryBootSuffix, nil
	case StateResetImgName:
		return BaseBootTitle(title) + StateResetBootSuffix, nil
	default:
		return "", errors.New("invalid role")
	}
}

// DiskUUID is the static UUID for main disk identification
var DiskUUID = uuid.NewV5(uuid.NamespaceURL, "KAIROS_DISK").String()
