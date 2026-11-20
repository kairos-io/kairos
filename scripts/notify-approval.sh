#!/usr/bin/env bash
#
# Tells the maintainers that a release is built and waits for approval.
#
# usage: notify-approval.sh <tag> <run url>
#
# Posts to the Slack incoming webhook in SLACK_RELEASE_WEBHOOK. When the secret
# is not set, or the post fails, the text is printed as a workflow warning and
# the script still exits 0: a Slack outage must not stop the release from
# reaching approval.

set -euo pipefail

if [ "$#" -ne 2 ]; then
  echo "usage: $0 <tag> <run url>" >&2
  exit 2
fi

tag="$1"
url="$2"
msg="Kairos ${tag} is built and waiting for approval: ${url}"

if [ -z "${SLACK_RELEASE_WEBHOOK:-}" ]; then
  echo "::warning::${msg}"
  exit 0
fi

if ! curl -fsS -X POST -H 'Content-Type: application/json' \
  --data "$(jq -n --arg t "$msg" '{text:$t}')" "$SLACK_RELEASE_WEBHOOK"; then
  echo "::warning::Slack post failed: ${msg}"
fi
