#!/bin/sh
#
# Says whether a Linux system can become a Kairos system.
#
# usage: check-base-image.sh [--root <dir>] [--arch <arch>] [--trusted-boot]
#                            [--converted] [--quiet]
#
# It reads the system it runs on, or the root filesystem under --root, and
# prints one line for each requirement: pass, warn or fail. It changes nothing,
# it needs no network and it does not need to run as root.
#
#   pass  the requirement is met, or it is left to the packages kairos-init
#         installs for the family
#   warn  Kairos works without it, but a feature or a boot mode is not available
#   fail  kairos-init cannot turn this system into a Kairos system
#
#   --root <dir>    check an unpacked root filesystem instead of the running one
#   --arch <arch>   architecture of --root, as `uname -m` prints it
#   --trusted-boot  check for a Trusted Boot (UKI) image, which has no GRUB and
#                   no dracut initramfs
#   --converted     the system already went through kairos-init, so anything
#                   missing is missing for good
#   --quiet         print the warn and fail lines and the result only
#
# It does not look in the package repositories, so on a distribution nobody
# built before only a build proves that the packages exist. It does not cover
# the board models and FIPS either.
#
# Exit status: 0 when nothing required is missing, 1 when something is, 2 on a
# usage error.
#
# The rules below are the ones kairos-init, yip, immucore and the agent apply.
# kairos-init/Dockerfile.test runs this script next to kairos-init on every
# base image in CI and fails when the two disagree, and
# scripts/check-base-image_test.sh compares the distributions, the names, the
# Ubuntu releases, the EFI paths, the GRUB modules and the initramfs commands
# with the Go sources. When one of those changes, change it here too. Nothing
# compares the oldest releases and the package managers of yip, so look at those
# when the package lists or yip change.
#
# POSIX sh and as few external commands as possible: this has to run on base
# images that have no bash, no awk and no findutils.

set -u
# No path expansion on values from os-release.
set -f

ROOT=""
ARCH=""
TRUSTED_BOOT=false
CONVERTED=false
QUIET=false

usage() {
  echo "usage: $0 [--root <dir>] [--arch <arch>] [--trusted-boot] [--converted] [--quiet]" >&2
  exit 2
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --root) if [ "$#" -lt 2 ] || [ -z "$2" ]; then usage; fi; ROOT="${2%/}"; shift ;;
    --arch) [ "$#" -ge 2 ] || usage; ARCH="$2"; shift ;;
    --trusted-boot) TRUSTED_BOOT=true ;;
    --converted) CONVERTED=true ;;
    --quiet) QUIET=true ;;
    *) usage ;;
  esac
  shift
done

if [ -n "$ROOT" ] && [ ! -d "$ROOT" ]; then
  echo "$0: $ROOT is not a directory" >&2
  exit 2
fi

fails=0
warns=0
# Fails that are about what the tools make of the name and the version, and not
# about what is in the system.
identity_fails=0
# Whether kairos-init has a package list for the family of this system. Set by
# the identity checks, read by every component check.
installs=false

section() { [ "$QUIET" = true ] || printf '\n%s\n' "$1"; }
pass() { [ "$QUIET" = true ] || printf '  pass  %s\n' "$1"; }
warn() { warns=$((warns + 1)); printf '  warn  %s\n' "$1"; }
fail() { fails=$((fails + 1)); printf '  fail  %s\n' "$1"; }
fail_identity() { identity_fails=$((identity_fails + 1)); fail "$1"; }

# os_release <key>: value of the key in os-release, without quotes.
os_release() {
  _value=""
  [ -r "$OS_RELEASE" ] || return 0
  while IFS= read -r _line || [ -n "$_line" ]; do
    case "$_line" in
      "$1="*)
        _value="${_line#*=}"
        case "$_value" in
          \"*\") _value="${_value#\"}"; _value="${_value%\"}" ;;
          \'*\') _value="${_value#\'}"; _value="${_value%\'}" ;;
        esac
        ;;
    esac
  done <"$OS_RELEASE"
  printf '%s' "$_value"
}

