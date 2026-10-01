package constants

import (
	"errors"
	"os"
	"path"

	sdkConstants "github.com/kairos-io/kairos/v4/sdk/constants"
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
		"dm_snapshot",
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
	// OpPersistentSnapshot stacks a copy-on-write snapshot over the read-only
	// persistent partition so it can be mounted read-write, with the changes
	// held in RAM. Only registered when the media is write-protected, so a
	// writable install keeps exactly the graph it had. See
	// MountPersistentSnapshotDagStep.
	OpPersistentSnapshot = "persistent-snapshot"
	// InRAMSentinelName is the extra sentinel file written under /run/cos/ when
	// the kairos.ram workflow is active. It is additive: WriteSentinelDagStep
	// still writes the BootState-driven sentinel (which is active_mode for
	// in-RAM boots because kairos-sdk forces BootState=Active) so existing
	// cloud-init gates keep firing. Tooling that specifically needs to know the
	// rootfs is on a tmpfs can stat this file.
	InRAMSentinelName = "in_ram_mode"

	// Read-only media boot. A unit is installed on a writable disk and then
	// write-protected in hardware, so from that point on every boot sees a block
	// device the kernel refuses writes to. immucore then puts a device-mapper
	// snapshot over the persistent partition, with its copy-on-write store on a
	// tmpfs, and mounts that read-write in the partition's place: unchanged
	// blocks read from the disk, changed blocks live in RAM until power-off,
	// and the filesystem above is an ordinary writable ext4, which is what a
	// container runtime's own overlayfs needs underneath it.
	//
	// CmdlineWriteProtected is the gate. Absent or "=0", the layout is never
	// applied and the device is not asked; present, the device decides;
	// "=force" applies it without asking. Matched as an exact token, the way
	// ParseAutoCreateDisk matches its stanza, so a typo that merely starts
	// with the key cannot switch the layout on by accident.
	CmdlineWriteProtected = "rd.immucore.write_protected"
	// WriteProtectedSentinelName is the extra sentinel written under /run/cos/ when
	// the media is write-protected. Additive, exactly like InRAMSentinelName:
	// the BootState sentinel is still written, so existing cloud-init gates keep
	// firing, and a stage that must not write to the disk gates on this one.
	// Defined in the SDK because kairos-agent reads the same file back into its
	// runtime state, and the writer and the reader must not drift.
	WriteProtectedSentinelName = sdkConstants.WriteProtectedSentinelName
	// CmdlineCow sizes the tmpfs that backs the snapshot's copy-on-write store,
	// as a tmpfs:<size> spec like rd.immucore.overlay=. Nested under the mode
	// it belongs to, the way dracut nests rd.live.overlay.size under
	// rd.live.overlay, so the name itself says it does nothing on a writable
	// disk. Absent, the store is sized like the base overlay. WRITE_PROTECTED_COW
	// in cos-layout.env is the same knob from a cloud-config.
	CmdlineCow = "rd.immucore.write_protected.cow="
	// PersistentCowDir is the tmpfs the store lives on, and PersistentCowFile
	// the sparse file inside it that device-mapper writes changed chunks to.
	// Under /run because that carries over switch_root, so the loop device and
	// the snapshot stay valid in the booted system.
	PersistentCowDir  = "/run/immucore/cow"
	PersistentCowFile = "/run/immucore/cow/persistent.cow"
	// PersistentSnapshotName is the device-mapper name of the snapshot; the
	// agent reads its fill level back through it. Defined in the SDK.
	PersistentSnapshotName = sdkConstants.PersistentSnapshotName
	// OverlayBaseDir is the tmpfs that backs the ephemeral RW_PATHS overlays'
	// upper and work directories. Sized by OVERLAY in cos-layout.env, default
	// tmpfs:25%.
	OverlayBaseDir = "/run/overlay"

	// OpEncryptPending runs on the normal boot DAG, gated behind
	// kcrypt.encrypt_on_boot, and encrypts partitions that the configuration
	// marks for encryption but that are still plaintext on disk, before
	// anything mounts them. See kairos-io/kairos#4556.
	OpEncryptPending = "encrypt-pending"

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
