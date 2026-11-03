package constants

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
)

// Eltorito image is basically a grub cdboot.img + grub core image
// So basically you build a grub image with modules embedded and prepend it with the cdboot, which allows it to boot from
// CDRom and run the grub embedded in the image directly from BIOS
// This can be generated from any distro by runing something like:
/*
grub2-mkimage -O i386-pc -o core.img -p /boot/grub2 -d PATH_TO_i386_MODULES ext2 iso9660 linux echo configfile search_label search_fs_file search search_fs_uuid ls normal gzio gettext font gfxterm gfxmenu all_video test true loadenv part_gpt part_msdos biosdisk vga vbe chain boot
cat $(find / -name cdboot.img -print) core.img > eltorito.img

Important things in the grub image creation:
 - -O i386-pc is the architecture we want to build the image for. Bios is i386
 - -p is the prefix dir, this is where grub will start searching for things, including the grub.cfg config, when it boots
 - -d is the current dir where modules and images are. Usually this is automatically set so it can be dropped
 - the list at the end are the modules to bundle for grub. Honestly the list is not too big and it can probably be dropped to like half for the livecd
   as it only uses linux, echo, font, video ones and boot. But it doesnt hurt to have extra modules.
*/
//go:embed eltorito.img
var Eltorito []byte

// BootHybrid is boot_hybrid.img which comes bundled with grub
// Its ASM to boot from the grub image embedded
// You can check its source here: https://github.com/rhboot/grub2/blob/fedora-39/grub-core/boot/i386/pc/cdboot.S
//
//go:embed boot_hybrid.img
var BootHybrid []byte

// GrubLiveBiosCfg is the livecd config for BIOS boot
//
//go:embed grub_live_bios.cfg
var GrubLiveBiosCfg []byte

// Ids of the live menu entries in grub_live_bios.cfg, as given by their
// `--id`. Grub resolves `set default` against ids before titles, and it
// truncates the value at the first space while doing so
// (grub-core/normal/menu.c, get_entry_number_helper), so a title is not
// usable here: "Kairos (install)" would match an entry whose id is
// "Kairos" and boot that one instead. Ids carry no spaces for that reason.
//
// These strings are part of the template's contract. Renaming an id in the
// .cfg without renaming the constant leaves `default` naming nothing, and
// grub silently falls back to the first entry.
const (
	// LiveGrubEntryInstall runs the installer. It is one entry, not one per
	// install style: `kairos-agent interactive-install` is a dispatcher, so
	// it runs AutoInstall first and installs unattended when the config says
	// `install.auto: true`, and otherwise shows the welcome page, from which
	// the user reaches the TUI, the WebUI or a shell. The UKI path has
	// shipped a single entry (`norole`) for the same reason; this is the
	// GRUB equivalent, and it is what a live ISO boots when the build does
	// not name another entry.
	LiveGrubEntryInstall = "kairos-install"
	// LiveGrubEntryBootLocal chainloads the system already installed on
	// the disk (cmdline `kairos.boot_live_mode`).
	LiveGrubEntryBootLocal = "kairos-boot-local"
	// LiveGrubEntryDebug boots the live system with the initrd debug
	// shell and immucore debug logging.
	LiveGrubEntryDebug = "kairos-debug"
)

// LiveGrubEntries lists every id a build may name as the default live entry.
// Grub falls back to the first entry when `set default` matches nothing, so
// an id outside this list does not fail the boot, it silently boots the first
// entry. The build refuses such a value instead.
var LiveGrubEntries = []string{
	LiveGrubEntryInstall,
	LiveGrubEntryBootLocal,
	LiveGrubEntryDebug,
}

type UkiOutput string