# resolve <path>: where the path really is, with every link followed inside the
# root. A link to an absolute path in --root would otherwise end up on the host.
resolve() {
  if [ -z "$ROOT" ]; then
    [ -e "$1" ] && printf '%s' "$1"
    return
  fi
  _rest="$1"
  _done=""
  _hops=0
  while [ -n "$_rest" ]; do
    _part="${_rest%%/*}"
    case "$_rest" in */*) _rest="${_rest#*/}" ;; *) _rest="" ;; esac
    case "$_part" in
      "" | .) continue ;;
      ..) _done="${_done%/*}"; continue ;;
    esac
    if [ -h "$ROOT$_done/$_part" ]; then
      _hops=$((_hops + 1))
      [ "$_hops" -le 40 ] || return 1
      _target="$(readlink "$ROOT$_done/$_part")" || return 1
      case "$_target" in /*) _done="" ;; esac
      _rest="$_target${_rest:+/$_rest}"
    else
      _done="$_done/$_part"
    fi
  done
  [ -e "$ROOT$_done" ] && printf '%s' "$ROOT$_done"
}

# exists <path>: the path is in the root, and not as a link to nowhere.
exists() { resolve "$1" >/dev/null; }

# find_bin <name>...: path of the first of the commands that is in the root.
# /usr/local does not count: the persistent partition is mounted over it.
find_bin() {
  for _name in "$@"; do
    for _dir in /usr/sbin /usr/bin /sbin /bin; do
      if _file="$(resolve "$_dir/$_name")" && [ -f "$_file" ] && [ -x "$_file" ]; then
        printf '%s' "$_dir/$_name"
        return 0
      fi
    done
  done
  return 1
}

# missing <required|optional> <what> <what it is for>: report something that is
# not there. A base container image has next to nothing installed, so what the
# package list of the family brings counts as there.
missing() {
  _brought="$installs"
  # What the package lists leave out.
  case "$family:$distro $VERSION_ID:$2" in
    redhat:*" 8"*:systemd-cryptenroll | redhat:*" 8"*:systemd-sysext) _brought=false ;;
    "debian:ubuntu 20.04:systemd-cryptenroll" | "debian:ubuntu 20.04:systemd-sysext") _brought=false ;;
    *:mksquashfs) [ "$TRUSTED_BOOT" = false ] || _brought=false ;;
  esac
  if [ "$_brought" = true ]; then
    pass "$2 ($3): not installed yet, left to the packages for the $family family"
  elif [ "$1" = required ]; then
    fail "$2 is missing ($3)"
  else
    warn "$2 is missing ($3)"
  fi
}

# component <required|optional> <what it is for> <command>...: one of the
# commands has to be there.
component() {
  _level="$1"
  _what="$2"
  shift 2
  if _path="$(find_bin "$@")"; then
    pass "$1 ($_what): $_path"
  else
    _names="$1"
    shift
    for _other in "$@"; do _names="$_names or $_other"; done
    missing "$_level" "$_names" "$_what"
  fi
}

# family_of <id>: the family kairos-init puts an os-release ID in.
# kairos-init/pkg/system/system.go, detectFromID
family_of() {
  case "$1" in
    debian | ubuntu) echo debian ;;
    fedora | rocky | almalinux | ol | rhel) echo redhat ;;
    arch) echo arch ;;
    alpine) echo alpine ;;
    opensuse-leap | opensuse-tumbleweed | sles | sle-micro-rancher) echo suse ;;
    hadron) echo hadron ;;
  esac
}

# installer_of <id>: the package manager yip uses for an os-release ID. yip
# follows ID_LIKE only when the whole value is "suse".
# mudler/yip pkg/plugins/packages.go, identifyInstaller
installer_of() {
  case "$1" in
    debian | ubuntu) echo apt-get ;;
    fedora | rocky | ol | almalinux | rhel | centos | openEuler) echo dnf ;;
    arch) echo pacman ;;
    alpine) echo apk ;;
    opensuse-leap | opensuse-tumbleweed | suse) echo zypper ;;
    *) [ "$2" = suse ] && echo zypper ;;
  esac
}

# initramfs_of <pretty name>: the initramfs kairos-init builds. The stages that
# build it match on PRETTY_NAME.
# kairos-init/pkg/stages/steps_init.go, osDracutFamilies. GetInitrdStage also
# matches SLES, but a system with only that in its name gets an initramfs
# without immucore.
initramfs_of() {
  case "$1" in
    *Ubuntu* | *Debian* | *Fedora* | *CentOS* | *"Red Hat"* | *Rocky* | *AlmaLinux* | \
      *"Oracle Linux"* | *openSUSE* | *OpenSUSE* | *SUSE* | *Hadron*) echo dracut ;;
    *Alpine*) echo mkinitfs ;;
  esac
}

# --- identity ----------------------------------------------------------------

section "Identity: how the Kairos tools see this distribution"

# The Kairos tools read /etc/os-release only, not /usr/lib/os-release.
OS_RELEASE="$(resolve /etc/os-release)" || OS_RELEASE=""

ID="$(os_release ID)"
ID_LIKE="$(os_release ID_LIKE)"
VERSION_ID="$(os_release VERSION_ID)"
PRETTY_NAME="$(os_release PRETTY_NAME)"

distro=""
family=""
if [ -z "$ID" ]; then
  fail_identity "/etc/os-release has no ID, so no Kairos tool can tell what this system is"
else
  pass "os-release: ${PRETTY_NAME:-no PRETTY_NAME} (ID=$ID, VERSION_ID=${VERSION_ID:-none})"

  family="$(family_of "$ID")"
  if [ -n "$family" ]; then
    distro="$ID"
    pass "family: $family (ID=$ID)"
  else
    for token in $ID_LIKE; do
      family="$(family_of "$token")"
      distro="$token"
      if [ -z "$family" ]; then
        case "$token" in
          redhat) family=redhat; distro=fedora ;;
          suse) family=suse; distro=opensuse-leap ;;
        esac
      fi
      [ -z "$family" ] || break
    done
    if [ -n "$family" ]; then
      pass "family: $family, as a derivative of $distro (ID_LIKE=$ID_LIKE)"
    else
      distro=""
      fail_identity "family: kairos-init knows neither ID=$ID nor ID_LIKE=${ID_LIKE:-none}"
    fi
  fi
fi

# kairos-init has no package list for the Arch family.
case "$family" in
  arch) fail "packages: kairos-init has no package list for the Arch family" ;;
  hadron) pass "packages: Hadron ships everything, kairos-init installs no package on it" ;;
  ?*)
    [ "$CONVERTED" = true ] || installs=true
    installer="$(installer_of "$ID" "$ID_LIKE")"
    if [ -z "$installer" ]; then
      fail_identity "packages: yip cannot tell the package manager from ID=$ID, kairos-init stops with \"unknown package manager\""
    elif [ "$CONVERTED" = false ] && ! find_bin "$installer" >/dev/null; then
      fail "packages: $installer is missing, kairos-init cannot install packages"
    else
      pass "packages: $installer"
    fi
    case "$distro" in
      rhel | sles | sle-micro-rancher)
        # CentOS Stream is a derivative of rhel with public repositories.
        if [ "$distro" = "$ID" ] && [ "$CONVERTED" = false ]; then
          warn "packages: the kernel and other packages are in repositories that need a subscription"
        fi
        ;;
    esac
    ;;
esac

# The version filters the package lists, and one that does not parse stops
# kairos-init. Hadron installs no package, so its version is not parsed.
version_ok=true
case "$family" in
  hadron | "") ;;
  *)
    _numbers="${VERSION_ID%%[-+]*}"
    [ "$family" != alpine ] || _numbers="${_numbers%%_*}"
    case "${_numbers#v}" in
      "" | .* | *. | *..* | *[!0-9.]*)
        version_ok=false
        fail_identity "version: VERSION_ID=${VERSION_ID:-none} is not a version, kairos-init stops on it"
        ;;
      *) pass "version: $VERSION_ID" ;;
    esac
    ;;
esac

# Releases the package lists do not fit. A derivative counts its own versions, so
# only the distribution itself can be judged by its number, except for Ubuntu,
# where the kernel is listed for each release.
# kairos-init/pkg/values/packagemaps.go
if [ "$version_ok" = true ] && [ "$CONVERTED" = false ]; then
  _version="${VERSION_ID#v}"
  _major="${_version%%[!0-9]*}"
  _minor=0
  case "$_version" in *.*) _minor="${_version#*.}"; _minor="${_minor%%[!0-9]*}" ;; esac
  _minor="${_minor#0}"
  case "$distro" in
    ubuntu)
      if [ "$TRUSTED_BOOT" = true ]; then
        # KernelPackagesTrustedBoot: the 24.04 HWE kernel, on every release
        _known=false
        if [ "$_major" -gt 24 ] || { [ "$_major" -eq 24 ] && [ "${_minor:-0}" -ge 4 ]; }; then
          _known=true
        fi
      else
        case "$VERSION_ID" in
          *[!0-9.]*) _known=false ;;
          20.04 | 22.04 | 24.04 | 24.10 | 25.04 | 25.10) _known=true ;;
          *)
            _known=false
            if [ "$_major" -gt 26 ] || { [ "$_major" -eq 26 ] && [ "${_minor:-0}" -ge 4 ]; }; then
              _known=true
            fi
            # The riscv64 list is the same for every release.
            case "${ARCH:-$(uname -m)}" in riscv64) _known=true ;; esac
            ;;
        esac
      fi
      if [ "$_known" = true ]; then
        pass "kernel package: kairos-init has one for Ubuntu $VERSION_ID"
      else
        fail_identity "kernel package: kairos-init has none for Ubuntu $VERSION_ID, VERSION_ID has to be an Ubuntu release it knows"
      fi
      ;;
    debian)
      if [ "$ID" = debian ] && [ "$_major" -lt 12 ]; then
        fail "packages: Debian $VERSION_ID does not have all of them, Debian 12 or newer does"
      fi
      ;;
    opensuse-leap)
      if [ "$ID" = opensuse-leap ] && [ "$_major" -lt 16 ]; then
        fail "packages: openSUSE Leap $VERSION_ID does not have all of them, Leap 16 or newer does"
      fi
      ;;
    sles)
      if [ "$ID" = sles ] && [ "$_major" -lt 16 ]; then
        fail "packages: SLES $VERSION_ID does not have all of them in its public repositories"
      fi
      ;;
  esac
fi

initramfs="$(initramfs_of "$PRETTY_NAME")"
if [ -z "$initramfs" ]; then
  fail_identity "build steps: no kairos-init stage matches PRETTY_NAME=${PRETTY_NAME:-none}, so it builds no initramfs and does not enable the network and ssh services"
elif [ "$TRUSTED_BOOT" = true ]; then
  pass "build steps: PRETTY_NAME matches"
  initramfs=uki
elif [ "$initramfs" = mkinitfs ] && [ "$family" != alpine ]; then
  fail "initramfs: PRETTY_NAME says Alpine, but the family is ${family:-unknown}"
elif [ "$initramfs" = dracut ] && [ "$family" = alpine ]; then
  fail "initramfs: PRETTY_NAME says dracut, but the family is alpine"
else
  pass "initramfs: $initramfs (PRETTY_NAME matches)"
fi

# --- platform ----------------------------------------------------------------

section "Platform"

[ -n "$ARCH" ] || ARCH="$(uname -m)"
case "$ARCH" in
  x86_64 | amd64) efi_arch=x86_64; pass "architecture: $ARCH" ;;
  aarch64 | arm64) efi_arch=arm64; pass "architecture: $ARCH" ;;
  riscv64) efi_arch=riscv64; pass "architecture: $ARCH" ;;
  *) efi_arch=""; fail "architecture: $ARCH, Kairos is built for amd64, arm64 and riscv64" ;;
esac

# yip skips every stage that is meant for one init system when it finds both.
systemd="$(find_bin systemctl)" || systemd=""
openrc="$(find_bin openrc)" || openrc=""
if [ -n "$systemd" ] && [ -n "$openrc" ]; then
  fail "init system: systemd and OpenRC are both installed, and yip cannot choose"
elif [ -n "$systemd" ]; then
  pass "init system: systemd"
elif [ -n "$openrc" ]; then
  if [ "$TRUSTED_BOOT" = true ]; then
    fail "init system: OpenRC, Trusted Boot needs systemd"
  else
    pass "init system: OpenRC"
  fi
elif [ "$family" = alpine ] && [ "$TRUSTED_BOOT" = true ]; then
  fail "init system: OpenRC, Trusted Boot needs systemd"
elif [ "$family" != alpine ] && { exists /sbin/init || exists /usr/sbin/init || exists /etc/inittab; }; then
  # The Alpine base image has busybox as init and no OpenRC yet.
  fail "init system: neither systemd nor OpenRC, and something else is installed as init"
else
  missing required "systemd or OpenRC" "init system"
fi

# The version can only be asked of a binary that runs here.
if [ -n "$systemd" ] && [ -z "$ROOT" ]; then
  _version="$("$systemd" --version 2>/dev/null)" || _version=""
  _version="${_version#systemd }"
  _version="${_version%%[!0-9]*}"
  if [ -z "$_version" ]; then
    warn "systemd version: cannot be read"
  elif [ "$_version" -ge 252 ]; then
    pass "systemd version: $_version"
  elif [ "$TRUSTED_BOOT" = true ]; then
    fail "systemd version: $_version, Trusted Boot needs 252 or newer"
  else
    warn "systemd version: $_version, encryption bound to TPM measurements needs 252 or newer"
  fi
elif [ -n "$systemd" ] && [ "$TRUSTED_BOOT" = true ]; then
  warn "systemd version: not read from another root, Trusted Boot needs 252 or newer"
fi

# --- kernel ------------------------------------------------------------------

section "Kernel"

kernel=""
if modules="$(resolve /lib/modules)"; then
  # The newest one, as kairos-init chooses it. ls and sort are there when a
  # kernel is.
  # shellcheck disable=SC2012 # kernel releases have no spaces
  for _name in $(ls "$modules" 2>/dev/null | sort -V 2>/dev/null); do
    [ ! -f "$modules/$_name/modules.builtin" ] || kernel="$modules/$_name"
  done
fi

# What encrypts the disk of a Trusted Boot system by default.
encryption=optional
[ "$TRUSTED_BOOT" = false ] || encryption=required

# module <required|optional> <what it is for> <name>: the driver is a module or
# is built into the kernel. File names use dashes and underscores alike.
module() {
  _pattern="/$(printf '%s' "$3" | sed 's/[-_]/[-_]/g')\.ko"
  if grep -q "$_pattern" "$kernel/modules.builtin" "$kernel/modules.dep" 2>/dev/null; then
    pass "module $3 ($2)"
  else
    missing "$1" "module $3" "$2"
  fi
}

if [ -z "$kernel" ]; then
  missing required "/lib/modules" "kernel"
elif ! command -v grep >/dev/null 2>&1 || ! command -v sed >/dev/null 2>&1; then
  warn "kernel: the modules cannot be checked without grep and sed"
else
  pass "kernel: ${kernel##*/}"
  module required "read-only system, writable paths" overlay
  module required "install media and recovery image" squashfs
  module required "system images" loop
  module required "state, OEM and persistent partitions" ext4
  module required "EFI system partition" vfat
  module required "install media" isofs
  module required "LVM and encryption" dm_mod
  module "$encryption" "encrypted partitions" dm_crypt
  module optional "boot from write-protected media" dm_snapshot
  module optional "TPM 2.0" tpm_crb
  module optional "TPM" tpm_tis
  module optional "multipath storage" dm_multipath
  if [ "$CONVERTED" = true ]; then
    if exists /boot/vmlinuz; then pass "/boot/vmlinuz"; else fail "/boot/vmlinuz is missing"; fi
    if [ "$TRUSTED_BOOT" = false ]; then
      if exists /boot/initrd; then pass "/boot/initrd"; else fail "/boot/initrd is missing"; fi
    fi
  fi
