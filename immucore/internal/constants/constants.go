package constants

import (
	"errors"
	"os"
	"path"
	"time"
)

func DefaultRWPaths() []string {
	// Default RW_PATHS to mount if not override by the cos-layout.env file
	return []string{"/etc", "/root", "/home", "/opt", "/srv", "/usr/local", "/var"}
}

func GetCloudInitPaths() []string {
	return []string{"/system/oem", "/oem/", "/usr/local/cloud-config/"}
}

func TPMKernelModules() []string {
	return []string{
		"tpm_ftpm_tee",
	}
}

// GenericKernelDrivers returns a list of generic kernel drivers to insmod during uki mode
// as they could be useful for a lot of situations.
func GenericKernelDrivers() []string {
	return []string{
		"af_packet",
		"ahci",
		"ahcpi-platform",
		"ata_generic",
		"ata_piix",
		"cdrom",
		"dm_mod",
		"dm-verity",
		"e1000",
		"e1000e",
		"ehci_hcd",
		"ehci_pci",
		"ext2",
		"ext4",
		"fat",
		"fuse",
		"hid-generic",
		"iso9660",
		"isofs",
		"libahci-platform",
		"libata",
		"loop",
		"mmc_block", // mmc block device support
		"nls_cp437",
		"nls_iso8859_1",
		"nvme",
		"nvme_core",
		"ohci_hcd",
		"ohci_pci",
		"overlay",
		"paride",
		"part_msdos",
		"pata_acpi",
		"scsi_mod",
		"sd_mod",
		"sdhci-pci", // some mmc devices seems to use this like the raxda x4
		"simpledrm",
		"squashfs",
		"sr_mod",
		"uas",
		"uhci_hcd",
		"usb_common",
		"usbcore",
		"usbhid",
		"usbms",
		"usb_storage",
		"vfat",
		"virtio",
		"virtio_blk",
		"virtio_net",
		"virtio_pci",
		"virtio_scsi",
		"xhci_hcd",
		"xhci_pci",
		"nfit",               // For http boot NFIT memory mapping
		"libnvdimm",          // For http boot NFIT memory mapping
		"nd_pmem",            // For http boot NFIT memory mapping
		"dax_pmem",           // For http boot NFIT memory mapping
		"tegra-bpmp",         // For Thor
		"tegra-bpmp-thermal", // For Thor
		"phy-tegra194-p2u",   // For Thor
		"pcie-tegra264",      // For Thor
	}
}

// bindMountModes holds the mode the mountpoint of a bind mount has to be
// created with when nothing on the machine has created it yet.
//
// A bind mount exposes the inode of the directory that backs it, so the mode
// visible at the mountpoint once it is mounted is the one that directory
// carries, and that one is taken from the mountpoint at the moment the pair is
// first created. Where the image ships the mountpoint, its mode is the answer
// and this is not consulted. Where the image ships nothing, the mode is the
// default of whoever creates the directory first, and the machine then keeps
// it for as long as it lives, so a path whose consumer refuses a laxer mode
// has to say which mode it needs here.
var bindMountModes = map[string]os.FileMode{
	// auditd refuses a trail directory that anyone other than root can read,
	// and no image ships /var/log/audit yet.
	AuditLogPath: 0o700,
}

// BindMountMode returns the mode a bind mountpoint has to be created with, and
// whether the path asks for a particular one at all. A leading slash is
// optional, the bind mount code strips it off the paths it handles.
func BindMountMode(mountpoint string) (os.FileMode, bool) {
	mode, ok := bindMountModes[path.Join("/", mountpoint)]
	return mode, ok
}

var ErrAlreadyMounted = errors.New("already mounted")

// ErrMountTargetMissing is returned when a mount target directory does not exist
// and cannot be created, typically because it is missing from the OS image and
// the rootfs is still mounted read-only at that point in the boot.
var ErrMountTargetMissing = errors.New("mount target does not exist and could not be created")

