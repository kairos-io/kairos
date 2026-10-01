#!/usr/bin/env bash
#
# Runs scripts/iso-size-diff.sh against small ISOs built on the fly, laid out
# the way build-uki lays them out: a FAT efiboot.img at the ISO root, with the
# UKIs under EFI/kairos. Needs xorriso and mtools.

set -euo pipefail

SCRIPT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/iso-size-diff.sh"
MIB=1048576

for tool in xorriso mformat mmd mcopy; do
  command -v "$tool" >/dev/null 2>&1 || { echo "missing $tool" >&2; exit 1; }
done

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

failed=0
passed=0

# make_plain_iso <root> <artifact-dir> <iso-name> <bytes>
make_plain_iso() {
  local dir="$1/$2" stage
  stage="$(mktemp -d "$WORK/stage.XXXXXX")"
  mkdir -p "$dir"
  head -c "$4" /dev/zero >"$stage/rootfs"
  xorriso -as mkisofs -quiet -o "$dir/$3" "$stage" >/dev/null 2>&1
}

# make_uki_iso <root> <artifact-dir> <iso-name> <efi-name>=<bytes>...
make_uki_iso() {
  local dir="$1/$2" iso="$3" stage img total=0 spec
  shift 3
  stage="$(mktemp -d "$WORK/stage.XXXXXX")"
  mkdir -p "$dir" "$stage/iso" "$stage/files"

  for spec in "$@"; do
    head -c "${spec#*=}" /dev/urandom >"$stage/files/${spec%%=*}"
    total=$((total + ${spec#*=}))
  done
  head -c 4096 /dev/urandom >"$stage/BOOTX64.EFI"

  img="$stage/iso/efiboot.img"
  head -c $((total + 4 * MIB)) /dev/zero >"$img"
  mformat -i "$img" -F ::
  mmd -i "$img" ::EFI ::EFI/BOOT ::EFI/kairos
  mcopy -i "$img" "$stage/BOOTX64.EFI" ::EFI/BOOT/BOOTX64.EFI
  for spec in "$@"; do
    mcopy -i "$img" "$stage/files/${spec%%=*}" "::EFI/kairos/${spec%%=*}"
  done

  xorriso -as mkisofs -quiet -V UKI_ISO_INSTALL -e efiboot.img -no-emul-boot \
    -o "$dir/$iso" "$stage/iso" >/dev/null 2>&1
}

# make_uki_iso_no_kairos_dir <root> <artifact-dir> <iso-name>
#
# A FAT efiboot.img that only has EFI/BOOT (systemd-boot), the way an ISO
# built with a different UKI layout, or a partial/corrupted build, might
# look. This is a different failure than a missing efiboot.img: xorriso and
# mcopy both succeed on the image itself, it just doesn't have EFI/kairos in
# it, so the error has to come from the mcopy step, not the "no efiboot.img"
# check.
make_uki_iso_no_kairos_dir() {
  local dir="$1/$2" iso="$3" stage img
  stage="$(mktemp -d "$WORK/stage.XXXXXX")"
  mkdir -p "$dir" "$stage/iso"

  head -c 4096 /dev/urandom >"$stage/BOOTX64.EFI"
  img="$stage/iso/efiboot.img"
  head -c $((4 * MIB)) /dev/zero >"$img"
  mformat -i "$img" -F ::
  mmd -i "$img" ::EFI ::EFI/BOOT
  mcopy -i "$img" "$stage/BOOTX64.EFI" ::EFI/BOOT/BOOTX64.EFI

  xorriso -as mkisofs -quiet -V UKI_ISO_INSTALL -e efiboot.img -no-emul-boot \
    -o "$dir/$iso" "$stage/iso" >/dev/null 2>&1
}

# expect <name> <want-exit> <want-in-output|-> <args...>
expect() {
  local name="$1" want="$2" grep_for="$3" out rc=0
  shift 3
  out="$("$SCRIPT" "$@" 2>&1)" || rc=$?
  if [[ "$rc" -ne "$want" ]]; then
    printf 'FAIL %s: exit %d, want %d\n%s\n' "$name" "$rc" "$want" "$out"
    failed=$((failed + 1))
    return
  fi
  if [[ "$grep_for" != "-" ]] && ! grep -qF -- "$grep_for" <<<"$out"; then
    printf 'FAIL %s: output is missing %q\n%s\n' "$name" "$grep_for" "$out"
    failed=$((failed + 1))
    return
  fi
  printf 'ok   %s\n' "$name"
  passed=$((passed + 1))
}

GENERIC="kairos-hadron-v0.5.3-core-amd64-generic.iso.zip"
UKI="kairos-hadron-trusted-v0.5.1-core-amd64-generic-uki.iso.zip"

# Generic ISOs only: informational report, never a failure.
make_plain_iso "$WORK/generic/base" "$GENERIC" kairos-a.iso $((2 * MIB))
make_plain_iso "$WORK/generic/cand" "$GENERIC" kairos-b.iso $((3 * MIB))
expect "generic ISO growth does not gate" 0 "| kairos-hadron-v0.5.3-core-amd64-generic |" \
  "$WORK/generic/base" "$WORK/generic/cand"
expect "--require-uki fails without UKI ISOs" 1 "No UKI ISOs" \
  --require-uki "$WORK/generic/base" "$WORK/generic/cand"

# UKI within both limits.
make_uki_iso "$WORK/small/base" "$UKI" kairos-a-uki.iso norole.efi=$((2 * MIB)) single_entry_testentry.efi=$((2 * MIB))
make_uki_iso "$WORK/small/cand" "$UKI" kairos-b-uki.iso norole.efi=$((2 * MIB + 100000)) single_entry_testentry.efi=$((2 * MIB))
expect "UKI under the ceiling and the growth limit passes" 0 "| norole.efi |" \
  --require-uki "$WORK/small/base" "$WORK/small/cand"
expect "BOOTX64.EFI is not measured" 0 "single_entry_testentry.efi" \
  "$WORK/small/base" "$WORK/small/cand"
if "$SCRIPT" "$WORK/small/base" "$WORK/small/cand" 2>&1 | grep -qi "bootx64"; then
  printf 'FAIL BOOTX64.EFI showed up in the UKI table\n'
  failed=$((failed + 1))
fi

# UKI over the ceiling.
expect "UKI over the ceiling fails" 1 "over the 1 MiB ceiling" \
  --max-efi-mib 1 "$WORK/small/base" "$WORK/small/cand"
expect "UKI over the ceiling fails without a baseline" 1 "over the 1 MiB ceiling" \
  --max-efi-mib 1 "$WORK/missing" "$WORK/small/cand"

# UKI growing past the limit against the baseline.
make_uki_iso "$WORK/grow/cand" "$UKI" kairos-b-uki.iso norole.efi=$((3 * MIB)) single_entry_testentry.efi=$((2 * MIB))
expect "UKI growing 50% fails the default 10% limit" 1 "norole.efi grew" \
  "$WORK/small/base" "$WORK/grow/cand"
expect "UKI growing 50% passes a 60% limit" 0 "| ok |" \
  --max-efi-growth-pct 60 "$WORK/small/base" "$WORK/grow/cand"
expect "UKI growth is not gated without a baseline" 0 "_n/a_" \
  "$WORK/missing" "$WORK/grow/cand"

# Generic and UKI side by side, the way the PR job downloads them.
mkdir -p "$WORK/mixed/base" "$WORK/mixed/cand"
cp -r "$WORK/generic/base/$GENERIC" "$WORK/small/base/$UKI" "$WORK/mixed/base/"
cp -r "$WORK/generic/cand/$GENERIC" "$WORK/grow/cand/$UKI" "$WORK/mixed/cand/"
expect "mixed set lists both ISOs" 0 "| kairos-hadron-trusted-v0.5.1-core-amd64-generic-uki |" \
  --max-efi-growth-pct 60 "$WORK/mixed/base" "$WORK/mixed/cand"
expect "mixed set still gates the UKI" 1 "norole.efi grew" \
  --require-uki "$WORK/mixed/base" "$WORK/mixed/cand"

# A UKI ISO without an EFI image is an error, not a pass.
make_plain_iso "$WORK/broken/cand" "$UKI" kairos-b-uki.iso $MIB
expect "UKI ISO without efiboot.img is an error" 1 "Could not read the EFI image" \
  "$WORK/missing" "$WORK/broken/cand"

# A UKI ISO with a valid efiboot.img but no EFI/kairos dir inside it is also
# an error, not a quietly empty UKI table.
mkdir -p "$WORK/nokairos/cand"
make_uki_iso_no_kairos_dir "$WORK/nokairos/cand" "$UKI" kairos-b-uki.iso
expect "UKI efiboot.img without EFI/kairos is an error" 1 "Could not read the EFI image" \
  "$WORK/missing" "$WORK/nokairos/cand"

expect "bad flag value is rejected" 1 "whole number" \
  --max-efi-mib lots "$WORK/small/base" "$WORK/small/cand"

printf '\n%d passed, %d failed\n' "$passed" "$failed"
[[ "$failed" -eq 0 ]]