fi

# --- boot --------------------------------------------------------------------

section "Boot"

# first_of <path>...: the first of the paths that is in the root.
first_of() {
  for _path in "$@"; do
    if exists "$_path"; then
      printf '%s' "$_path"
      return 0
    fi
  done
  return 1
}

# grub_file <name> <directory name>: the file is in the GRUB directory for that
# platform, wherever the distribution keeps it.
grub_file() {
  for _dir in /usr/lib/grub /usr/share/grub2 /usr/lib/grub2 /usr/share/grub; do
    exists "$_dir/$2/$1" && return 0
  done
  return 1
}

# The paths the agent copies to the EFI system partition.
# sdk/utils/utils.go, GetEfiGrubFiles and GetEfiShimFiles
case "$efi_arch" in
  x86_64)
    shim_files="/usr/share/efi/x86_64/shim.efi /usr/lib/shim/shimx64.efi.dualsigned
      /usr/lib/shim/shimx64.efi.signed.latest /usr/lib/shim/shimx64.efi.signed
      /boot/efi/EFI/fedora/shim.efi /boot/efi/EFI/rocky/shim.efi /boot/efi/EFI/redhat/shim.efi
      /boot/efi/EFI/redhat/shimx64.efi /boot/efi/EFI/almalinux/shim.efi"
    grub_files="/usr/share/efi/x86_64/grub.efi /usr/lib/grub/x86_64-efi-signed/grubx64.efi.signed
      /boot/efi/EFI/fedora/grubx64.efi /boot/efi/EFI/rocky/grubx64.efi /boot/efi/EFI/redhat/grubx64.efi
      /boot/efi/EFI/almalinux/grubx64.efi /usr/lib/grub/x86_64-efi/grubx64.efi"
    ;;
  arm64)
    shim_files="/usr/share/efi/aarch64/shim.efi /usr/lib/shim/shimaa64.efi.dualsigned
      /usr/lib/shim/shimaa64.efi.signed.latest /usr/lib/shim/shimaa64.efi.signed
      /boot/efi/EFI/fedora/shim.efi /boot/efi/EFI/rocky/shim.efi /boot/efi/EFI/redhat/shim.efi
      /boot/efi/EFI/redhat/shimaa64.efi /boot/efi/EFI/almalinux/shim.efi"
    grub_files="/usr/share/efi/aarch64/grub.efi /usr/lib/grub/arm64-efi-signed/grubaa64.efi.signed
      /boot/efi/EFI/fedora/grubaa64.efi /boot/efi/EFI/rocky/grubaa64.efi /boot/efi/EFI/redhat/grubaa64.efi
      /boot/efi/EFI/almalinux/grubaa64.efi /usr/lib/grub/arm64-efi/grubaa64.efi"
    ;;
  *)
    # riscv64 firmware loads GRUB without a shim.
    shim_files=""
    grub_files="/usr/share/efi/riscv64/grub.efi /usr/lib/grub/riscv64-efi/grubriscv64.efi
      /usr/lib/grub/riscv64-efi/monolithic/grubriscv64.efi /boot/efi/EFI/debian/grubriscv64.efi
      /boot/efi/EFI/ubuntu/grubriscv64.efi /boot/efi/EFI/fedora/grubriscv64.efi
      /boot/efi/EFI/BOOT/BOOTRISCV64.EFI"
    ;;