const (
	KairosDefaultArtifactName = "kairos"
	IsoEFIPath                = "/boot/uefi.img"
	EfiBootPath               = "/EFI/BOOT"
	EfiLabel                  = "EFI_LIVE"
	EfiFs                     = "vfat"
	IsoRootFile               = "rootfs.squashfs"
	ISOLabel                  = "COS_LIVE"
	ShimEfiDest               = EfiBootPath + "/bootx64.efi"
	ShimEfiArmDest            = EfiBootPath + "/bootaa64.efi"
	ShimEfiRiscv64Dest        = EfiBootPath + "/bootriscv64.efi"
	BuildImgName              = "elemental"
	GrubCfg                   = "grub.cfg"
	GrubPrefixDir             = "/boot/grub2"
	// CdGrubPrefixDir is the prefix baked into Debian/Ubuntu's CD grub
	// binary (gcdx64.efi.signed / gcdaa64.efi.signed). When we ship the CD
	// grub on an ISO we must place a grub.cfg at this path so it can find
	// its config regardless of what the firmware sets $root to.
	CdGrubPrefixDir = "/boot/grub"
	GrubEfiCfg      = "search --no-floppy --file --set=root " + IsoKernelPath +
		"\nset prefix=($root)" + GrubPrefixDir +
		"\nconfigfile $prefix/" + GrubCfg

	// GrubEfiRecovery Used for RAW images as we chainload the grub config in the recovery partition
	GrubEfiRecovery = "search --no-floppy --label --set=root COS_RECOVERY" +
		"\nset root=($root)" +
		"\nset prefix=($root)/grub2\n" +
		"configfile ($root)/etc/cos/grub.cfg"
	// GrubEfiState Used for boot-active RAW images, chainloads the grub config in the state partition like an install does
	GrubEfiState = "search --no-floppy --label --set=root COS_STATE" +
		"\nset prefix=($root)/grub2" +
		"\nconfigfile ($root)/grub2/grub.cfg"
	IsoBootCatalog = "/boot/boot.catalog"
	IsoHybridMBR   = "/boot/boot_hybrid.img"
	IsoBootFile    = "/boot/eltorito.img"

	// These paths are arbitrary but coupled to grub.cfg
	IsoKernelPath = "/boot/kernel"
	IsoInitrdPath = "/boot/initrd"

	// Default directory and file fileModes
	DirPerm        = os.ModeDir | os.ModePerm
	FilePerm       = 0666
	NoWriteDirPerm = 0555 | os.ModeDir
	TempDirPerm    = os.ModePerm | os.ModeSticky | os.ModeDir

	ArchArm64   = "arm64"
	Archx86     = "x86_64"
	ArchAmd64   = "amd64"
	Archaarch64 = "aarch64"
	ArchRiscv64 = "riscv64"

	// UkiCmdline is the cmdline every UKI entry is built from. It deliberately
	// carries no install-mode keyword: on Trusted Boot the cmdline lives in a
	// signed section of the EFI, and the installer copies norole.efi byte for
	// byte into the active, passive, recovery and statereset roles, so anything
	// put here is on the kernel command line of the installed machine for its
	// whole life. A live boot is identified by immucore's
	// /run/cos/uki_install_mode sentinel instead, which is derived from
	// removable-media boot. See kairos-io/kairos#5000.
	//
	// splash is here for the same reason: it has to be, because there is no
	// other way to get it onto a Trusted Boot node. kairos-init installs and
	// enables kairos-splash.service on every systemd image, and that unit is
	// gated on ConditionKernelCommandLine=splash. On a GRUB system the token
	// comes from BootArgsCfg; Trusted Boot does not read that file, so without
	// this the unit is skipped on every boot of every UKI image. The token
	// turns on the booted-system half only, which covers switch-root to the
	// login prompt. The initramfs half cannot run on Trusted Boot at all: a
	// UKI initrd is not built with dracut, so the 50kairos-splash dracut
	// module is never installed in one. See kairos-io/kairos#5285.
	//
	// quiet is deliberately NOT here. kairos-io/kairos#5284 puts it on the
	// GRUB entries that animate, where an operator can drop it at the boot
	// menu. Here it could never be dropped, so a node would lose kernel
	// messages on tty1 and ttyS0 for its whole life. The animation is still
	// escapable without it: ESC streams /dev/kmsg, and
	// `systemctl mask kairos-splash.service` turns it off for good.
	UkiCmdline                    = "console=ttyS0 console=tty1 splash net.ifnames=1 rd.immucore.oemlabel=COS_OEM rd.immucore.oemtimeout=2 rd.immucore.uki selinux=0 panic=5 rd.shell=0 systemd.crash_reboot=yes"
	UkiSystemdBootx86Name         = "systemd-bootx64.efi"
	UkiSystemdBootx86Path         = "/amd/systemd-boot/" + UkiSystemdBootx86Name
	UkiSystemdBootStubx86Name     = "linuxx64.efi.stub"
	UkiSystemdBootStubx86Path     = "/amd/systemd-boot/" + UkiSystemdBootStubx86Name
	UkiSystemdBootArmName         = "systemd-bootaa64.efi"
	UkiSystemdBootArmPath         = "/arm/systemd-boot/" + UkiSystemdBootArmName
	UkiSystemdBootStubArmName     = "linuxaa64.efi.stub"
	UkiSystemdBootStubArmPath     = "/arm/systemd-boot/" + UkiSystemdBootStubArmName
	UkiSystemdBootRiscv64Name     = "systemd-bootriscv64.efi"
	UkiSystemdBootRiscv64Path     = "/riscv64/systemd-boot/" + UkiSystemdBootRiscv64Name
	UkiSystemdBootStubRiscv64Name = "linuxriscv64.efi.stub"
	UkiSystemdBootStubRiscv64Path = "/riscv64/systemd-boot/" + UkiSystemdBootStubRiscv64Name

	EfiFallbackNamex86     = "BOOTX64.EFI"
	EfiFallbackNameArm     = "BOOTAA64.EFI"
	EfiFallbackNameRiscv64 = "BOOTRISCV64.EFI"

	ArtifactBaseName   = "norole"
	DefaultCloudConfig = `#cloud-config
stages:
  initramfs:
    - name: "Set user and password"
      users:
        kairos:
          passwd: "kairos"
          groups:
            - "admin"`
)

