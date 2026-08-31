package bundled

// JetsonQSPIScriptPath is where the QSPI update script is installed in the image.
const JetsonQSPIScriptPath = "/usr/sbin/kairos-jetson-qspi-update"

// JetsonQSPIScript checks the board's QSPI boot firmware version against the L4T version
// baked into the image, and stages a UEFI capsule update when the image is newer.
//
// It deliberately does not use `dpkg-reconfigure nvidia-l4t-bootloader`: that package's
// postinst also overwrites the ESP's EFI/BOOT/BOOTAA64.efi with NVIDIA's L4TLauncher
// (which would replace Kairos's own bootloader) and rewrites /boot/extlinux/extlinux.conf.
// We reproduce only the two operations that actually stage the capsule.
//
// The board is detected from the devicetree SoC compatible string (nvidia,tegra264),
// which is uniform across every Thor variant. /etc/nv_boot_control.conf is deliberately
// NOT used: no L4T package ships it, NVIDIA's own installer generates it, so a Kairos
// image never has it and gating on it made this whole feature inert.
//
// See kairos-io/kairos#4228.
const JetsonQSPIScript = `#!/bin/bash
set -euo pipefail

# All roots are overridable so the logic can be tested without hardware.
ESRT_FW_VERSION_FILE="${ESRT_FW_VERSION_FILE:-/sys/firmware/efi/esrt/entries/entry0/fw_version}"
DT_COMPATIBLE_FILE="${DT_COMPATIBLE_FILE:-/sys/firmware/devicetree/base/compatible}"
OTA_PACKAGE_DIR="${OTA_PACKAGE_DIR:-/opt/ota_package}"
EFIVARS_DIR="${EFIVARS_DIR:-/sys/firmware/efi/efivars}"
ESP_MOUNT_DIR="${ESP_MOUNT_DIR:-/run/kairos-esp}"
ESP_LABEL="${ESP_LABEL:-COS_GRUB}"

# Below this the board cannot be updated in-band and needs a USB host flash.
FACTORY_QSPI_VER="2490368" # 38.0.0

log()  { echo "[kairos-qspi] $*"; }
fail() { echo "[kairos-qspi] ERROR: $*" >&2; exit 1; }

# Mounts we are responsible for tearing down again.
esp_mounted=0
efivars_mounted=0

cleanup() {
	local rc=$?
	if [ "${efivars_mounted}" = "1" ]; then
		efivars_mounted=0
		if ! umount "${EFIVARS_DIR}"; then
			echo "[kairos-qspi] ERROR: cannot unmount ${EFIVARS_DIR}" >&2
			rc=1
		fi
	fi
	if [ "${esp_mounted}" = "1" ]; then
		esp_mounted=0
		# Flush before unmounting: kairos-agent holds a concurrent rw mount of the
		# same ESP (see the mount section below), so leaving dirty pages behind is
		# not acceptable.
		sync || true
		if ! umount "${ESP_MOUNT_DIR}"; then
			echo "[kairos-qspi] ERROR: cannot unmount ${ESP_MOUNT_DIR}; the ESP may still hold unwritten data" >&2
			rc=1
			# TRADE-OFF (deliberate, not a bug): if we get here after a successful
			# stage, the capsule is already on the ESP and OsIndications is already
			# set - the board WILL flash firmware on next boot regardless of this
			# script's exit code. Forcing rc=1 still aborts the install on an
			# unmount failure that has nothing left to protect. We considered
			# clearing OsIndications here to make the abort "clean", but that write
			# has its own failure modes (immutability, a wedged efivarfs) and would
			# turn a recoverable warning into another way to fail mid-cleanup for no
			# real benefit. So we accept the noisy, occasionally-unnecessary abort.
		fi
	fi
	exit "${rc}"
}
# INT/TERM (not just EXIT) so a killed install still tears down the ESP mount
# instead of leaking it at ESP_MOUNT_DIR, which would otherwise block
# kairos-agent's own teardown of the underlying device.
trap cleanup EXIT INT TERM

# encode_version 39.2.0 -> 2556416. Accepts 2 or 3 numeric components; anything
# else is an error rather than a silently wrong number.
encode_version() {
	local v="$1" branch major minor rest c
	case "${v}" in
		*.*.*.*) return 1 ;;
		*.*.*)
			branch="${v%%.*}"
			rest="${v#*.}"
			major="${rest%%.*}"
			minor="${rest#*.}"
			;;
		*.*)
			branch="${v%%.*}"
			major="${v#*.}"
			minor=0
			;;
		*) return 1 ;;
	esac
	for c in "${branch}" "${major}" "${minor}"; do
		case "${c}" in
			'' | *[!0-9]*) return 1 ;;
		esac
	done
	echo $(( (branch << 16) | (major << 8) | minor ))
}

# decode_version 2556416 -> 39.2.0. Inverse of encode_version, used so every
# user-facing message carries a version humans recognise, not a raw integer.
decode_version() {
	local v="$1"
	case "${v}" in
		'' | *[!0-9]*) return 1 ;;
	esac
	echo "$(( v >> 16 )).$(( (v >> 8) & 255 )).$(( v & 255 ))"
}

# Hidden entrypoints for tests.
if [ "${1:-}" = "__encode_version" ]; then
	encode_version "$2"
	exit 0
fi
if [ "${1:-}" = "__decode_version" ]; then
	decode_version "$2"
	exit 0
fi

# --- board gate ---------------------------------------------------------------
# The SoC compatible string is the only identifier that is uniform across all Thor
# variants (AGX, IGX, devkit); the model strings differ between them. It lives in
# plain sysfs, so it is readable inside the install chroot's bind-mounted /sys.
if [ ! -r "${DT_COMPATIBLE_FILE}" ]; then
	fail "cannot read ${DT_COMPATIBLE_FILE}; cannot determine whether this board is a Jetson Thor"
fi
# The property is a list of NUL-separated strings.
dt_compatible="$(tr '\000' '\n' < "${DT_COMPATIBLE_FILE}")" ||
	fail "cannot read ${DT_COMPATIBLE_FILE}; cannot determine whether this board is a Jetson Thor"
if [ -z "${dt_compatible}" ]; then
	fail "${DT_COMPATIBLE_FILE} is empty; cannot determine whether this board is a Jetson Thor"
fi

case "${dt_compatible}" in
	*nvidia,tegra264*) payload_subdir="t26x" ;;
	*)
		log "SoC is not an nvidia,tegra264 (Jetson Thor) board; skipping"
		exit 0
		;;
esac

# --- current firmware version -------------------------------------------------
if [ ! -r "${ESRT_FW_VERSION_FILE}" ]; then
	fail "cannot read current firmware version from ${ESRT_FW_VERSION_FILE}"
fi
current_qspi_ver="$(tr -d '[:space:]' < "${ESRT_FW_VERSION_FILE}")" ||
	fail "cannot read current firmware version from ${ESRT_FW_VERSION_FILE}"
if ! [ "${current_qspi_ver}" -eq "${current_qspi_ver}" ] 2>/dev/null; then
	fail "cannot read current firmware version from ${ESRT_FW_VERSION_FILE}"
fi

# --- expected version from the image ------------------------------------------
if [ -n "${KAIROS_QSPI_IMAGE_VERSION:-}" ]; then
	image_pkg_ver="${KAIROS_QSPI_IMAGE_VERSION}"
else
	image_pkg_ver="$(dpkg-query -W -f='${Version}' nvidia-l4t-bootloader 2>/dev/null | cut -d- -f1)" || image_pkg_ver=""
fi
[ -n "${image_pkg_ver}" ] || fail "cannot determine the L4T bootloader version in this image"
image_ver="$(encode_version "${image_pkg_ver}")" ||
	fail "cannot parse the L4T bootloader version '${image_pkg_ver}' found in this image"

current_qspi_str="$(decode_version "${current_qspi_ver}")" ||
	fail "cannot decode the board firmware version ${current_qspi_ver}"
image_str="$(decode_version "${image_ver}")" ||
	fail "cannot decode the image L4T version ${image_ver}"
factory_str="$(decode_version "${FACTORY_QSPI_VER}")" ||
	fail "cannot decode the supported firmware floor ${FACTORY_QSPI_VER}"

log "board firmware: ${current_qspi_str} (${current_qspi_ver}), image L4T: ${image_str} (${image_ver})"

# --- decision -----------------------------------------------------------------
if [ "${current_qspi_ver}" -lt "${FACTORY_QSPI_VER}" ]; then
	fail "board firmware ${current_qspi_str} (${current_qspi_ver}) is below the supported floor ${factory_str} (${FACTORY_QSPI_VER}).
This board cannot be updated from a running system and needs a USB host flash.
See https://canonical-ubuntu-for-jetson.readthedocs-hosted.com/latest/how-to/flash/"
fi

if [ "${current_qspi_ver}" -gt "${image_ver}" ]; then
	fail "board firmware ${current_qspi_str} (${current_qspi_ver}) is newer than this image, which is L4T ${image_str} (${image_ver}).
UEFI capsule update cannot downgrade firmware, so this board would not boot.
Use a Kairos image built for L4T ${current_qspi_str}, or reflash the board.
See https://github.com/kairos-io/kairos/issues/4228"
fi

if [ "${current_qspi_ver}" -eq "${image_ver}" ]; then
	log "board firmware ${current_qspi_str} already matches the image; nothing to do"
	exit 0
fi

log "board firmware ${current_qspi_str} is older than the image's ${image_str}; staging capsule update"

# A dry run must exit before anything that requires the real image layout
# (e.g. OTA_PACKAGE_DIR), since the decision-logic tests exercise this seam
# without staging fixtures in place.
if [ "${KAIROS_QSPI_DRY_RUN:-0}" = "1" ]; then
	log "dry run; not staging"
	exit 0
fi

# --- efivarfs -----------------------------------------------------------------
# kairos-agent prepares the install chroot with a NON-recursive bind mount of /sys.
# efivarfs is a separate filesystem mounted at /sys/firmware/efi/efivars, so it is
# NOT carried into the chroot: inside, that path is an empty plain sysfs directory.
# Writing there with a shell redirect would create a regular file, the script would
# report success, and the board would never receive the capsule - the exact silent
# failure this whole feature exists to prevent. So: mount efivarfs ourselves, verify
# the target really is efivarfs, and never, ever create the directory.
is_efivarfs() {
	local d="$1" fstype
	fstype="$(stat -f -c %T "${d}" 2>/dev/null)" || fstype=""
	if [ "${fstype}" = "efivarfs" ]; then
		return 0
	fi
	if command -v findmnt >/dev/null 2>&1; then
		fstype="$(findmnt -n -o FSTYPE --target "${d}" 2>/dev/null)" || fstype=""
		if [ "${fstype}" = "efivarfs" ]; then
			return 0
		fi
	fi
	return 1
}

[ -d "${EFIVARS_DIR}" ] ||
	fail "${EFIVARS_DIR} does not exist; this system does not look like it booted through UEFI"

# KAIROS_QSPI_SKIP_EFIVARS_CHECK bypasses only the filesystem-type verification, so
# tests can point EFIVARS_DIR at a temp dir. It never skips the write itself, and
# nothing on the production path sets it.
if [ "${KAIROS_QSPI_SKIP_EFIVARS_CHECK:-0}" != "1" ]; then
	if ! is_efivarfs "${EFIVARS_DIR}"; then
		mount -t efivarfs efivarfs "${EFIVARS_DIR}" ||
			fail "cannot mount efivarfs at ${EFIVARS_DIR}"
		efivars_mounted=1
	fi
	is_efivarfs "${EFIVARS_DIR}" ||
		fail "${EFIVARS_DIR} is not an efivarfs mount; refusing to write the capsule flag to a plain directory, which would silently do nothing"
fi

# --- locate the payload -------------------------------------------------------
# The capsule filename encodes the L4T build and the board, and NVIDIA renames it
# between releases. Glob for it instead of hardcoding, but insist on exactly one
# match so we never stage an ambiguous payload.
payload_dir="${OTA_PACKAGE_DIR}/${payload_subdir}"
shopt -s nullglob
capsules=("${payload_dir}"/*.Cap)
shopt -u nullglob
if [ "${#capsules[@]}" -eq 0 ]; then
	fail "no capsule payload (*.Cap) found in ${payload_dir}"
fi
if [ "${#capsules[@]}" -gt 1 ]; then
	fail "expected exactly one capsule payload in ${payload_dir}, found ${#capsules[@]}: ${capsules[*]}"
fi
capsule_src="${capsules[0]}"
[ -r "${capsule_src}" ] || fail "capsule payload ${capsule_src} is not readable"
capsule_size="$(stat -c %s "${capsule_src}")" || fail "cannot stat ${capsule_src}"
capsule_kb=$(( (capsule_size + 1023) / 1024 ))

# --- mount the ESP ------------------------------------------------------------
# HAZARD: kairos-agent already holds this same ESP mounted read-write at /run/cos/efi
# for the whole install, and has just installed GRUB onto it. /run is not bind-mounted
# into the chroot, so the mount below is a SECOND, independent rw mount of the same
# vfat device, and we write tens of megabytes through it while the agent's mount still
# holds dirty FAT metadata. The correct fix is for kairos-agent to pass its existing
# ESP mount into the chroot as an extraMount, which is out of scope here; until then we
# keep this window as short as possible and sync before unmounting (see cleanup()).
mkdir -p "${ESP_MOUNT_DIR}" || fail "cannot create ${ESP_MOUNT_DIR}"
if [ "${KAIROS_QSPI_SKIP_MOUNT:-0}" != "1" ]; then
	esp_dev="$(blkid -L "${ESP_LABEL}")" || fail "cannot find an ESP labelled ${ESP_LABEL}"
	mount "${esp_dev}" "${ESP_MOUNT_DIR}" || fail "cannot mount ${esp_dev} at ${ESP_MOUNT_DIR}"
	esp_mounted=1
fi

# --- capacity check -----------------------------------------------------------
if [ -n "${KAIROS_QSPI_FORCE_FREE_KB:-}" ]; then
	# Test hook: let tests simulate a full ESP without needing real block devices.
	free_kb="${KAIROS_QSPI_FORCE_FREE_KB}"
else
	free_kb="$(df -Pk "${ESP_MOUNT_DIR}" | awk 'NR==2 {print $4}')" ||
		fail "cannot determine free space on ${ESP_MOUNT_DIR}"
fi
# A successful pipeline that produced no output leaves free_kb empty, and any
# non-numeric value makes the comparison below return 2, which set -e does not
# catch inside an if condition - the guard would pass and staging would proceed.
if ! [ "${free_kb}" -eq "${free_kb}" ] 2>/dev/null; then
	fail "cannot determine free space on ${ESP_MOUNT_DIR}: got '${free_kb}'"
fi
# The comparison is deliberately -lt, not -le: free space exactly equal to the
# payload size is accepted. Do not "correct" this to -le.
if [ "${free_kb}" -lt "${capsule_kb}" ]; then
	fail "not enough free space on the ESP: need ${capsule_kb} KiB, have ${free_kb} KiB.
       The Kairos ESP may need to be larger for this board."
fi

# --- stage --------------------------------------------------------------------
capsule_dir="${ESP_MOUNT_DIR}/EFI/UpdateCapsule"
mkdir -p "${capsule_dir}" || fail "cannot create ${capsule_dir}"
cp "${capsule_src}" "${capsule_dir}/" || fail "cannot copy ${capsule_src} to ${capsule_dir}"
# Flush the payload out before we advertise it through OsIndications, and before the
# concurrent mount described above can be torn down under us.
sync || fail "cannot flush the staged capsule to the ESP"
log "staged ${capsule_src##*/} on the ESP"

# Set bit 2 of OsIndications (EFI_OS_INDICATIONS_FILE_CAPSULE_DELIVERY_SUPPORTED).
# UEFI applies the capsule on the next boot, before Linux starts.
osind="${EFIVARS_DIR}/OsIndications-8be4df61-93ca-11d2-aa0d-00e098032b8c"

# efivarfs marks every variable it discovers at mount time S_IMMUTABLE. If this
# variable already exists on the board (it usually does), opening it for write
# fails with EPERM no matter how we write it. Try to clear the immutable
# attribute first; chattr may be missing (minimal images) or may fail for other
# reasons (kernel that never set the flag) - either is tolerated here, because
# the write immediately below is the real, hard gate, not this best-effort step.
if [ -e "${osind}" ]; then
	chattr -i "${osind}" 2>/dev/null || true
fi

# dd (rather than a redirect) because efivarfs rejects an O_TRUNC open, and
# conv=notrunc tells dd not to attempt to truncate the destination before
# writing (a plain shell ">" redirect would open O_TRUNC and fail). This does
# NOT address the immutable attribute - only the chattr above does that, and
# even that is best-effort; a stubborn EPERM below is still possible.
# iflag=fullblock guards against a short read: a single read() on the printf
# pipe below is not guaranteed to return all 12 bytes, and a short read here
# would silently write a truncated, garbage OsIndications value.
printf '\x07\x00\x00\x00\x04\x00\x00\x00\x00\x00\x00\x00' |
	dd of="${osind}" bs=12 count=1 conv=notrunc iflag=fullblock status=none ||
	fail "cannot write ${osind}. If this variable already exists on the board, the
efivarfs immutable attribute is the likely cause: chattr -i \"${osind}\" may need
to be run manually, or this kernel may not support clearing it in-band."

log "firmware will be updated on the next boot"
`
