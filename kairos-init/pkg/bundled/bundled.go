package bundled

import (
	"embed"
)

//nolint:staticcheck
//go:embed binaries/kairos
var EmbeddedKairos []byte

//go:embed binaries/provider-kairos
var EmbeddedKairosProvider []byte

//go:embed binaries/kairos-installer
var EmbeddedKairosInstaller []byte

//nolint:staticcheck
//go:embed binaries/edgevpn
var EmbeddedEdgeVPN []byte

//go:embed binaries/version-info.yaml
var EmbeddedVersionInfo []byte

// EmbeddedConfigs contains the cloudconfigs that go into /system/oem
//
//go:embed cloudconfigs/*
var EmbeddedConfigs embed.FS

// EmbeddedAlpineInit contains the alpine initramfs config and scripts used to build the alpine initramfs
//
//go:embed alpineInit/*
var EmbeddedAlpineInit embed.FS

// ReconcileScript /usr/bin/cos-setup-reconcile is a script that is used to run the kairos-agent in a loop for the reconcile service
// Mainly for alpine, for systemd distros we user a simple timer unit
// TODO: Check if we can move this into a supervisord script that runs every 300 seconds???
const ReconcileScript = `#!/bin/sh

SLEEP_TIME=${SLEEP_TIME:-360}

while :
do
    kairos-agent run-stage "reconcile"
    sleep "$SLEEP_TIME"
done`

// LogRotateConfig contains the logrotate config for the kairos-agent logs and such
const LogRotateConfig = `/var/log/kairos/*.log {
    create
    daily
    compress
    copytruncate
    missingok
    rotate 3
}`

// DRACUT stuff starts here

// Paths
const (
	DracutPmemPath                = "/etc/dracut.conf.d/kairos-pmem.conf"
	DracutFipsPath                = "/etc/dracut.conf.d/kairos-fips.conf"
	DracutSysextPath              = "/etc/dracut.conf.d/kairos-sysext.conf"
	DracutNetworkPath             = "/etc/dracut.conf.d/kairos-network.conf"
	DracutMultipathPath           = "/etc/dracut.conf.d/kairos-multipath.conf"
	DracutSkipNvidiaDriversPath   = "/etc/dracut.conf.d/kairos-skip-nvidia.conf"
	DracutSkipScsiPath            = "/etc/dracut.conf.d/kairos-skip-scsi.conf"
	DracutXhciRenesasPath         = "/etc/dracut.conf.d/kairos-xhci-renesas.conf"
	DracutConfigPath              = "/etc/dracut.conf.d/99-immucore.conf"
	DracutImmucoreModuleSetupPath = "/usr/lib/dracut/modules.d/28immucore/module-setup.sh"
	DracutImmucoreGeneratorPath   = "/usr/lib/dracut/modules.d/28immucore/generator.sh"
	DracutImmucoreServicePath     = "/usr/lib/dracut/modules.d/28immucore/immucore.service"
)

// ImmucoreConfigDracut /etc/dracut.conf.d/10-immucore.conf is the dracut config file that is used to build the initramfs
const ImmucoreConfigDracut = `hostonly_cmdline="no"
hostonly="no"
compress="xz"
i18n_install_all="yes"
show_modules="yes"
install_items+=" /etc/hosts "
add_dracutmodules+=" livenet dmsquash-live immucore network "
`

// ImmucoreGeneratorDracut is the dracut generator script that is used to generate the sysroot.mount file
// This is used to set a timeout for the sysroot mount and to ensure that the sysroot.mount is properly linked
// Ideally at some point this could be dropped
const ImmucoreGeneratorDracut = `#!/bin/bash

set +x

type getarg >/dev/null 2>&1 || . /lib/dracut-lib.sh

GENERATOR_DIR="$2"
[ -z "$GENERATOR_DIR" ] && exit 1
[ -d "$GENERATOR_DIR" ] || mkdir "$GENERATOR_DIR"

# Add a timeout to the sysroot so it waits a bit for immucore to mount it properly
mkdir -p "$GENERATOR_DIR"/sysroot.mount.d
{
    echo "[Mount]"
    echo "TimeoutSec=300"
} > "$GENERATOR_DIR"/sysroot.mount.d/timeout.conf

# Make sure initrd-root-fs.target depends on sysroot.mount
# This seems to affect mainly ubuntu-22 where initrd-usr-fs depends on sysroot, but it has a broken link to it as sysroot.mount
# is generated under the generator.early dir but the link points to the generator dir.
# So it makes everything else a bit broken if you insert deps in the middle.
# By default other distros seem to do this as it shows on the map page https://man7.org/linux/man-pages/man7/dracut.bootup.7.html
if ! [ -L "$GENERATOR_DIR"/initrd-root-fs.target.wants/sysroot.mount ]; then
  [ -d "$GENERATOR_DIR"/initrd-root-fs.target.wants ] || mkdir -p "$GENERATOR_DIR"/initrd-root-fs.target.wants
  ln -s ../sysroot.mount "$GENERATOR_DIR"/initrd-root-fs.target.wants/sysroot.mount
fi`

