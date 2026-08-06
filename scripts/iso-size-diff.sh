#!/usr/bin/env bash

set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  scripts/iso-size-diff.sh <baseline-root> <candidate-root>

Compares the size of the ISOs built for this PR against the ones from the
last successful master build and prints a Markdown report to stdout.

Each root is a directory holding one sub-directory per downloaded artifact
(as produced by actions/download-artifact and gh run download), and each of
those sub-directories contains the built ISO. Artifacts are paired by their
sub-directory name, which is stable across builds even though the ISO file
names carry a per-build version suffix.
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

file_size() {
  wc -c <"$1" | tr -d '[:space:]'
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

BASELINE_ROOT=""
CANDIDATE_ROOT=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    -h|--help)
      usage
      exit 0
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

printf '## ISO size diff\n\n'
printf 'PR ISOs compared against the last successful master build.\n\n'
printf '| Artifact | master | PR | Δ |\n'
printf '| --- | --- | --- | --- |\n'

rows=0
for cand_dir in "$CANDIDATE_ROOT"/*/; do
  [[ -d "$cand_dir" ]] || continue
  cand_iso="$(iso_in "$cand_dir")"
  [[ -n "$cand_iso" ]] || continue

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
  else
    printf '| %s | _n/a_ | %s | _new_ |\n' "$name" "$(human "$cand_size")"
  fi
  rows=$((rows + 1))
done

if [[ "$rows" -eq 0 ]]; then
  die "No candidate ISOs found under $CANDIDATE_ROOT"
fi