esac

# uefi <what> <path>...: a file the agent needs to make the disk bootable on
# UEFI. kairos-init does not look at it, so a missing one is never a fail.
uefi_missing=false
uefi() {
  _what="$1"
  shift
  if _path="$(first_of "$@")"; then
    pass "$_what: $_path"
  elif [ "$installs" = true ]; then
    case "$family:$ID" in
      debian:* | suse:* | redhat:fedora | redhat:rocky | redhat:rhel | redhat:ol | redhat:almalinux)
        pass "$_what: not installed yet, left to the packages for the $family family"
        ;;
      *)
        uefi_missing=true
        warn "$_what: the agent looks in no directory of ID=$ID, so a UEFI install fails"
        ;;
    esac
  else
    uefi_missing=true
    warn "$_what: not in any path the agent looks in, so a UEFI install fails"
  fi
}

if [ "$TRUSTED_BOOT" = true ]; then
  pass "Trusted Boot: AuroraBoot builds the UKI and brings systemd-boot"
elif [ -n "$efi_arch" ]; then
  component required "boot entries" grub2-editenv grub-editenv
  if [ "$efi_arch" = x86_64 ]; then
    component optional "install on a BIOS machine" grub2-install grub-install
    if grub_file modinfo.sh i386-pc; then
      pass "GRUB for BIOS"
    else
      missing optional "GRUB i386-pc" "BIOS boot"
    fi
  fi
  # agent/pkg/constants/constants.go, GetGrubModules
  _absent=""
  for _module in loopback.mod squash4.mod xzio.mod gzio.mod regexp.mod; do
    grub_file "$_module" "$efi_arch-efi" || _absent="$_absent $_module"
  done
  if [ -z "$_absent" ]; then
    pass "GRUB modules for UEFI"
  else
    missing optional "GRUB modules:$_absent" "UEFI boot"
  fi
  # shellcheck disable=SC2086 # lists of paths without spaces
  [ -z "$shim_files" ] || uefi "shim for UEFI" $shim_files
  # shellcheck disable=SC2086
  uefi "GRUB for UEFI" $grub_files
