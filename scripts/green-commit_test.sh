#!/usr/bin/env bash
#
# Runs scripts/green-commit.sh against JSON fixtures and checks the key=value
# lines it prints and its exit status.

set -euo pipefail

SCRIPT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/green-commit.sh"

failed=0
passed=0
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

out=""
status=0

# commits <sha>...: writes commits.json, newest first.
commits() {
  local sha
  printf '[' >"$tmp/commits.json"
  local sep=""
  for sha in "$@"; do
    printf '%s{"sha":"%s","commit":{"message":"msg %s"}}' "$sep" "$sha" "$sha" >>"$tmp/commits.json"
    sep=","
  done
  printf ']' >>"$tmp/commits.json"
}

# runs <sha:workflow:status:conclusion:url>...: writes runs.json.
runs() {
  local spec sep=""
  local sha wf st con url
  printf '[' >"$tmp/runs.json"
  for spec in "$@"; do
    IFS=: read -r sha wf st con url <<<"$spec"
    printf '%s{"workflowName":"%s","headSha":"%s","status":"%s","conclusion":"%s","url":"https://x/%s"}' \
      "$sep" "$wf" "$sha" "$st" "$con" "${url:-$sha-$wf}" >>"$tmp/runs.json"
    sep=","
  done
  printf ']' >>"$tmp/runs.json"
}

# pick: runs the script on the fixtures, filling $out and $status.
pick() {
  status=0
  out="$("$SCRIPT" "$tmp/commits.json" "$tmp/runs.json" 2>/dev/null)" || status=$?
}

# check <case> <want status> <key=value>...: the last run had that status and printed those lines.
check() {
  local name="$1" want_status="$2" line
  shift 2
  if [ "$status" -ne "$want_status" ]; then
    echo "FAIL: $name: want exit $want_status, got $status" >&2
    failed=$((failed + 1))
    return
  fi
  for line in "$@"; do
    if ! grep -qxF "$line" <<<"$out"; then
      echo "FAIL: $name: missing '$line' in: $(tr '\n' ' ' <<<"$out")" >&2
      failed=$((failed + 1))
      return
    fi
  done
  passed=$((passed + 1))
}

commits c5 c4 c3 c2 c1

runs c5:master:completed:success c5:CodeQL:completed:success
pick
check "newest green" 0 state=green sha=c5

runs c5:master:in_progress: c4:master:completed:success
pick
check "newest still running" 0 state=green sha=c4

runs c5:master:completed:cancelled c4:master:completed:success
pick
check "cancelled gate" 0 state=green sha=c4

runs c5:CodeQL:completed:success c4:master:completed:success
pick
check "docs-only commit has no gate run" 0 state=green sha=c4

runs c5:master:completed:success c5:CodeQL:completed:failure c4:master:completed:success
pick
check "other workflow red" 2 state=red sha=c5 since=c5 workflow=CodeQL url=https://x/c5-CodeQL

runs c5:master:completed:success c5:zizmor:queued: c4:master:completed:success c4:zizmor:completed:success
pick
check "other workflow still running" 0 state=green sha=c4

runs c5:master:completed:failure c4:master:completed:failure c2:master:completed:success
pick
check "red streak" 2 state=red sha=c5 since=c4 workflow=master url=https://x/c5-master

runs c5:master:completed:timed_out
pick
check "timed_out is red" 2 state=red sha=c5

runs c5:master:completed:success "c5:Scorecard analysis workflow:completed:neutral" "c5:Build Examples:completed:skipped"
pick
check "skipped and neutral are green" 0 state=green sha=c5

runs c5:master:in_progress: c4:master:in_progress: c3:master:queued: c2:master:waiting: c1:CodeQL:completed:success
pick
check "nothing decided" 3 state=none

runs c5:master:completed:failure c5:CodeQL:completed:failure c4:master:completed:success
pick
check "first red run on sha" 2 state=red sha=c5 workflow=master

runs c5:master:completed:startup_failure c4:master:completed:action_required c3:master:completed:success
pick
check "startup_failure and action_required are red" 2 state=red sha=c5 since=c4

echo "green-commit: $passed passed, $failed failed"
[ "$failed" -eq 0 ]