const (
	OpCustomMounts         = "custom-mount"
	OpDiscoverState        = "discover-state"
	OpMountState           = "mount-state"
	OpMountBind            = "mount-bind"
	OpMountRoot            = "mount-root"
	OpOverlayMount         = "overlay-mount"
	OpWriteFstab           = "write-fstab"
	OpMountBaseOverlay     = "mount-base-overlay"
	OpMountOEM             = "mount-oem"
	OpRootfsHook           = "rootfs-hook"
	OpInitramfsHook        = "initramfs-hook"
	OpLoadConfig           = "load-config"
	OpMountTmpfs           = "mount-tmpfs"
	OpUkiInit              = "uki-init"
	OpSentinel             = "create-sentinel"
	OpUkiUdev              = "uki-udev"
	OpUkiBaseMounts        = "uki-base-mounts"
	OpUkiPivotToSysroot    = "uki-pivot-to-sysroot"
	OpUkiTPMKernelModules  = "uki-tpm-modules"
	OpUkiKernelModules     = "uki-kernel-modules"
	OpUkiNetwork           = "uki-network"
	OpWaitForSysroot       = "wait-for-sysroot"
	OpLvmActivate          = "lvm-activation"
	OpKcryptUnlock         = "unlock-all"
	OpKcryptUpgrade        = "upgrade-kcrypt"
	OpUkiKcrypt            = "uki-unlock"
	OpUkiMountLivecd       = "mount-livecd"
	OpUkiExtractCerts      = "extract-certs"
	OpUkiTransitionSysext  = "uki-transition-sysext"
	OpUkiCopySysExtensions = "enable-sysext-confext"
	// OpQuarantineStaleUnits moves unit symlinks that an earlier image left in
	// the persistent /etc/systemd bind and that now shadow a packaged unit out
	// of the unit load path. See internalUtils.QuarantineStaleUnitSymlinks.
	OpQuarantineStaleUnits = "quarantine-stale-units"

	// UkiNetworkTimeout bounds how long the UKI initrd waits for an interface
	// to get an address before giving up and letting the unlock step run and
	// report the real failure. A DHCP exchange on a healthy link takes a
	// couple of seconds; this leaves room for a slow switch without adding a
	// visible stall to a boot whose network is simply absent.
	UkiNetworkTimeout = 30 * time.Second
	// InRAMSentinelName is the extra sentinel file written under /run/cos/ when
	// the kairos.ram workflow is active. It is additive: WriteSentinelDagStep
	// still writes the BootState-driven sentinel (which is active_mode for
	// in-RAM boots because kairos-sdk forces BootState=Active) so existing
	// cloud-init gates keep firing. Tooling that specifically needs to know the
	// rootfs is on a tmpfs can stat this file.
	InRAMSentinelName = "in_ram_mode"

	// OpEnsurePartitions runs early in the in-RAM DAG and either confirms that
	// COS_OEM + COS_PERSISTENT already exist on disk, or auto-creates the
	// missing ones on the disk selected via kairos.ram.create_partitions.
	// After it completes, downstream mount steps behave as if the workstation
	// had been installed normally with an empty OEM.
	OpEnsurePartitions = "ensure-partitions"

	// Partition labels and default sizes come from kairos-sdk/constants
	// (OEMLabel, PersistentLabel, OEMSize, PersistentSize) — do not duplicate
	// them here.

	// Cmdline stanzas driving the ensure-partitions step. All live under the
	// kairos.ram.* namespace so they sit next to the kairos.ram flag that
	// enables the in-RAM workflow in the first place. See
	// EnsurePartitionsDagStep for the exact semantics of each.
	CmdlineAutoCreatePartitions     = "kairos.ram.create_partitions"
	CmdlineAutoCreatePartitionsWipe = "kairos.ram.wipe"
	CmdlineAutoCreateOemSize        = "kairos.ram.oem="
	CmdlineAutoCreatePersistentSize = "kairos.ram.persistent="
	UkiLivecdMountPoint             = "/run/initramfs/live"
	UkiIsoBaseTree                  = "/run/rootfsbase"
	UkiIsoBootImage                 = "efiboot.img"
	UkiLivecdPath                   = "/dev/disk/by-label/UKI_ISO_INSTALL"
	UkiDefaultcdrom                 = "/dev/sr0"
	UkiDefaultcdromFsType           = "iso9660"
	UkiDefaultEfiimgFsType          = "vfat"
	UkiSysrootDir                   = "sysroot"
	PersistentStateTarget           = "/usr/local/.state"
	LogDir                          = "/run/immucore"
	PathAppend                      = "/usr/bin:/usr/sbin:/bin:/sbin"
	PATH                            = "PATH"
	DefaultPCR                      = 11
	SysExt                          = "sysext"
	ConfExt                         = "confext"
	SourceSysExtDir                 = "/var/lib/kairos/extensions/"
	SourceConfExtDir                = "/var/lib/kairos/confexts/"
	DestSysExtDir                   = "/run/extensions"
	DestConfExtDir                  = "/run/confexts"
	VerityCertDir                   = "/run/verity.d/"
	SysextDefaultPolicy             = "--image-policy=\"root=signed+absent:usr=signed+absent\""
	EfiDir                          = "/efi"

	// AuditLogPath is the kernel audit log directory. auditd keeps the audit
	// trail here, so it has to be backed by the persistent partition rather
	// than by the ephemeral /var overlay. It is one of the persistent bind
	// mounts, see LoadEnvLayoutDagStep.
	AuditLogPath = "/var/log/audit"

	// CmdlineBreak requests dracut-style breakpoints. Its values are step
	// names, i.e. the Op* constants above (rd.immucore.break=mount-root), and
	// several can be given either comma-separated or by repeating the stanza.
	// Immucore stops right before a named step, hands the console to a shell
	// and resumes the boot once that shell exits.
	CmdlineBreak = "rd.immucore.break="
)