// ImmucoreServiceDracut is the dracut service file that is used to run immucore in the initramfs.
//
// Conflicts= carries no ordering of its own, so it alone lets systemd start
// initrd-switch-root.target while immucore is still building the mount DAG
// and stop it at an arbitrary point. The switch then continues into a
// userland whose binds and overlays were never established. Before= pins the
// order: immucore finishes, then the switch runs.
const ImmucoreServiceDracut = `[Unit]
Description=immucore
DefaultDependencies=no
After=systemd-udev-settle.service
Requires=systemd-udev-settle.service
Before=initrd-fs.target
Before=initrd-switch-root.target
Conflicts=initrd-switch-root.target

[Service]
Type=oneshot
RemainAfterExit=yes
StandardOutput=journal+console
ExecStart=/usr/bin/immucore`

// ImmucoreModuleSetupDracut is the dracut module setup script that is used to install the immucore module and deps
const ImmucoreModuleSetupDracut = `#!/bin/bash

# check() is called by dracut to evaluate the inclusion of a dracut module in the initramfs.
# we always want to have this module so we return 0
check() {
    return 0
}

# The function depends() should echo all other dracut module names the module depends on
depends() {
    echo rootfs-block dm fs-lib lvm
    return 0
}

# In installkernel() all kernel related files should be installed
installkernel() {
    instmods overlay
}

# custom function to check if binaries exist before calling inst_multiple
inst_check_multiple() {
    for bin in "$@"; do
        if ! command -v "$bin" >/dev/null 2>&1; then
            derror "Required binary $bin not found!"
            exit 1
        fi
    done
    inst_multiple "$@"
}


# The install() function is called to install everything non-kernel related.
install() {
    declare moddir=${moddir}
    declare systemdutildir=${systemdutildir}
    declare systemdsystemunitdir=${systemdsystemunitdir}

    inst_check_multiple immucore
    # add utils used by yip stages
    inst_check_multiple sync udevadm blkid lsblk e2fsck mount umount rsync cryptsetup gawk awk mkfs.ext2 mkfs.ext3 mkfs.ext4 mkfs.vfat
    # add mkfs.fat using inst_multiple which doesnt check for existence
    # we should remove this as soon as Hadron supports mkfs.fat
    inst_multiple mkfs.fat

    # Install libraries needed by gawk
    inst_libdir_file "libsigsegv.so*"
    inst_libdir_file "libmpfr.so*"

    # missing mkfs.xfs xfs_growfs in image?
    inst_script "${moddir}/generator.sh" "${systemdutildir}/system-generators/immucore-generator"
    # SERVICES FOR SYSTEMD-BASED SYSTEMS
    inst_simple "${moddir}/immucore.service" "${systemdsystemunitdir}/immucore.service"
    mkdir -p "${initdir}/${systemdsystemunitdir}/initrd.target.requires"
    ln_r "../immucore.service" "${systemdsystemunitdir}/initrd.target.requires/immucore.service"
    # END SYSTEMD SERVICES

    dracut_need_initqueue
}
`

// DracutFipsConfig is the dracut config file that is used to enable FIPS mode in the initramfs
const DracutFipsConfig = `omit_dracutmodules+=" iscsi iscsiroot "
add_dracutmodules+=" fips "`

// Skips any multipath module for Ubuntu 20.04 and below. Also matches new versions in the future
// such as 30.04, 31.56 etc. This assumes Ubuntu sticks with the versioning scheme of
// <major>.<minor> where major is 20, 21, 22,
const UbuntuSupportedMultipathVersions = `(2[1-9]|[3-9][0-9]).+`

// DracutMultipathConfig is the dracut config file that is used to enable multipath support in the initramfs
const DracutMultipathConfig = `add_dracutmodules+=" multipath "`

