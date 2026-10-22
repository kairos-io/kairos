#!/usr/bin/env bash

set -euo pipefail

# Default UKI size gate. Both are policy, not physics, so tune them here or
# override them per run with the flags below.
#
# A UKI is the kernel, initrd and command line in one signed PE binary, and
# the firmware reads the whole of it into memory before it runs it. The first
# limit is a ceiling per .efi, high enough to sit well above what the amd64
# hadron-trusted core and standard UKIs weigh today, and low enough that a UKI
# heading for the size where firmware starts refusing to load it gets caught
# in review instead of on a machine.
#
# The second limit catches a jump that is still under the ceiling. A routine
# kernel or package bump moves a UKI by a few percent, so growing by more than
# 10% against the last green master build is worth a human looking at it.
DEFAULT_MAX_EFI_MIB=1024
DEFAULT_MAX_EFI_GROWTH_PCT=10

usage() {
  cat <<'EOF'
Usage:
  scripts/iso-size-diff.sh [options] <baseline-root> <candidate-root>

Compares the size of the ISOs built for this PR against the ones from the
last successful master build and prints a Markdown report to stdout.

Each root is a directory holding one sub-directory per downloaded artifact
(as produced by actions/download-artifact and gh run download), and each of
those sub-directories contains the built ISO. Artifacts are paired by their
sub-directory name, which is stable across builds even though the ISO file
names carry a per-build version suffix.

UKI ISOs (named *-uki.iso) also get the .efi files inside their EFI image
measured one by one. The ISO table is informational only, but the UKI table
is a gate: the script exits 1 after printing the report when a UKI .efi is
bigger than the ceiling, or grew by more than the allowed percentage against
the same file in the baseline. Reading the .efi files needs xorriso and
mtools.

Options:
  --max-efi-mib <n>         Ceiling for any UKI .efi, in MiB (default 1024)
  --max-efi-growth-pct <n>  Allowed growth of a UKI .efi against the baseline,
                            in percent (default 10)
  --require-uki             Fail when no UKI ISO is found under the candidate
                            root, so a renamed artifact cannot turn the gate
                            into a silent pass
  -h, --help                Show this help
EOF
}

die() {
  printf 'Error: %s\n' "$*" >&2
  exit 1
}

iso_in() {
  [[ -d "$1" ]] || return 0
  find "$1" -maxdepth 2 -type f -name '*.iso' ! -name '*ipxe*' 2>/dev/null | head -n1 || true
}

is_uki_iso() {
  [[ "$1" == *-uki.iso ]]
}

file_size() {
  # Read the size straight from the filesystem metadata so we don't stream
  # multi-GiB ISOs just to count bytes. GNU stat wants -c, BSD/macOS wants -f,
  # and wc is the last resort if neither is around.
  stat -c%s "$1" 2>/dev/null \
    || stat -f%z "$1" 2>/dev/null \
    || wc -c <"$1" | tr -d '[:space:]'
}

human() {
  awk -v b="$1" 'BEGIN { printf "%.2f MiB", b / 1048576 }'
}

delta() {
  local base="$1" cand="$2" diff
  diff=$((cand - base))
  if [[ "$base" -eq 0 ]]; then
    printf '%s' "$(human "$diff")"
    return
  fi
  awk -v d="$diff" -v b="$base" \
    'BEGIN { printf "%+.2f MiB (%+.2f%%)", d / 1048576, d / b * 100 }'
}

# Prints "<name> <bytes>" for every UKI under EFI/kairos in the EFI image of a
# UKI ISO. EFI/BOOT only holds systemd-boot, which is not a UKI and is small
# enough that a percentage gate on it would only produce noise.
efi_sizes_in() {
  local iso="$1" work esp f
  work="$(mktemp -d "$WORK_ROOT/efi.XXXXXX")"

  # build-uki puts the .efi files in a FAT image at the ISO root, so pull that
  # out first and then read it with mtools, the same tools that wrote it.
  if ! xorriso -osirrox on -indev "$iso" -extract / "$work/iso" >"$work/xorriso.log" 2>&1; then
    cat "$work/xorriso.log" >&2
    rm -rf "$work"
    return 1
  fi

  # Without Rock Ridge the image may be recorded as EFIBOOT.IMG;1.
  esp="$(find "$work/iso" -maxdepth 1 -type f -iname 'efiboot.img*' | head -n1)"
  if [[ -z "$esp" ]]; then
    printf 'No efiboot.img at the root of %s\n' "$iso" >&2
    rm -rf "$work"
    return 1
  fi

  mkdir -p "$work/efi"
  if ! MTOOLS_SKIP_CHECK=1 mcopy -n -s -i "$esp" ::/EFI/kairos "$work/efi/" >"$work/mcopy.log" 2>&1; then
    cat "$work/mcopy.log" >&2
    rm -rf "$work"
    return 1
  fi

  while IFS= read -r f; do
    printf '%s %s\n' "$(basename "$f")" "$(file_size "$f")"
  done < <(find "$work/efi" -type f -iname '*.efi' | sort)

  rm -rf "$work"
}

