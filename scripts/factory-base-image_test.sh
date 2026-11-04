#!/usr/bin/env bash
#
# Runs the base_image parsing block of .github/workflows/reusable-factory.yaml
# (between the BEGIN/END base_image parsing markers) against a table of
# images and checks the FLAVOR and FLAVOR_RELEASE it derives.
# Usage: factory-base-image_test.sh [workflow-file]

set -euo pipefail

WORKFLOW="${1:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/.github/workflows/reusable-factory.yaml}"

BLOCK="$(sed -n '/# BEGIN base_image parsing/,/# END base_image parsing/p' "$WORKFLOW" | sed 's/^          //')"
if [[ -z "$BLOCK" ]]; then
  echo "base_image parsing markers not found in $WORKFLOW" >&2
  exit 1
fi

failed=0
passed=0

# check <base_image> <flavor> <flavor_release>
check() {
  local got
  got="$(INPUTS_BASE_IMAGE="$1" bash -c "$BLOCK"'
echo "$FLAVOR $FLAVOR_RELEASE"')"
  if [[ "$got" == "$2 $3" ]]; then
    passed=$((passed + 1))
    echo "ok   $1 -> $got"
  else
    failed=$((failed + 1))
    echo "FAIL $1 -> got '$got', want '$2 $3'"
  fi
}

check "ubuntu:24.04" ubuntu 24.04
check "opensuse/leap:15.6" opensuse leap-15.6
check "ghcr.io/kairos-io/hadron:main" hadron main
check "ghcr.io/kairos-io/hadron:v0.6.3" hadron v0.6.3
check "quay.io/kairos/ubuntu:22.04" ubuntu 22.04
check "ubuntu" ubuntu latest
check "opensuse/leap" opensuse leap-latest
check "ghcr.io/kairos-io/hadron" hadron latest
check "localhost:5000/kairos-io/hadron:main" hadron main
check "localhost:5000/kairos-io/hadron" hadron latest
check "ubuntu@sha256:0000000000000000000000000000000000000000000000000000000000000000" ubuntu latest
check "ubuntu:24.04@sha256:0000000000000000000000000000000000000000000000000000000000000000" ubuntu 24.04
check "ghcr.io/kairos-io/hadron:main@sha256:0000000000000000000000000000000000000000000000000000000000000000" hadron main

echo "$passed passed, $failed failed"
[[ $failed -eq 0 ]]