// DracutPmemConfig is the dracut config file that is used to enable pmem support in the initramfs
const DracutPmemConfig = `add_drivers+=" nfit libnvdimm nd_pmem dax_pmem "`

// DracutSysextConfig is the dracut config file that is used to enable systemd-sysext in the initramfs
const DracutSysextConfig = `add_dracutmodules+=" systemd-sysext "`

// DracutNetworkConfig is the dracut config file that is used to enable network support in the initramfs
const DracutNetworkConfig = `add_dracutmodules+=" %s "`

// DracutSkipNvidiaDrivers is the dracut config to avoid loading drivers during initramfs which may be too soon for some systems
const DracutSkipNvidiaDrivers = `omit_drivers+=" nvidia nvidia_drm nvidia_modeset nvidia_uvm "`

// DracutSkipIscsi is the dracut config to avoid loading iscsi during initramfs
const DracutSkipIscsi = `omit_dracutmodules+=" iscsi "`

// DracutXhciRenesasConfig forces the xhci_pci_renesas kernel module into the initramfs.
//
// The module drives Renesas uPD720201/uPD720202 xHCI USB 3.0 controllers. These parts
// are used broadly by server BMC (baseboard management controller) implementations to
// expose remote virtual media — virtual CD-ROM, virtual USB storage, virtual USB
// keyboard — to the host as USB 3.0 devices. This is a BMC hardware/firmware trait,
// not a server-chipset trait: HPE ProLiant (iLO), Dell PowerEdge (iDRAC), and
// Supermicro (BMC / SuperDoctor / IPMI) all exhibit it on affected generations,
// regardless of the platform chipset. Without this module loaded early, BMC-remoted
// installs and rescue sessions lose keyboard input and cannot see the mounted
// virtual media during the initramfs phase, dropping the operator to an unusable shell.
//
// Kairos already sets hostonly="no" globally (see ImmucoreConfigDracut), so dracut is in
// generic mode — but generic mode is not "include every .ko under /lib/modules". Dracut
// still applies its own driver-class filter and hardcoded include set, and uncommon USB
// host controllers like this Renesas part are not in the default pull. On top of that
// the module ships in the "extras" kernel package (kernel-modules-extra on RHEL family,
// linux-modules-extra-* on Ubuntu/Debian), so the package must be installed too — the
// package maps in pkg/values are responsible for that half. This file is the other half:
// once the .ko is on disk, tell dracut explicitly to bundle it via add_drivers.
const DracutXhciRenesasConfig = `# Force-include the Renesas xHCI USB 3.0 driver (uPD720201/uPD720202).
# Required for server BMC virtual media (vCD, vUSB, virtual keyboard) to work during
# the initramfs phase. This affects all major server brands — HPE ProLiant (iLO),
# Dell PowerEdge (iDRAC), Supermicro (BMC/IPMI) — because the dependency is on the
# BMC controller hardware/firmware exposing a Renesas xHCI USB device to the host,
# not on the server's chipset. Kairos runs dracut with hostonly=no, but dracut's
# generic mode still filters drivers and does not auto-include this uncommon USB
# host controller, so it must be listed explicitly. The module itself lives in the
# kernel "extras" package (kernel-modules-extra / linux-modules-extra-*); this
# config assumes it is installed. Do not remove without confirming BMC-remoted
# installs still work on affected server generations.
add_drivers+=" xhci_pci_renesas "
`

// DRACUT stuff ends here