is_number() {
  [[ "$1" =~ ^[0-9]+$ ]]
}

BASELINE_ROOT=""
CANDIDATE_ROOT=""
MAX_EFI_MIB="$DEFAULT_MAX_EFI_MIB"
MAX_EFI_GROWTH_PCT="$DEFAULT_MAX_EFI_GROWTH_PCT"
REQUIRE_UKI=false

while [[ $# -gt 0 ]]; do
  case "$1" in
    -h|--help)
      usage
      exit 0
      ;;
    --max-efi-mib)
      [[ $# -ge 2 ]] || die "--max-efi-mib needs a value"
      is_number "$2" || die "--max-efi-mib wants a whole number of MiB, got: $2"
      MAX_EFI_MIB="$2"
      shift
      ;;
    --max-efi-growth-pct)
      [[ $# -ge 2 ]] || die "--max-efi-growth-pct needs a value"
      is_number "$2" || die "--max-efi-growth-pct wants a whole number, got: $2"
      MAX_EFI_GROWTH_PCT="$2"
      shift
      ;;
    --require-uki)
      REQUIRE_UKI=true
      ;;
    --*)
      die "Unknown option: $1"
      ;;
    *)
      if [[ -z "$BASELINE_ROOT" ]]; then
        BASELINE_ROOT="$1"
      elif [[ -z "$CANDIDATE_ROOT" ]]; then
        CANDIDATE_ROOT="$1"
      else
        die "Unexpected argument: $1"
      fi
      ;;
  esac
  shift
done

[[ -z "$BASELINE_ROOT" || -z "$CANDIDATE_ROOT" ]] && { usage; exit 1; }
[[ -d "$CANDIDATE_ROOT" ]] || die "Candidate root not found: $CANDIDATE_ROOT"

WORK_ROOT="$(mktemp -d)"
trap 'rm -rf "$WORK_ROOT"' EXIT

# A baseline is only useful if we actually downloaded at least one master ISO.
# When it's missing (no baseline run, or the artifacts expired) we still want a
# report, just one that's honest about being PR-only.
baseline_available=false
if [[ -d "$BASELINE_ROOT" && -n "$(iso_in "$BASELINE_ROOT")" ]]; then
  baseline_available=true
fi

printf '## ISO size diff\n\n'
if [[ "$baseline_available" == true ]]; then
  printf 'PR ISOs compared against the last successful master build.\n\n'
else
  printf 'No master baseline was available, so this lists the PR ISO sizes only.\n\n'
fi
printf '| Artifact | master | PR | Δ |\n'
printf '| --- | --- | --- | --- |\n'

