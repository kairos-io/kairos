#!/usr/bin/env bash
#
# Runs scripts/semver-bump.sh against commit messages and pull request titles,
# and checks the bump it prints and its exit status.

set -euo pipefail

SCRIPT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/semver-bump.sh"

failed=0
passed=0

# expect <bump> <message>: the script prints <bump> and exits 0.
expect() {
  local want="$1" msg="$2" got
  if got="$("$SCRIPT" "$msg" 2>/dev/null)" && [ "$got" = "$want" ]; then
    passed=$((passed + 1))
  else
    echo "FAIL: want '$want', got '${got:-<error>}' for: $msg" >&2
    failed=$((failed + 1))
  fi
}

# reject <message>: the script exits non-zero and prints nothing on stdout.
reject() {
  local msg="$1" got
  if got="$("$SCRIPT" "$msg" 2>/dev/null)"; then
    echo "FAIL: accepted as '$got': $msg" >&2
    failed=$((failed + 1))
  elif [ -n "$got" ]; then
    echo "FAIL: rejected but printed '$got': $msg" >&2
    failed=$((failed + 1))
  else
    passed=$((passed + 1))
  fi
}

expect minor 'feat(agent): add --dry-run to upgrade'
expect minor 'feat: selinux support (non-uki) (#4519)'
expect patch 'fix(grub): load all_video only when the module file exists (#5524)'
expect patch 'docs(agent): name install.encrypted_partitions in the install config guide (#5871)'
expect patch 'chore(deps): update docker/build-push-action action to v7.4.0'
expect patch 'fix(deps): update module github.com/foo/bar to v1.2.3'
for type in build chore ci docs perf refactor revert style test; do
  expect patch "$type: something"
done
expect patch 'Revert "feat(sysext): cover the live media extension sweep"'

expect major 'feat(agent)!: drop the --legacy flag'
expect major 'fix!: change the default partition layout'
expect major $'feat(agent): rename the install keys\n\nBREAKING CHANGE: install.foo is now install.bar'
expect major $'fix: tighten the schema\n\nBREAKING-CHANGE: unknown keys are errors'
# Only a footer at the start of a line counts.
expect patch $'fix: mention it\n\nThis is not a BREAKING CHANGE: just text.'

reject ':arrow_up: Update docker/build-push-action action to v7.4.0'
reject '✨ feat(agent): support explicit registry credentials (#4839)'
reject 'Relax config'
reject 'go mod tidy'
reject 'tests: add a test'
reject 'feature: add dry run to kairos-init (#4594)'
reject 'Feat: capitalised type'
reject 'feat(agent):no space after the colon'
reject 'feat(agent): '
reject 'feat(): empty scope'
reject 'feat agent: missing colon'
reject ''

echo "semver-bump: $passed passed, $failed failed"
[ "$failed" -eq 0 ]
