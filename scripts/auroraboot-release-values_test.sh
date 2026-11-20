#!/usr/bin/env bash
#
# Runs scripts/auroraboot-release-values.sh on sample values files and checks
# what it changes and when it refuses.

set -euo pipefail

SCRIPT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/auroraboot-release-values.sh"

failed=0
passed=0
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

ok() {
  echo "ok   $1"
  passed=$((passed + 1))
}

bad() {
  echo "FAIL $1: $2"
  failed=$((failed + 1))
}

cat >"$tmp/values.yaml" <<'YAML'
host: ""

image:
  repository: ghcr.io/kairos-io/kairos/auroraboot
  tag: ""
  pullPolicy: IfNotPresent

imagePullSecrets: []
YAML

# The repository goes to quay and nothing else changes.
cp "$tmp/values.yaml" "$tmp/out.yaml"
status=0
"$SCRIPT" "$tmp/out.yaml" >/dev/null 2>&1 || status=$?
changed="$(diff "$tmp/values.yaml" "$tmp/out.yaml" | grep -c '^[<>]' || true)"
if [ "$status" = 0 ] && grep -qx '  repository: quay.io/kairos/auroraboot' "$tmp/out.yaml" && [ "$changed" = 2 ]; then
  ok "points the image at quay and changes one line only"
else
  bad "points the image at quay and changes one line only" "status=$status changed=$changed"
fi

# A second run finds nothing to change and refuses, so a rerun cannot hide a
# values file that was edited by hand.
status=0
"$SCRIPT" "$tmp/out.yaml" >/dev/null 2>&1 || status=$?
if [ "$status" = 1 ]; then
  ok "refuses a file that already points at quay"
else
  bad "refuses a file that already points at quay" "status=$status"
fi

# A values file whose repository line moved elsewhere must not pass unnoticed.
printf 'image:\n  repository: example.org/other/auroraboot\n' >"$tmp/other.yaml"
status=0
"$SCRIPT" "$tmp/other.yaml" >/dev/null 2>&1 || status=$?
if [ "$status" = 1 ] && grep -q 'example.org/other' "$tmp/other.yaml"; then
  ok "refuses an unexpected repository and leaves the file alone"
else
  bad "refuses an unexpected repository and leaves the file alone" "status=$status"
fi

# Two matching lines are ambiguous.
{ cat "$tmp/values.yaml"; printf 'extra:\n  repository: ghcr.io/kairos-io/kairos/auroraboot\n'; } >"$tmp/two.yaml"
status=0
"$SCRIPT" "$tmp/two.yaml" >/dev/null 2>&1 || status=$?
if [ "$status" = 1 ]; then
  ok "refuses a file with the repository twice"
else
  bad "refuses a file with the repository twice" "status=$status"
fi

# Usage errors.
status=0
"$SCRIPT" >/dev/null 2>&1 || status=$?
if [ "$status" = 2 ]; then
  ok "usage error without arguments"
else
  bad "usage error without arguments" "status=$status"
fi
status=0
"$SCRIPT" "$tmp/missing.yaml" >/dev/null 2>&1 || status=$?
if [ "$status" = 2 ]; then
  ok "usage error for a missing file"
else
  bad "usage error for a missing file" "status=$status"
fi

echo "$passed passed, $failed failed"
[ "$failed" = 0 ]