fi

case "$initramfs" in
  dracut)
    component required "builds the initramfs" dracut
    # The immucore dracut module is written in bash and depends on these.
    # kairos-init/pkg/bundled/bundled.go, ImmucoreModuleSetupDracut
    component required "dracut modules, systemd units" bash
    component required "initramfs" gawk
    component required "initramfs" awk
    component required "initramfs" sync
    component required "initramfs" lvm
    component optional "check of the initramfs content" lsinitrd
    ;;
  mkinitfs)
    component required "builds the initramfs" mkinitfs
    component required "machine id" dbus-uuidgen
    ;;
esac

# --- tools -------------------------------------------------------------------

section "Disk and filesystem tools"

component required "mounts" mount
component required "mounts" umount
component required "finds partitions by label" blkid
component required "finds partitions" lsblk
component required "finds mounts" findmnt
component required "device events" udevadm
component required "kernel modules" depmod
component required "kernel modules" modprobe
component required "copies the system" rsync
component required "unpacks archives" tar
component required "encryption" cryptsetup
component required "filesystem check" e2fsck
component required "filesystem labels" tune2fs
component required "system images" mkfs.ext2
component required "initramfs" mkfs.ext3
component required "partitions" mkfs.ext4
component required "EFI system partition" mkfs.vfat
component optional "squashfs recovery image" mksquashfs
component optional "state of encrypted partitions" dmsetup