// GrubCfg /etc/cos/grub.cfg is the default grub config that is used for the system boot
const GrubCfg = `set timeout=10

# set default values for kernel, initramfs
# Other files can still override this
set kernel=/boot/vmlinuz
set initramfs=/boot/initrd

# Load custom env file
set env_file="/grubenv"
search --no-floppy --file --set=env_blk "${env_file}"

# Load custom env file
set oem_env_file="/grub_oem_env"
search --no-floppy --file --set=oem_blk "${oem_env_file}"

# Load custom config file
set custom_menu="/grubmenu"
search --no-floppy --file --set=menu_blk "${custom_menu}"

# Load custom config file
set custom="/grubcustom"
search --no-floppy --file --set=custom_blk "${custom}"

if [ "${oem_blk}" ] ; then
  load_env -f "(${oem_blk})${oem_env_file}"
fi

if [ "${env_blk}" ] ; then
  load_env -f "(${env_blk})${env_file}"
fi

# Save default
if [ "${next_entry}" ]; then
  set default="${next_entry}"
  set selected_entry="${next_entry}"
  set next_entry=
  save_env -f "(${env_blk})${env_file}" next_entry
else
  set default="${saved_entry}"
fi

## Display a default menu entry if set
if [ "${default_menu_entry}" ]; then
  set display_name="${default_menu_entry}"
else
  set display_name="Kairos"
fi

## Set a default fallback if set
if [ "${default_fallback}" ]; then
  set fallback="${default_fallback}"
else
  set fallback="0 1 2"
fi

insmod all_video
insmod loopback
insmod squash4
insmod serial

loadfont unicode
if [ "${grub_platform}" = "efi" ]; then
    ## workaround for grub2-efi bug: https://bugs.launchpad.net/ubuntu/+source/grub2/+bug/1851311
    rmmod tpm
fi

menuentry "${display_name}" --id cos {
  search --no-floppy --label --set=root COS_STATE
  set img=/cOS/active.img
  set label=COS_ACTIVE
  if [ -d (loop0) ]; then
    loopback -d loop0
  fi
  loopback loop0 /$img
  set root=($root)
  source (loop0)/etc/cos/bootargs.cfg
  linux (loop0)$kernel $kernelcmd ${extra_cmdline} ${extra_active_cmdline}
  initrd (loop0)$initramfs
}

menuentry "${display_name} (fallback)" --id fallback {
  search --no-floppy --label --set=root COS_STATE
  set img=/cOS/passive.img
  set label=COS_PASSIVE
  if [ -d (loop0) ]; then
    loopback -d loop0
  fi
  loopback loop0 /$img
  set root=($root)
  source (loop0)/etc/cos/bootargs.cfg
  linux (loop0)$kernel $kernelcmd ${extra_cmdline} ${extra_passive_cmdline}
  initrd (loop0)$initramfs
}

menuentry "${display_name} recovery" --id recovery {
  search --no-floppy --label --set=root COS_RECOVERY
  if [ test -s /cOS/recovery.squashfs ]; then
    set img=/cOS/recovery.squashfs
    set recoverylabel=COS_RECOVERY
  else
    set img=/cOS/recovery.img
  fi
  set label=COS_SYSTEM
  if [ -d (loop0) ]; then
    loopback -d loop0
  fi
  loopback loop0 /$img
  set root=($root)
  source (loop0)/etc/cos/bootargs.cfg
  linux (loop0)$kernel $kernelcmd ${extra_cmdline} ${extra_recovery_cmdline}
  initrd (loop0)$initramfs
}

menuentry "${display_name} state reset (auto)" --id statereset {
  search --no-floppy --label --set=root COS_RECOVERY
  if [ test -s /cOS/recovery.squashfs ]; then
    set img=/cOS/recovery.squashfs
    set recoverylabel=COS_RECOVERY
  else
    set img=/cOS/recovery.img
  fi
  set label=COS_SYSTEM
  if [ -d (loop0) ]; then
    loopback -d loop0
  fi
  loopback loop0 /$img
  set root=($root)
  source (loop0)/etc/cos/bootargs.cfg
  linux (loop0)$kernel $kernelcmd ${extra_cmdline} ${extra_recovery_cmdline} vga=795 nomodeset kairos.reset
  initrd (loop0)$initramfs
}

if [ "${menu_blk}" ]; then
  source "(${menu_blk})${custom_menu}"
fi

if [ "${custom_blk}" ]; then
  source "(${custom_blk})${custom}"
fi
`

