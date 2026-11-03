#!/usr/bin/env bash
#
# Turns a `git describe --tags --always --dirty` string into the version
# stamped into every binary, which kairos-init writes into images as
# KAIROS_INIT_VERSION and the upgrade downgrade gate compares with semver.
#
# A build between tags becomes a pre-release of the next patch version, the
# way Go pseudo-versions work, with the commit in build metadata:
#
#   v4.4.0                       -> v4.4.0                  (tagged, unchanged)
#   v4.4.0-108-gfa680143         -> v4.4.1-dev.108+gfa680143
#   v4.4.0-108-gfa680143-dirty   -> v4.4.1-dev.108+gfa680143.dirty
#   v4.5.0-rc1-3-gabc1234        -> v4.5.0-rc1.dev.3+gabc1234
#   v4.4.0-dirty                 -> v4.4.0+dirty
#
# Plain semver then orders the builds: v4.4.0 < v4.4.1-dev.50 < v4.4.1-dev.108
# < v4.4.1 < v4.5.0. The git describe form, v4.4.0-108-gfa680143, reads as a
# pre-release of v4.4.0 and ranks below it, so an agent released with the gate
# refuses to upgrade to it. Build metadata alone (v4.4.0+108.gfa680143) ranks
# equal to the tag and to every other build after it.
#
# Anything else (a bare commit hash, "dev") is printed unchanged.
#
# Usage: binary-version.sh <git describe output>

set -euo pipefail

if [ $# -ne 1 ]; then
  echo "usage: $0 <git describe output>" >&2
  exit 2
fi

in="$1"
dirty=""
rest="$in"
if [[ "$rest" == *-dirty ]]; then
  dirty="dirty"
  rest="${rest%-dirty}"
fi

semver_re='^(v?)([0-9]+)\.([0-9]+)\.([0-9]+)(-[0-9A-Za-z.-]+)?$'

if [[ "$rest" =~ ^(.+)-([0-9]+)-g([0-9a-f]+)$ ]]; then
  tag="${BASH_REMATCH[1]}"
  n="${BASH_REMATCH[2]}"
  sha="${BASH_REMATCH[3]}"
  meta="g$sha${dirty:+.$dirty}"
  if [[ "$tag" =~ $semver_re ]]; then
    if [ -n "${BASH_REMATCH[5]}" ]; then
      echo "$tag.dev.$n+$meta"
    else
      echo "${BASH_REMATCH[1]}${BASH_REMATCH[2]}.${BASH_REMATCH[3]}.$((BASH_REMATCH[4] + 1))-dev.$n+$meta"
    fi
    exit 0
  fi
elif [ -n "$dirty" ] && [[ "$rest" =~ $semver_re ]]; then
  echo "$rest+dirty"
  exit 0
fi

echo "$in"
