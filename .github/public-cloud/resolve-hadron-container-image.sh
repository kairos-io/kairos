#!/bin/bash

# Given a kairos release tag (e.g. "v4.1.0"), resolve the hadron container image
# that this release was actually built with, and print the full reference to stdout:
#
#   quay.io/kairos/hadron:<hadron-version>-core-amd64-generic-<kairos-tag>
#
# The hadron version is NOT hardcoded: it is discovered from the published hadron
# tags on quay (whose names embed both versions), so the cloud upload jobs always
# pull the image matching the release instead of a version we have to bump by hand.

set -e
set -o pipefail

latestTag="$1"
if [ -z "$latestTag" ]; then
  echo "usage: $0 <kairos-tag>   (e.g. $0 v4.1.0)" >&2
  exit 1
fi

suffix="-core-amd64-generic-${latestTag}"
hadronVersion=$(curl -fsSL "https://quay.io/api/v1/repository/kairos/hadron/tag/?onlyActiveTags=true&limit=100&filter_tag_name=like:core-amd64-generic-${latestTag}" \
  | jq -r --arg suf "$suffix" '.tags[].name | select(endswith($suf)) | rtrimstr($suf)' \
  | sort -Vu | tail -n1)

if [ -z "$hadronVersion" ]; then
  echo "Could not resolve hadron version for ${latestTag} from quay.io/kairos/hadron tags" >&2
  exit 1
fi

echo "Resolved hadron version for ${latestTag}: ${hadronVersion}" >&2
echo "quay.io/kairos/hadron:${hadronVersion}${suffix}"