// BootArgsCfg /etc/cos/bootargs.cfg is the script run under grub that chooses what cmdline to construct based on the distro
const BootArgsCfg = `function setSelinux {
    source (loop0)/etc/os-release
    if [ -f (loop0)/etc/kairos-release ]; then
        source (loop0)/etc/kairos-release
    fi

    # Disable selinux for all distros. Supporting selinux requires more than
    # just enabling it like this.
    set baseSelinuxCmd="selinux=0"

    #if test $KAIROS_FAMILY == "rhel" -o test $ID == "opensuse-tumbleweed" -o test $ID == "opensuse-leap"; then
    #    set baseSelinuxCmd="selinux=0"
    #else
    #    # if not in recovery
    #    if [ -z "$recoverylabel" ];then
    #        set baseSelinuxCmd="security=selinux selinux=1"
    #    fi
    #fi
}

function setExtraConsole {
    source (loop0)/etc/os-release
    if [ -f (loop0)/etc/kairos-release ]; then
        source (loop0)/etc/kairos-release
    fi
    set baseExtraConsole="console=ttyS0"
    # rpi
    if test $KAIROS_MODEL == "rpi3" -o test $KAIROS_MODEL == "rpi4"; then
		echo "Found rpi model"
        set baseExtraConsole="console=ttyS0,115200"
    fi
    # nvidia agx orin
    if test $KAIROS_MODEL == "nvidia-jetson-agx-orin"; then
		echo "Found agx-orin model"
        set baseExtraConsole="console=ttyTCU0,115200"
    fi
    # nvidia orin nx - we set the terga TCU serial console and the ARM PL011 UART
    if test $KAIROS_MODEL == "nvidia-jetson-orin-nx"; then
		echo "Found orin-nx model"
        set baseExtraConsole="console=ttyTCU0,115200 console=ttyAMA0,115200"
    fi
    if test $KAIROS_MODEL == "nvidia-jetson-thor"; then
		echo "Found Thor model, setting console options"
		set baseExtraConsole="console=ttyUTC0,115200 earlycon=tegra_utc,mmio32,0xc5a0000"
	fi
    # nvidia dgx spark (GB10) - standard SBSA serial console
    if test $KAIROS_MODEL == "nvidia-dgx-spark"; then
		echo "Found DGX Spark model, setting console options"
		set baseExtraConsole="console=ttyS0,921600"
	fi
}

function setExtraArgs {
    source (loop0)/etc/os-release
    if [ -f (loop0)/etc/kairos-release ]; then
        source (loop0)/etc/kairos-release
    fi
    set baseExtraArgs=""
    # rpi
    if test $KAIROS_MODEL == "rpi3" -o test $KAIROS_MODEL == "rpi4"; then
        # on rpi we need to enable memory cgroup for docker/k3s to work
        set baseExtraArgs="modprobe.blacklist=vc4 8250.nr_uarts=1 cgroup_enable=memory"
    fi
    if test $KAIROS_MODEL == "nvidia-jetson-thor"; then
		echo "Found Thor model, setting ignore unused options"
		# on Thor we need to set the ignore unused so devices don't die during booting
		set baseExtraArgs="pd_ignore_unused clk_ignore_unused fbcon=map:0 nospectre_bhb efi=runtime"
	fi
    # nvidia dgx spark (GB10) - platform args from the nvidia-spark-* grub.d drop-ins
    # on the stock DGX OS (Kairos uses its own grub template, so set them explicitly)
    if test $KAIROS_MODEL == "nvidia-dgx-spark"; then
		echo "Found DGX Spark model, setting platform options"
		set baseExtraArgs="initcall_blacklist=tegra234_cbb_init pci=pcie_bus_safe iommu.passthrough=0"
	fi
}

function setKernelCmd {
    # At this point we have the system mounted under (loop0)
    #
    # baseCmd -> Shared between all entries
    # baseRootCmd -> specific bits that immucore uses to mount the boot devices and identify the image to mount
    # baseSelinuxCmd -> selinux enabled/disabled
    # baseExtraConsole -> extra console to set
    # baseExtraArgs -> extra needed args
    set baseCmd="console=tty1 net.ifnames=1 rd.cos.oemlabel=COS_OEM rd.cos.oemtimeout=10 panic=5 rd.emergency=reboot rd.shell=0 systemd.crash_reboot=yes"
    if [ -n "$recoverylabel" ]; then
        set baseRootCmd="root=live:LABEL=$recoverylabel rd.live.dir=/ rd.live.squashimg=$img"
    else
        set baseRootCmd="root=LABEL=$label cos-img/filename=$img"
    fi
    setSelinux
    setExtraConsole
    setExtraArgs
    # finally set the full cmdline
    set kernelcmd="$baseExtraConsole $baseCmd $baseRootCmd $baseSelinuxCmd $baseExtraArgs"
}

# grub.cfg now ships this but during upgrades we do not update the COS_GRUB partition, so no new grub.cfg is copied over there
# We need to keep it for upgrades to work.
# TODO: Deprecate in v2.8-v3.0
set kernel=/boot/vmlinuz
set initramfs=/boot/initrd
# set the kernelcmd dynamically
setKernelCmd
`

// MOTD /etc/motd is the message of the day that is displayed when the user logs in
const MOTD = `Welcome to Kairos!

Refer to https://kairos.io for documentation.
`

