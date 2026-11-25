#!/usr/bin/env bash
#
# Tests scripts/check-base-image.sh: the verdict it gives on made-up root
# filesystems, and that the lists it carries are the ones in the Go sources.

set -euo pipefail

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(dirname "$DIR")"
SCRIPT="$DIR/check-base-image.sh"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

# The shell that runs the script, for example "busybox sh".
SHELL_UNDER_TEST="${SHELL_UNDER_TEST:-sh}"

failed=0
passed=0
ok() { passed=$((passed + 1)); }
nok() { echo "FAIL: $1" >&2; failed=$((failed + 1)); }

# rootfs <ID> <ID_LIKE> <VERSION_ID> <PRETTY_NAME> [<executable>...]: a new root
# filesystem with that os-release and those files. Sets $root.
rootfs() {
  root="$(mktemp -d "$TMP/root.XXXXXX")"
  mkdir -p "$root/etc" "$root/usr/bin"
  printf 'PRETTY_NAME="%s"\nID=%s\nID_LIKE="%s"\nVERSION_ID="%s"\n' "$4" "$1" "$2" "$3" >"$root/etc/os-release"
  shift 4
  local file
  for file in "$@"; do
    case "$file" in /*) ;; *) file="/usr/bin/$file" ;; esac
    mkdir -p "$root${file%/*}"
    : >"$root$file"
    chmod +x "$root$file"
  done
}

# expect <status> <text the output holds, or ""> <what is tested> [<flag>...]
# $arch is the architecture.
arch=x86_64
expect() {
  local want="$1" text="$2" what="$3" out got=0
  shift 3
  out="$($SHELL_UNDER_TEST "$SCRIPT" --root "$root" --arch "$arch" "$@" 2>&1)" || got=$?
  if [ "$got" != "$want" ]; then
    nok "$what: exit status $got, want $want"
    printf '%s\n' "$out" | grep -E '^  fail|^Result' >&2 || true
  elif [ -n "$text" ] && ! grep -qF -- "$text" <<<"$out"; then
    nok "$what: the output does not hold: $text"
  else
    ok
  fi
}

# Base container images, with nothing but the package manager installed.
rootfs ubuntu debian 24.04 "Ubuntu 24.04.5 LTS" apt-get
expect 0 "this system can become" "ubuntu"
rootfs debian "" 13 "Debian GNU/Linux 13 (trixie)" apt-get
expect 0 "" "debian"
rootfs fedora "" 43 "Fedora Linux 43 (Container Image)" dnf
expect 0 "" "fedora"
rootfs alpine "" 3.23.8 "Alpine Linux v3.23" apk
expect 0 "" "alpine"
expect 1 "Trusted Boot needs systemd" "alpine with Trusted Boot" --trusted-boot
rootfs alpine "" 3.23.8 "Alpine Linux v3.23" apk /sbin/init /etc/inittab
expect 0 "" "alpine with the busybox init"
rootfs alpine "" 3.24.0_alpha20261001 "Alpine Linux edge" apk
expect 0 "" "alpine edge"
rootfs opensuse-leap "suse opensuse" 16.0 "openSUSE Leap 16.0" zypper
expect 0 "" "opensuse leap"
rootfs sles suse 15.7 "SUSE Linux Enterprise Server 15 SP7" zypper
expect 1 "in its public repositories" "a SLES release without all packages"
rootfs sles suse 16.0 "SUSE Linux Enterprise Server 16.0" zypper
expect 0 "need a subscription" "sles, through ID_LIKE=suse"
rootfs rhel fedora 9.6 "Red Hat Enterprise Linux 9.6 (Plow)" dnf
expect 0 "need a subscription" "rhel"
rootfs debian "" 11 "Debian GNU/Linux 11 (bullseye)" apt-get
expect 1 "Debian 12 or newer" "a Debian release without all packages"
rootfs opensuse-leap "suse opensuse" 15.6 "openSUSE Leap 15.6" zypper
expect 1 "Leap 16 or newer" "a Leap release without all packages"

# absent <text> <what is tested> [<flag>...]: the output does not hold the text.
absent() {
  local text="$1" what="$2" out
  shift 2
  out="$($SHELL_UNDER_TEST "$SCRIPT" --root "$root" --arch "$arch" "$@" 2>&1)" || true
  if grep -qF -- "$text" <<<"$out"; then nok "$what: the output holds: $text"; else ok; fi
}

# The package manager yip uses for each distribution it knows.
for pair in debian:apt-get ubuntu:apt-get fedora:dnf rocky:dnf ol:dnf almalinux:dnf rhel:dnf \
  alpine:apk opensuse-leap:zypper opensuse-tumbleweed:zypper; do
  rootfs "${pair%:*}" "" 1.0 none "${pair#*:}"
  expect 1 "packages: ${pair#*:}" "package manager of ${pair%:*}"
done
# These have no family of their own, yip knows them by their ID.
rootfs centos rhel 1.0 none dnf
expect 1 "packages: dnf" "package manager of centos"
rootfs openEuler fedora 1.0 none dnf
expect 1 "packages: dnf" "package manager of openEuler"
rootfs suse sles 16.0 none zypper
expect 1 "packages: zypper" "package manager of suse"
rootfs rocky "rhel centos fedora" 8.10 "Rocky Linux 8.10" dnf
expect 0 "systemd-cryptenroll is missing" "a Red Hat release with an old systemd"
expect 1 "systemd-cryptenroll is missing" "a Red Hat release with an old systemd, with Trusted Boot" --trusted-boot
rootfs almalinux "rhel centos fedora" 9.6 "AlmaLinux 9.6" microdnf
expect 1 "dnf is missing" "no package manager that yip knows"
absent "family can" "a missing package manager is not a name problem"

# Words in ID_LIKE that are a family and not a distribution.
rootfs mylinux redhat 1.0 "Fedora based" dnf
expect 1 "family: redhat, as a derivative of fedora" "ID_LIKE=redhat"
rootfs mylinux suse 1.0 "SUSE based" zypper
expect 0 "as a derivative of opensuse-leap" "ID_LIKE=suse"
rootfs mylinux "*" 1.0 "Something"
expect 1 "nor ID_LIKE=*" "ID_LIKE with a pattern"

# Derivatives.
rootfs centos "rhel fedora" 10 "CentOS Stream 10 (Coughlan)" dnf
expect 0 "as a derivative of rhel" "centos, a derivative both tools know"
expect 0 "a UEFI install fails" "centos, whose EFI directory the agent does not know"
rootfs kali debian 2026.3 "Kali GNU/Linux Rolling" apt-get
expect 1 "unknown package manager" "debian derivative, package manager"
expect 1 "builds no initramfs" "debian derivative, initramfs"
expect 1 "The debian family can." "debian derivative, hint"
rootfs linuxmint "ubuntu debian" 22 "Linux Mint 22" apt-get
expect 1 "kairos-init has none for Ubuntu 22" "ubuntu derivative with its own versions"
rootfs ubuntu debian 24.04 "Derivative Linux" apt-get
expect 1 "builds no initramfs" "derivative that keeps the ID of its parent"
expect 1 "does not enable the network and ssh services" "derivative that keeps the ID of its parent, with Trusted Boot" --trusted-boot
rootfs ubuntu debian 24.04 "Ubuntu 24.04 (Derivative)" apt-get
expect 0 "" "derivative that keeps the names of its parent"
rootfs devuan debian 5 "Debian based" apt-get /sbin/init
expect 1 "neither systemd nor OpenRC" "another init system"

# Ubuntu releases.
for version in 20.04 22.04 24.04 24.10 25.04 25.10 26.04 26.10 27.04 28.04; do
  rootfs ubuntu debian "$version" "Ubuntu $version" apt-get
  expect 0 "" "ubuntu $version"
done
for version in 18.04 23.10 26.03 22.04.5; do
  rootfs ubuntu debian "$version" "Ubuntu $version" apt-get
  expect 1 "kairos-init has none for Ubuntu" "ubuntu $version"
done

for version in v24.04 26.04-beta; do
  rootfs ubuntu debian "$version" "Ubuntu" apt-get
  expect 1 "kairos-init has none" "ubuntu $version, which gets no package name"
done
rootfs ubuntu debian 23.10 "Ubuntu 23.10" apt-get
arch=riscv64
expect 0 "" "ubuntu on riscv64, whose kernel list has no release"
arch=x86_64
for version in 24.04 25.10 26.04; do
  rootfs ubuntu debian "$version" "Ubuntu $version" apt-get
  expect 0 "" "ubuntu $version with Trusted Boot" --trusted-boot
done
for version in 20.04 22.04; do
  rootfs ubuntu debian "$version" "Ubuntu $version" apt-get
  expect 1 "kairos-init has none" "ubuntu $version with Trusted Boot" --trusted-boot
done

# Systems no list covers.
rootfs arch "" 20261122.0.629543 "Arch Linux" pacman
expect 1 "no package list for the Arch family" "arch"
rootfs gentoo "" 2.18 "Gentoo Linux"
expect 1 "kairos-init knows neither" "gentoo"
rootfs void "" "" "Void Linux"
expect 1 "builds no initramfs" "void"
rootfs debian "" "" "Debian GNU/Linux forky/sid" apt-get
expect 1 "is not a version" "no VERSION_ID"
root="$(mktemp -d "$TMP/root.XXXXXX")"
expect 1 "has no ID" "no os-release"
rootfs ubuntu debian 24.04 "Ubuntu 24.04"
expect 1 "apt-get is missing" "no package manager"

# Platform.
rootfs alpine "" 3.23.8 "Alpine Linux v3.23" apk systemctl openrc
expect 1 "yip cannot choose" "both init systems"
rootfs ubuntu debian 24.04 "Ubuntu 24.04" apt-get
arch=armv7l
expect 1 "architecture: armv7l" "architecture Kairos is not built for"
arch=x86_64

rootfs ubuntu debian 24.04 "Ubuntu 24.04" apt-get
for arch in aarch64 riscv64; do expect 0 "architecture: $arch" "$arch"; done
arch=x86_64
rootfs alpine "" 3.23.8 "Alpine Linux v3.23" apk
expect 0 "It cannot be installed on a UEFI machine." "no path to shim and GRUB"
arch=aarch64
expect 0 "no BIOS" "no path to shim and GRUB, and no BIOS"
arch=x86_64

# A system that has everything. Nothing brings what is missing after kairos-init.
commands="bash gawk awk sync lvm dracut lsinitrd grub-install grub-editenv mount umount blkid lsblk
  findmnt udevadm depmod modprobe rsync tar cryptsetup e2fsck tune2fs mkfs.ext2 mkfs.ext3 mkfs.ext4
  mkfs.vfat mksquashfs dmsetup sudo less mount.nfs sshd curl NetworkManager systemctl
  systemd-cryptenroll systemd-sysext"
complete() {
  local files
  read -r -d '' -a files <<<"$commands" || true
  rootfs ubuntu debian 24.04 "Ubuntu 24.04" "${files[@]}" /boot/vmlinuz /boot/initrd \
    /usr/lib/systemd/systemd-cryptsetup /usr/lib/grub/i386-pc/modinfo.sh \
    /usr/lib/grub/x86_64-efi/{loopback,squash4,xzio,gzio,regexp}.mod \
    /usr/lib/shim/shimx64.efi.signed /usr/lib/grub/x86_64-efi-signed/grubx64.efi.signed
  mkdir -p "$root/lib/modules/6.8.0"
  : >"$root/lib/modules/6.8.0/modules.builtin"
  local module
  for module in overlay squashfs loop ext4 vfat isofs dm_mod dm_crypt dm_snapshot tpm_crb tpm_tis dm_multipath; do
    echo "kernel/$module.ko.zst:" >>"$root/lib/modules/6.8.0/modules.dep"
  done
}
complete
expect 0 "has what Kairos needs (0 warn)" "complete system" --converted
expect 0 "has what Kairos needs" "complete system with Trusted Boot" --converted --trusted-boot

# Each thing it has is checked: without it there is a fail, or a warn.
# without <0|1> <path> [<flag>...]
without() {
  local status="$1" path="$2" text
  shift 2
  complete
  rm -r "${root:?}$path"
  if [ "$status" = 1 ]; then text="  fail  "; else text="  warn  "; fi
  expect "$status" "$text" "without $path $*" --converted "$@"
}
for command in bash gawk awk sync lvm dracut grub-editenv mount umount blkid lsblk findmnt udevadm depmod \
  modprobe rsync tar cryptsetup e2fsck tune2fs mkfs.ext2 mkfs.ext3 mkfs.ext4 mkfs.vfat sudo less mount.nfs systemctl; do
  without 1 "/usr/bin/$command"
done
for command in lsinitrd grub-install mksquashfs dmsetup sshd curl NetworkManager systemd-cryptenroll systemd-sysext; do
  without 0 "/usr/bin/$command"
done
for path in /boot/vmlinuz /boot/initrd /lib/modules; do without 1 "$path"; done
for path in /usr/lib/grub/i386-pc/modinfo.sh /usr/lib/grub/x86_64-efi/squash4.mod \
  /usr/lib/shim/shimx64.efi.signed /usr/lib/grub/x86_64-efi-signed/grubx64.efi.signed \
  /usr/lib/systemd/systemd-cryptsetup; do
  without 0 "$path"
done
for path in /usr/bin/systemd-cryptenroll /usr/lib/systemd/systemd-cryptsetup /usr/bin/mkfs.vfat; do
  without 1 "$path" --trusted-boot
done
complete
rm -r "$root/boot/initrd" "$root/usr/bin/dracut" "$root/usr/bin/grub-editenv" "$root/usr/lib/shim"
expect 0 "" "Trusted Boot without initramfs tool, GRUB and shim" --converted --trusted-boot

# Something that is there but cannot be used is not there.
complete
chmod -x "$root/usr/bin/rsync"
expect 1 "rsync is missing" "command that is not executable" --converted
complete
rm "$root/usr/bin/rsync"
mkdir "$root/usr/bin/rsync"
expect 1 "rsync is missing" "directory with the name of a command" --converted
complete
ln -sf /nowhere "$root/boot/vmlinuz"
expect 1 "/boot/vmlinuz is missing" "kernel link to nowhere" --converted
complete
mkdir -p "$root/usr/local/bin"
mv "$root/usr/bin/rsync" "$root/usr/local/bin/rsync"
expect 1 "rsync is missing" "command in /usr/local" --converted

# Links are followed inside the root, and never out of it.
complete
mv "$root/usr/bin" "$root/usr/programs"
ln -s /usr/programs "$root/usr/bin"
mv "$root/etc/os-release" "$root/usr/lib/os-release"
ln -s /usr/lib/os-release "$root/etc/os-release"
expect 0 "has what Kairos needs" "absolute links inside the root" --converted
rootfs gentoo "" 2.18 "Gentoo Linux"
rm "$root/etc/os-release"
ln -s /etc/hostname "$root/etc/os-release"
expect 1 "has no ID" "link to a file of the host"
rootfs ubuntu debian 24.04 "Ubuntu 24.04" apt-get
mkdir -p "$root/usr/lib"
mv "$root/etc/os-release" "$root/usr/lib/os-release"
expect 1 "has no ID" "os-release in /usr/lib only"

# Hadron installs no package, and has no mount.nfs.
rootfs hadron "" v0.6.3 "Hadron Linux"
expect 1 "rsync is missing" "hadron without its tools"
complete
printf 'PRETTY_NAME="Hadron"\nID=hadron\nVERSION_ID="main"\n' >"$root/etc/os-release"
rm "$root/usr/bin/mount.nfs"
expect 0 "" "hadron"

# --quiet leaves the pass lines out.
complete
expect 0 "Result:" "result with --quiet" --converted --quiet
absent "  pass  " "pass lines with --quiet" --converted --quiet

# Kernel modules: a module, or built in, with either spelling.
rootfs ubuntu debian 24.04 "Ubuntu 24.04" apt-get
mkdir -p "$root/lib/modules/6.8.0"
for module in overlay squashfs loop ext4 isofs dm-mod; do
  echo "kernel/$module.ko.zst:" >>"$root/lib/modules/6.8.0/modules.dep"
done
echo "kernel/fs/fat/vfat.ko" >"$root/lib/modules/6.8.0/modules.builtin"
expect 0 "module dm_mod" "kernel with the modules"
expect 1 "  fail  rsync" "a system where nothing brings the rest" --converted
expect 1 "module dm_snapshot is missing" "kernel without an optional module" --converted
sed -i '/squashfs/d' "$root/lib/modules/6.8.0/modules.dep"
expect 1 "fail  module squashfs is missing" "kernel without a required module" --converted
expect 0 "module squashfs (install media and recovery image): not installed yet" "base image with a small kernel"
# 6.10 is newer than 6.8.
mkdir -p "$root/lib/modules/6.10.0"
: >"$root/lib/modules/6.10.0/modules.builtin"
expect 0 "kernel: 6.10.0" "the newest kernel"

# The check changes nothing.
before="$(find "$root" -printf '%p %s %T@ %m\n' | sort)"
sh "$SCRIPT" --root "$root" >/dev/null 2>&1 || true
after="$(find "$root" -printf '%p %s %T@ %m\n' | sort)"
if [ "$before" = "$after" ]; then ok; else nok "the check changed the root filesystem"; fi

# Usage errors.
got=0
sh "$SCRIPT" --no-such-flag >/dev/null 2>&1 || got=$?
if [ "$got" = 2 ]; then ok; else nok "unknown flag: exit status $got, want 2"; fi

# --- the lists in the Go sources ---------------------------------------------

# func_body <file> <function>: the body of a Go function.
func_body() { sed -n "/^func $2(/,/^}/p" "$REPO/$1"; }

# same <what> <list> <list>: the lists hold the same words.
same() {
  local want got
  want="$(tr -s ' \n' '\n' <<<"$2" | sed '/^$/d' | sort -u)"
  got="$(tr -s ' \n' '\n' <<<"$3" | sed '/^$/d' | sort -u)"
  if [ -n "$want" ] && [ "$want" = "$got" ]; then ok; else nok "$1 differs between the Go sources (<) and check-base-image.sh (>):
$(diff <(echo "$want") <(echo "$got") || true)"; fi
}

# Every distribution kairos-init knows has a family here, and the same one.
system=kairos-init/pkg/system/system.go
values=kairos-init/pkg/values/values.go
pairs="$(func_body "$system" detectFromID | sed -n 's/.*return values\.\([A-Za-z]*\), values\.\([A-Za-z]*\)Family$/\1 \2/p' | grep -v '^Unknown')"
[ -n "$pairs" ] || nok "no distribution found in $system"
ids=""
while read -r name family; do
  id="$(sed -n "s/^[[:space:]]*${name}[[:space:]]*Distro = \"\(.*\)\"/\1/p" "$REPO/$values")"
  family="$(sed -n "s/^[[:space:]]*${family}Family[[:space:]]*Family = \"\(.*\)\"/\1/p" "$REPO/$values")"
  ids="$ids $id"
  rootfs "$id" "" 1.0 "none"
  expect 1 "family: $family (ID=$id)" "family of $id"
done <<<"$pairs"
same "the list of distributions" "$ids" \
  "$(sed -n '/^family_of()/,/^}/p' "$SCRIPT" | sed -n 's/^    \([a-z].*\)) echo .*/\1/p' | tr '|' ' ')"

# The Ubuntu releases with a kernel, for amd64.
releases="$(sed -n '/^var KernelPackages = /,/^}/p' "$REPO/kairos-init/pkg/values/packagemaps.go" |
  sed -n '/^	Ubuntu:/,/^	},/p' | sed -n '/ArchAMD64:/,/^		},/p' |
  sed -n 's/^			"\([^"]*\)": {/\1/p' | sed 's/||//g; s/28\.04//; s/, !=//')"