rows=0
uki_dirs=()
for cand_dir in "$CANDIDATE_ROOT"/*/; do
  [[ -d "$cand_dir" ]] || continue
  cand_iso="$(iso_in "$cand_dir")"
  [[ -n "$cand_iso" ]] || continue
  is_uki_iso "$cand_iso" && uki_dirs+=("$cand_dir")

  name="$(basename "$cand_dir")"
  name="${name%.iso.zip}"
  cand_size="$(file_size "$cand_iso")"

  base_iso=""
  [[ -d "$BASELINE_ROOT" ]] && base_iso="$(iso_in "$BASELINE_ROOT/$(basename "$cand_dir")")"

  if [[ -n "$base_iso" ]]; then
    base_size="$(file_size "$base_iso")"
    printf '| %s | %s | %s | %s |\n' \
      "$name" "$(human "$base_size")" "$(human "$cand_size")" \
      "$(delta "$base_size" "$cand_size")"
  elif [[ "$baseline_available" == true ]]; then
    # We have a baseline overall, this artifact just isn't in it, so it's new.
    printf '| %s | _n/a_ | %s | _new_ |\n' "$name" "$(human "$cand_size")"
  else
    # No baseline at all, so there's nothing to call this ISO new against.
    printf '| %s | _n/a_ | %s | _n/a_ |\n' "$name" "$(human "$cand_size")"
  fi
  rows=$((rows + 1))
done

if [[ "$rows" -eq 0 ]]; then
  die "No candidate ISOs found under $CANDIDATE_ROOT"
fi

if [[ "${#uki_dirs[@]}" -eq 0 ]]; then
  if [[ "$REQUIRE_UKI" == true ]]; then
    die "No UKI ISOs (*-uki.iso) found under $CANDIDATE_ROOT"
  fi
  exit 0
fi

command -v xorriso >/dev/null 2>&1 || die "xorriso is needed to read the UKI ISOs"
command -v mcopy >/dev/null 2>&1 || die "mcopy (mtools) is needed to read the UKI EFI images"

max_efi_bytes=$((MAX_EFI_MIB * 1048576))

printf '\n## UKI size check\n\n'
printf 'Each .efi must stay under %s MiB' "$MAX_EFI_MIB"
if [[ "$baseline_available" == true ]]; then
  printf ' and grow by no more than %s%% against master' "$MAX_EFI_GROWTH_PCT"
fi
printf '.\n\n'
printf '| Artifact | EFI | master | PR | Δ | Result |\n'
printf '| --- | --- | --- | --- | --- | --- |\n'

failures=()
efi_rows=0
for cand_dir in "${uki_dirs[@]}"; do
  artifact="$(basename "$cand_dir")"
  name="${artifact%.iso.zip}"
  cand_iso="$(iso_in "$cand_dir")"

  if ! cand_efis="$(efi_sizes_in "$cand_iso")"; then
    die "Could not read the EFI image of $cand_iso"
  fi
  if [[ -z "$cand_efis" ]]; then
    die "No .efi files under EFI/kairos in $cand_iso"
  fi

  base_efis=""
  base_iso=""
  [[ -d "$BASELINE_ROOT" ]] && base_iso="$(iso_in "$BASELINE_ROOT/$artifact")"
  if [[ -n "$base_iso" ]]; then
    # A broken baseline should not block the PR, it only drops the growth
    # check for this artifact. The ceiling still applies.
    base_efis="$(efi_sizes_in "$base_iso" 2>/dev/null)" || base_efis=""
  fi

  while read -r efi cand_size; do
    base_size="$(awk -v n="$efi" '$1 == n { print $2; exit }' <<<"$base_efis")"
    result="ok"

    if [[ "$cand_size" -gt "$max_efi_bytes" ]]; then
      result="**over ${MAX_EFI_MIB} MiB**"
      failures+=("$name/$efi is $(human "$cand_size"), over the ${MAX_EFI_MIB} MiB ceiling")
    elif [[ -n "$base_size" && "$base_size" -gt 0 ]] \
      && awk -v c="$cand_size" -v b="$base_size" -v p="$MAX_EFI_GROWTH_PCT" \
        'BEGIN { exit !((c - b) * 100 > b * p) }'; then
      result="**grew over ${MAX_EFI_GROWTH_PCT}%**"
      failures+=("$name/$efi grew $(delta "$base_size" "$cand_size"), over the ${MAX_EFI_GROWTH_PCT}% limit")
    fi

    if [[ -n "$base_size" ]]; then
      printf '| %s | %s | %s | %s | %s | %s |\n' \
        "$name" "$efi" "$(human "$base_size")" "$(human "$cand_size")" \
        "$(delta "$base_size" "$cand_size")" "$result"
    else
      printf '| %s | %s | _n/a_ | %s | _n/a_ | %s |\n' \
        "$name" "$efi" "$(human "$cand_size")" "$result"
    fi
    efi_rows=$((efi_rows + 1))
  done <<<"$cand_efis"
done

if [[ "${#failures[@]}" -gt 0 ]]; then
  printf '\nUKI size check failed:\n\n'
  for f in "${failures[@]}"; do
    printf -- '- %s\n' "$f"
  done
  printf 'UKI size check failed for %d of %d .efi files\n' "${#failures[@]}" "$efi_rows" >&2
  exit 1
fi
