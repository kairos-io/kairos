#!/usr/bin/env bash
#
# Runs scripts/binary-version.sh against git describe strings and checks the
# version it prints.

set -euo pipefail

SCRIPT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/binary-version.sh"

failed=0
passed=0

# expect <want> <git describe output>
expect() {
  local want="$1" in="$2" got
  if got="$("$SCRIPT" "$in" 2>/dev/null)" && [ "$got" = "$want" ]; then
    passed=$((passed + 1))
  else
    echo "FAIL: want '$want', got '${got:-<error>}' for: $in" >&2
    failed=$((failed + 1))
  fi
}

# Tagged builds are unchanged.
expect v4.4.0 v4.4.0
expect v4.5.0-rc1 v4.5.0-rc1
expect v4.4.0+dirty v4.4.0-dirty
expect v4.5.0-rc1+dirty v4.5.0-rc1-dirty

# Between tags: a pre-release of the next patch.
expect v4.4.1-dev.108+gfa680143 v4.4.0-108-gfa680143
expect v4.4.1-dev.108+gfa680143.dirty v4.4.0-108-gfa680143-dirty
expect v4.4.10-dev.1+gabc1234 v4.4.9-1-gabc1234
expect 4.4.1-dev.2+gabc1234 4.4.0-2-gabc1234

# Between tags after a pre-release tag: extend the pre-release.
expect v4.5.0-rc1.dev.3+gabc1234 v4.5.0-rc1-3-gabc1234
expect v4.5.0-rc1.dev.3+gabc1234.dirty v4.5.0-rc1-3-gabc1234-dirty
expect v4.5.0-beta.2.dev.7+gabc1234 v4.5.0-beta.2-7-gabc1234

# Not a semver tag: unchanged.
expect 8c24a223 8c24a223
expect 8c24a223-dirty 8c24a223-dirty
expect dev dev
expect foo-3-gabc1234 foo-3-gabc1234

echo "binary-version: $passed passed, $failed failed"
[ "$failed" -eq 0 ]
