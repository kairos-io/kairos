#!/usr/bin/env bash
#
# Points the AuroraBoot chart at the release image repository.
#
# usage: auroraboot-release-values.sh <values.yaml>
#
# The values file in the repository names ghcr.io/kairos-io/kairos/auroraboot,
# where every push to master publishes the :master image that a checkout of the
# chart deploys. A release publishes its image to quay.io/kairos/auroraboot, so
# the release job runs this on a copy of the chart before `helm package`.
#
# The file is changed in place, and only when exactly one line names the
# repository of the repository's own default. Any other layout exits 1 and leaves
# the file alone: a values file that changed shape must not be packaged with the
# wrong image.

set -euo pipefail

from='  repository: ghcr.io/kairos-io/kairos/auroraboot'
to='  repository: quay.io/kairos/auroraboot'

if [ "$#" -ne 1 ] || [ ! -f "$1" ]; then
  echo "usage: $0 <values.yaml>" >&2
  exit 2
fi

values="$1"
matches="$(grep -cxF "$from" "$values" || true)"
if [ "$matches" != 1 ]; then
  echo "expected exactly one line '${from}' in ${values}, found ${matches}" >&2
  exit 1
fi

tmp="$(mktemp)"
trap 'rm -f "$tmp"' EXIT
while IFS= read -r line || [ -n "$line" ]; do
  if [ "$line" = "$from" ]; then
    printf '%s\n' "$to"
  else
    printf '%s\n' "$line"
  fi
done <"$values" >"$tmp"
cat "$tmp" >"$values"