section "Userspace"

component required "users" sudo
component required "console" less
# kairos-init validate asks for it on every family but Hadron.
[ "$family" = hadron ] || component required "NFS" mount.nfs
component optional "remote access" sshd
component optional "network boot" curl
if [ -n "$openrc" ] || [ "$family" = alpine ]; then
  component optional "network" connmand
else
  if exists /usr/lib/systemd/systemd-networkd || exists /lib/systemd/systemd-networkd ||
    find_bin NetworkManager >/dev/null; then
    pass "network: systemd-networkd or NetworkManager"
  else
    missing optional "systemd-networkd or NetworkManager" "network"
  fi
  component "$encryption" "encryption bound to the TPM" systemd-cryptenroll
  if exists /usr/lib/systemd/systemd-cryptsetup; then
    pass "systemd-cryptsetup (encryption bound to the TPM)"
  else
    missing "$encryption" systemd-cryptsetup "encryption bound to the TPM"
  fi
  component optional "system extensions" systemd-sysext
fi

# --- result ------------------------------------------------------------------

echo
if [ "$fails" -gt 0 ]; then
  echo "Result: this system cannot become a Kairos system as it is ($fails fail, $warns warn)."
  if [ "$installs" = true ] && [ "$identity_fails" -eq "$fails" ]; then
    echo "The $family family can. If this one has the same packages, all that stops it is that the Kairos tools do not recognize its name or version."
  fi
  exit 1
fi
if [ "$CONVERTED" = true ]; then
  echo "Result: this system has what Kairos needs ($warns warn)."
elif [ -n "$distro" ] && [ "$distro" != "$ID" ]; then
  echo "Result: this system can become a Kairos system, as a derivative of $distro ($warns warn)."
else
  echo "Result: this system can become a Kairos system ($warns warn)."
fi
if [ "$uefi_missing" = true ]; then
  echo "It cannot be installed on a UEFI machine."
  [ "$efi_arch" = x86_64 ] || echo "Machines with this architecture have no BIOS, so only images for a board such as the Raspberry Pi boot."
fi
[ "$installs" = false ] || echo "The package repositories were not checked. Only a build with kairos-init proves that the packages exist."