same "the list of Ubuntu releases" "$releases" \
  "$(sed -n 's/^          \(20\.04 .*\)) _known=true ;;/\1 >=26.04/p' "$SCRIPT" | tr '|' ' ' | sed 's/28\.04//')"

# Every name the dracut stages match on is matched here, and no other.
# shellcheck disable=SC2016 # the backquotes are Go's
names="$(sed -n 's/^const osDracutFamilies = `\(.*\)`/\1/p' "$REPO/kairos-init/pkg/stages/steps_init.go" |
  sed 's/\\s/ /g; s/\.\*//g; s/\[Oo\]/o/g')"
[ -n "$names" ] || nok "osDracutFamilies not found"
IFS='|' read -r -a names <<<"$names"
for name in "${names[@]}"; do
  rootfs none "" 1.0 "$name 1.0"
  expect 1 "initramfs: dracut" "initramfs for $name"
done
same "the list of names" "$(printf '%s\n' "${names[@]}" OpenSUSE | tr -d ' ')" \
  "$(sed -n '/^initramfs_of()/,/^}/p' "$SCRIPT" | sed '/mkinitfs/d; s/ //g' | grep -oE '\*"?[A-Za-z]+"?\*' | tr -d '*"')"

# The paths of shim and GRUB, for each architecture.
# go_paths <function> <arm64|riscv64|default>
go_paths() {
  func_body sdk/utils/utils.go "$1" | awk -v want="$2" '
    /^\tcase "/ { arch = $2; gsub(/[":]/, "", arch) }
    /^\tdefault:/ { arch = "default" }
    arch == want' | sed -n 's/.*append(modNames, "\([^"]*\)").*/\1/p'
}
# sh_paths <shim_files|grub_files> <x86_64|arm64|*>
sh_paths() {
  # shellcheck disable=SC2016 # matches the text of the script
  sed -n '/^case "\$efi_arch" in/,/^esac/p' "$SCRIPT" | awk -v want="  $2)" '
    index($0, want) == 1 { on = 1 } /^    ;;/ { on = 0 } on' |
    sed -n "/$1=\"/,/\"\$/p" | sed "s/.*$1=//; s/\"//g"
}
for pair in arm64:arm64 default:x86_64 'riscv64:*'; do
  same "the GRUB paths for ${pair#*:}" "$(go_paths GetEfiGrubFiles "${pair%:*}")" "$(sh_paths grub_files "${pair#*:}")"
  [ "${pair%:*}" = riscv64 ] ||
    same "the shim paths for ${pair#*:}" "$(go_paths GetEfiShimFiles "${pair%:*}")" "$(sh_paths shim_files "${pair#*:}")"
done

# The GRUB modules.
modules="$(func_body agent/pkg/constants/constants.go GetGrubModules | grep -o '[a-z0-9]*\.mod')"
same "the list of GRUB modules" "$modules" "$(sed -n 's/^  for _module in \(.*\); do/\1/p' "$SCRIPT")"

# The commands the immucore dracut module puts in the initramfs.
commands="$(sed -n 's/^    inst_check_multiple \(sync .*\)/\1/p' "$REPO/kairos-init/pkg/bundled/bundled.go")"
[ -n "$commands" ] || nok "no command found in the immucore dracut module"
for command in $commands; do
  if grep -qE "^ *component .* $command( |\$)" "$SCRIPT"; then ok; else nok "dracut module command is not checked: $command"; fi
done

echo "check-base-image: $passed passed, $failed failed"
[ "$failed" -eq 0 ]
