#!/usr/bin/env bash
#
# Runs scripts/notify-approval.sh with a fake curl on PATH and checks what it
# posts, what it prints and its exit status.

set -euo pipefail

SCRIPT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/notify-approval.sh"

failed=0
passed=0
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

mkdir "$tmp/bin"
cat >"$tmp/bin/curl" <<'FAKE'
#!/usr/bin/env bash
echo "$@" >"$FAKE_CURL_ARGS"
[ "${FAKE_CURL_FAIL:-false}" = true ] && exit 22
exit 0
FAKE
chmod +x "$tmp/bin/curl"

out=""
status=0

# notify <tag> <url>: runs the script, filling $out and $status, and leaving
# the curl arguments in $tmp/args (absent when curl was not called).
notify() {
  rm -f "$tmp/args"
  status=0
  out="$(PATH="$tmp/bin:$PATH" FAKE_CURL_ARGS="$tmp/args" "$SCRIPT" "$1" "$2" 2>&1)" || status=$?
}

ok() {
  echo "ok   $1"
  passed=$((passed + 1))
}

bad() {
  echo "FAIL $1: $2"
  failed=$((failed + 1))
}

# With a webhook the message goes to it as JSON text naming the tag and run.
SLACK_RELEASE_WEBHOOK=https://hooks.example/x notify v4.5.2 https://gh/run/1
args="$(cat "$tmp/args" 2>/dev/null || true)"
if [ "$status" = 0 ] && [[ "$args" == *"https://hooks.example/x"* ]] \
  && [[ "$args" == *"v4.5.2"* ]] && [[ "$args" == *"https://gh/run/1"* ]] \
  && [[ "$args" == *"waiting for approval"* ]]; then
  ok "posts the message to the webhook"
else
  bad "posts the message to the webhook" "status=$status args=$args"
fi

# Without a webhook nothing is posted and the text becomes a warning.
SLACK_RELEASE_WEBHOOK= notify v4.5.2 https://gh/run/1
if [ "$status" = 0 ] && [ ! -e "$tmp/args" ] && [[ "$out" == "::warning::"*"v4.5.2"* ]]; then
  ok "warns when the webhook is not set"
else
  bad "warns when the webhook is not set" "status=$status out=$out"
fi

# A failed post never fails the job: the release must still reach approval.
SLACK_RELEASE_WEBHOOK=https://hooks.example/x FAKE_CURL_FAIL=true notify v4.5.2 https://gh/run/1
if [ "$status" = 0 ] && [[ "$out" == *"::warning::Slack post failed"* ]]; then
  ok "a failed post is a warning, not a failure"
else
  bad "a failed post is a warning, not a failure" "status=$status out=$out"
fi

# Missing arguments are a usage error.
status=0
"$SCRIPT" >/dev/null 2>&1 || status=$?
if [ "$status" = 2 ]; then
  ok "usage error without arguments"
else
  bad "usage error without arguments" "status=$status"
fi

echo "$passed passed, $failed failed"
[ "$failed" = 0 ]
