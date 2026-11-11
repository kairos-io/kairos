#!/usr/bin/env bash
#
# Decides whether master is green from its master.yaml push runs alone.
#
# Usage: green-commit.sh <runs.json>
#
# runs.json is `gh run list --workflow master.yaml --branch master
# --event push --json headSha,status,conclusion,url,createdAt` output.
#
# The runs are sorted by createdAt, newest first. Runs that have not
# completed and runs that were cancelled or skipped have no verdict and are
# ignored. The first run left decides. Older runs are never consulted once
# that run is red, so the script never falls back to an older green commit.
#
# Exit 1 on a missing argument, an unreadable file or input that is not a
# JSON array, with the reason on stderr.
#
# Prints key=value lines:
#   green: state=green sha=<sha>                              exit 0
#   red:   state=red sha=<sha> conclusion=<conclusion> url=   exit 2
#   none:  state=none                                         exit 3

set -euo pipefail

if [ "$#" -ne 1 ]; then
  echo "usage: ${0##*/} <runs.json>" >&2
  exit 1
fi

if ! result="$(jq -r '
  if type != "array" then error("not an array") else . end
  | [ .[]
      | select(.status == "completed" and .conclusion != "cancelled" and .conclusion != "skipped") ]
  | sort_by(.createdAt) | reverse | first
  | if . == null then
      "state=none"
    elif .conclusion == "success" then
      "state=green\nsha=\(.headSha)"
    else
      "state=red\nsha=\(.headSha)\nconclusion=\(.conclusion)\nurl=\(.url)"
    end
' "$1" 2>/dev/null)"; then
  echo "${0##*/}: cannot read $1 as a JSON array" >&2
  exit 1
fi

printf '%s\n' "$result"

case "$result" in
  state=green*) exit 0 ;;
  state=red*) exit 2 ;;
  *) exit 3 ;;
esac
