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

# runs <sha:time:status:conclusion>...: writes runs.json in the given order.
# The url is https://x/<sha>.
runs() {
  local spec sep=""
  local sha at st con
  printf '[' >"$tmp/runs.json"
  for spec in "$@"; do
    IFS=: read -r sha at st con <<<"$spec"
    printf '%s{"headSha":"%s","createdAt":"2026-11-%sT06:00:00Z","status":"%s","conclusion":"%s","url":"https://x/%s"}' \
      "$sep" "$sha" "$at" "$st" "$con" "$sha" >>"$tmp/runs.json"
    sep=","
  done
  printf ']' >>"$tmp/runs.json"
}

# pick <file>: runs the script on a file, filling $out and $status.
pick() {
  status=0
  out="$("$SCRIPT" "$1" 2>/dev/null)" || status=$?
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

# check_no_stdout <case>: the last run printed nothing on stdout.
check_no_stdout() {
  if [ -n "$out" ]; then
    echo "FAIL: $1: stdout not empty" >&2
    failed=$((failed + 1))
  fi
}

R="$tmp/runs.json"

runs c3:09:completed:success c2:08:completed:success
pick "$R"
check "newest success" 0 state=green sha=c3

runs c3:09:completed:failure c2:08:completed:success
pick "$R"
check "newest failure, older success" 2 state=red sha=c3 conclusion=failure url=https://x/c3

runs c3:09:completed:cancelled c2:08:completed:success
pick "$R"
check "newest cancelled" 0 state=green sha=c2

runs c3:09:in_progress: c2:08:completed:failure
pick "$R"
check "newest in progress" 2 state=red sha=c2 conclusion=failure

runs c3:09:completed:skipped c2:08:completed:success
pick "$R"
check "newest skipped" 0 state=green sha=c2

runs c3:09:completed:timed_out
pick "$R"
check "timed_out is red" 2 state=red sha=c3 conclusion=timed_out url=https://x/c3

runs c1:07:completed:success c2:08:completed:failure c3:09:completed:success
pick "$R"
check "oldest first input" 0 state=green sha=c3

echo '[]' >"$R"
pick "$R"
check "empty array" 3 state=none

runs c3:09:completed:cancelled c2:08:in_progress:
pick "$R"
check "nothing finished" 3 state=none

runs c3:09:completed:cancelled c2:08:completed:cancelled
pick "$R"
check "only cancelled" 3 state=none

echo '{bad' >"$tmp/bad.json"
pick "$tmp/bad.json"
check "not json" 1
check_no_stdout "not json"

echo '{"headSha":"c1"}' >"$tmp/object.json"
pick "$tmp/object.json"
check "json object" 1
check_no_stdout "json object"

pick "$tmp/missing.json"
check "missing file" 1
check_no_stdout "missing file"

status=0
out="$("$SCRIPT" 2>/dev/null)" || status=$?
check "no argument" 1
check_no_stdout "no argument"

echo "green-commit: $passed passed, $failed failed"
[ "$failed" -eq 0 ]