// Issue /etc/issue.d/01-KAIROS is the issue that is displayed when the user logs in
const Issue = `                                                          
    _/    _/            _/                                
   _/  _/      _/_/_/      _/  _/_/    _/_/      _/_/_/   
  _/_/      _/    _/  _/  _/_/      _/    _/  _/_/        
 _/  _/    _/    _/  _/  _/        _/    _/      _/_/     
_/    _/    _/_/_/  _/  _/          _/_/    _/_/_/        
                                                          
                         
`

// Branding starts here

// ExtraGrubCfg /etc/kairos/branding/grubmenu.cfg is the extra grub config that is used for the system that can be
// overridden by the user to provide its own entries in grub
const ExtraGrubCfg = `menuentry "${display_name} remote recovery" --id remoterecovery {
    search --no-floppy --label --set=root COS_RECOVERY
    if [ test -s /cOS/recovery.squashfs ]; then
        set img=/cOS/recovery.squashfs
        set recoverylabel=COS_RECOVERY
    else
        set img=/cOS/recovery.img
    fi
    set label=COS_SYSTEM
    if [ -d (loop0) ]; then
        loopback -d loop0
    fi
    loopback loop0 /$img
    set root=($root)
    source (loop0)/etc/cos/bootargs.cfg
    linux (loop0)$kernel $kernelcmd ${extra_cmdline} ${extra_recovery_cmdline} vga=795 nomodeset kairos.remote_recovery_mode
    initrd (loop0)$initramfs
}
`

const InstallText = `Welcome to Kairos!
P2P device installation enrollment is starting.
A QR code will be displayed below.
In another machine, run "kairos register" with the QR code visible on screen,
or "kairos register <file>" to register the machine from a photo.
IF the qrcode is not displaying correctly,
try booting with another vga option from the boot cmdline (e.g. vga=791).

Press any key to abort pairing. To restart run 'kairos install'.

Starting in 5 seconds...
`

const ResetText = `Welcome to kairos!
The node will automatically reset its state in a few.

Press any key to abort this process. To restart run 'kairos reset'.

Starting in 60 seconds...
`

const RecoveryText = `Welcome to kairos recovery mode!
P2P device recovery mode is starting.
A QR code with a generated network token will be displayed below that can be used to connect 
over with "kairos bridge --qr-code-image /path/to/image.jpg" from another machine, 
further instruction will appear on the bridge CLI to connect over via SSH.
IF the qrcode is not displaying correctly,
try booting with another vga option from the boot cmdline (e.g. vga=791).

Press any key to abort recovery. To restart the process run 'kairos recovery'.
`

const InteractiveText = `Welcome to the Interactive installation.
Documentation is available at https://kairos.io.
`

// Branding ends here

// Services start here

// SystemdNetworkOnlineWaitOverride contains the service that is used to wait for the network to be online
// This makes the service wait for ANY interface to be online instead of the default which is to wait for all interfaces
const SystemdNetworkOnlineWaitOverride = `[Service]
ExecStart=
ExecStart=/usr/lib/systemd/systemd-networkd-wait-online --any`

// Services end here

// NvidiaL4TScript is the Nvidia Linux for Tegra (L4T) script to download and extract the necessary files
const NvidiaL4TScript = `#!/bin/bash
set -e

NVIDIA_RELEASE="%s"
NVIDIA_VERSION="%s"
NVIDIA_ARCHIVE_URI="https://developer.nvidia.com/downloads/embedded/l4t/r${NVIDIA_RELEASE}_release_v${NVIDIA_VERSION}/release"
TEGRA_ARCHIVE="jetson_linux_r${NVIDIA_RELEASE}.${NVIDIA_VERSION}_aarch64.tbz2"
ROOTFS_ARCHIVE="tegra_linux_sample-root-filesystem_r${NVIDIA_RELEASE}.${NVIDIA_VERSION}_aarch64.tbz2"
TEGRA_DIR="Linux_for_Tegra"

echo "Downloading NVIDIA L4T archives..."
wget "${NVIDIA_ARCHIVE_URI}/${TEGRA_ARCHIVE}" -O "$TEGRA_ARCHIVE"
wget "${NVIDIA_ARCHIVE_URI}/${ROOTFS_ARCHIVE}" -O "$ROOTFS_ARCHIVE"

echo "Extracting Jetson Linux..."
tar -xjf "$TEGRA_ARCHIVE"
tar -xjf "$ROOTFS_ARCHIVE" -C "$TEGRA_DIR/rootfs"

echo "Removing downloaded archives..."
rm -f "${TEGRA_ARCHIVE}" "${ROOTFS_ARCHIVE}"
`