const IsoOutput UkiOutput = "iso"
const ContainerOutput UkiOutput = "container"
const DefaultOutput UkiOutput = "uki"

const MB = int64(1024 * 1024)
const GB = 1024 * MB

func OutPutTypes() []string {
	return []string{string(IsoOutput), string(ContainerOutput), string(DefaultOutput)}
}

func GetXorrisoBooloaderArgs(root string) []string {
	args := []string{
		"-boot_image", "grub", fmt.Sprintf("bin_path=%s", IsoBootFile),
		"-boot_image", "grub", fmt.Sprintf("grub2_mbr=%s/%s", root, IsoHybridMBR),
		"-boot_image", "grub", "grub2_boot_info=on",
		"-boot_image", "any", "partition_offset=16",
		"-boot_image", "any", fmt.Sprintf("cat_path=%s", IsoBootCatalog),
		"-boot_image", "any", "cat_hidden=on",
		"-boot_image", "any", "boot_info_table=on",
		"-boot_image", "any", "platform_id=0x00",
		"-boot_image", "any", "emul_type=no_emulation",
		"-boot_image", "any", "load_size=2048",
		"-boot_image", "any", "next",
		"-boot_image", "any", "platform_id=0xef",
		"-boot_image", "any", "emul_type=no_emulation",
		"-append_partition", "2", "0xef", filepath.Join(root, IsoEFIPath),
		"-boot_image", "any", "efi_path=--interval:appended_partition_2:all::",
		// Publish appended partitions in a GPT header (with a protective MBR)
		// so strict UEFI firmwares (VMware ESXi, some OVMF/BMC stacks) that
		// enumerate the ESP via GPT can find and boot the ISO. Without this
		// the ISO carries an MBR-only partition table and boot fails on
		// those firmwares.
		"-boot_image", "any", "appended_part_as=gpt",
		// Preserve the active flag in the protective MBR for legacy BIOSes
		// which refuse to boot an isohybrid image without a bootable entry.
		"-boot_image", "any", "mbr_force_bootable=on",
	}
	return args
}

// GetDefaultSquashfsOptions returns the default options to use when creating a squashfs
func GetDefaultSquashfsOptions() []string {
	return []string{"-b", "1024k"}
}

// Ops constants
const (
	OpPrepareDirs = "prepare-dirs"

	OpDownloadISO     = "download-iso"
	OpCopyCloudConfig = "copy-cloud-config"
	OpInjectCC        = "inject-cloud-config"

	OpStartHTTPServer = "start-httpserver"
	OpStartNetboot    = "start-netboot"

	OpDumpSource     = "dump-source"
	OpGenISO         = "gen-iso"
	OpExtractNetboot = "extract-netboot"

	OpGenEFIRawDisk  = "gen-raw-efi-disk"
	OpGenBIOSRawDisk = "gen-raw-bios-disk"

	OpConvertGCE  = "convert-gce"
	OpConvertVHD  = "convert-vhd"
	OpConvertMAAS = "convert-maas"
)
