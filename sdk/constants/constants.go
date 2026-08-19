// Package constants provides constant values used across the kairos projects.
package constants

const (
	BiosPartName       = "bios"           // Name of the BIOS partition
	EfiPartName        = "efi"            // Name of the EFI partition
	EfiLabel           = "COS_GRUB"       // Label for the EFI filesystem
	RecoveryLabel      = "COS_RECOVERY"   // Label for the RECOVERY filesystem
	RecoveryPartName   = "recovery"       // Name of the RECOVERY partition
	StateLabel         = "COS_STATE"      // Label for the STATE filesystem
	StatePartName      = "state"          // Name of the STATE partition
	PersistentLabel    = "COS_PERSISTENT" // Label for the PERSISTENT filesystem
	PersistentPartName = "persistent"     // Name of the PERSISTENT partition
	OEMLabel           = "COS_OEM"        // Label for the OEM filesystem
	OEMPartName        = "oem"            // Name of the OEM partition
	PassiveLabel       = "COS_PASSIVE"    // Label for the PASSIVE filesystem
	SystemLabel        = "COS_SYSTEM"     // Label for the SYSTEM filesystem
	ActiveLabel        = "COS_ACTIVE"     // Label for the ACTIVE filesystem

	EfiFs                = "vfat"              // Filesystem type for EFI partition
	EfiDirTransient      = "/run/cos/efi"      // Transient mount point for EFI partition
	RecoveryDirTransient = "/run/cos/recovery" // Transient mount point for RECOVERY partition
	RecoveryImgFile      = "recovery.img"      // Recovery image file name
	GPT                  = "gpt"               // Partition table type GPT
	MSDOS                = "msdos"             // Partition table type MSDOS
	BIOS                 = "bios"              // Firmware type BIOS

	EFI            = "efi"       // Firmware type EFI
	ESPFLAG        = "esp"       // esp flag for EFI partition
	BIOSFLAG       = "bios_grub" // bios_grub flag for BIOS partition
	LinuxImgFs     = "ext2"      // Default filesystem type for Linux IMAGES. Used for active/passive/recovery.img
	LinuxFs        = "ext4"      // Default filesystem type for Linux PARTITIONS
	EfiSize        = uint(64)    // Size of the EFI partition in MiB by default
	BiosSize       = uint(1)     // Size of the BIOS partition in MiB by default
	OEMSize        = uint(64)    // Size of the OEM partition in MiB by default
	PersistentSize = uint(0)     // Size of the PERSISTENT partition in MiB by default. Set to 0 so its expanded to fill remaining space
	ImgSize        = uint(3072)  // Size of the image files in MiB by default. For active/passive/recovery.img
)

// Installer binary locations. These define the contract, shared across kairos
// projects, for where the installer lives in an image: kairos-init bundles the
// default and kairos-agent execs whichever is resolved at install time. The
// resolution helpers live in the installer package.
const (
	InstallerDefaultPath  = "/system/installer/kairos-installer" // Default installer path, bundled by kairos-init
	InstallerOverridePath = "/system/installer/installer"        // Override slot the base image or user can drop their own installer into; takes precedence over the default
	InstallerEnvVar       = "KAIROS_INSTALLER"                   // Env var that, when set to an existing path, overrides both installer locations
)

// Agent binary location. kairos-init installs kairos-agent to AgentDefaultPath;
// kairos-agent systemd units and phone-home services must use the same path.
// Resolution helpers live in the agentrun package.
const (
	AgentBinName     = "kairos-agent"
	AgentDefaultPath = "/usr/bin/kairos-agent"
	AgentEnvVar      = "KAIROS_AGENT_BIN" // Env var override for dev/tests; must point at an existing binary
)
