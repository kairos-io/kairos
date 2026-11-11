#!/usr/bin/env bash
#
# Runs scripts/release-version.sh against throwaway git repositories and checks
# the key=value lines it prints, its exit status and the notes file.

set -euo pipefail

SCRIPT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/release-version.sh"

failed=0
passed=0
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

repo=""
out=""
status=0

# git_t <args>: runs git in the current repository without any signing.
git_t() {
  git -c user.name=t -c user.email=t@t -c commit.gpgsign=false -c tag.gpgsign=false "$@"
}

# new_repo: creates an empty repository and makes it the current one.
new_repo() {
  repo="$(mktemp -d -p "$tmp")"
  git -C "$repo" init -q -b master
}

# commit <subject>: adds an empty commit to the current repository.
commit() {
  git_t -C "$repo" commit -q --allow-empty -m "$1"
}

# tag <name>: tags HEAD of the current repository.
tag() {
  git_t -C "$repo" tag "$1"
}

# run <args>: runs the script in the current repository, filling $out and $status.
run() {
  status=0
  out="$(cd "$repo" && "$SCRIPT" "$@" 2>/dev/null)" || status=$?
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

new_repo
commit 'chore: init'
tag v4.4.0
commit 'fix(a): x'
commit 'docs: y'
run HEAD
check "patch only" 0 version=v4.4.1 previous=v4.4.0 base=v4.4.0 bump=patch commits=2
run --bump minor HEAD
check "override minor" 0 version=v4.5.0 bump=minor
run --bump major HEAD
check "override major" 0 version=v5.0.0 bump=major
run --bump auto HEAD
check "override auto" 0 version=v4.4.1 bump=patch

new_repo
commit 'chore: init'
tag v4.4.0
commit 'fix: x'
commit 'feat(agent): y'
run HEAD
check "feat" 0 version=v4.5.0 bump=minor
run --notes "$tmp/notes.md" HEAD
notes="$(cat "$tmp/notes.md" 2>/dev/null || true)"
if [ "$(wc -l <<<"$notes")" -eq 2 ] &&
  [[ "$(sed -n 1p <<<"$notes")" =~ ^-\ minor:\ feat\(agent\):\ y\ \([0-9a-f]{7}\)$ ]] &&
  [[ "$(sed -n 2p <<<"$notes")" =~ ^-\ patch:\ fix:\ x\ \( ]]; then
  passed=$((passed + 1))
else
  echo "FAIL: notes: got: $notes" >&2
  failed=$((failed + 1))
fi

new_repo
commit 'chore: init'
tag v4.4.0
commit 'feat!: z'
run HEAD
check "breaking under auto" 4 bump=major version=v5.0.0

new_repo
commit 'chore: init'
tag v4.4.0
run HEAD
check "nothing landed" 3 commits=0

new_repo
commit 'chore: init'
tag v4.4.0
commit 'fix: a'
tag v4.5.0-rc1
commit 'fix: b'
run HEAD
check "rc ignored" 0 previous=v4.4.0 base=v4.4.0 commits=2 version=v4.4.1

new_repo
commit 'chore: init'
tag v4.4.0
git_t -C "$repo" checkout -q -b release/v4.4
commit 'fix: backport'
tag v4.4.1
git_t -C "$repo" checkout -q master
commit 'fix: c'
run HEAD
check "backport tag" 0 previous=v4.4.1 base=v4.4.0 commits=1 version=v4.4.2

new_repo
commit 'chore: init'
tag v4.4.0
commit 'fix: a'
tag v4.4.1
git_t -C "$repo" checkout -q -b release/v4.4
commit 'fix: backport'
tag v4.4.2
git_t -C "$repo" checkout -q master
run HEAD
check "higher backport on another branch" 3 previous=v4.4.2 base=v4.4.1 commits=0
commit 'fix: b'
run HEAD
check "commit after a higher backport" 0 previous=v4.4.2 base=v4.4.1 commits=1 version=v4.4.3

new_repo
commit 'chore: init'
tag v4.4.0
commit 'feat: b'
tag v4.5.0
commit 'fix: c'
run HEAD
check "pending minor is not counted again" 0 version=v4.5.1 previous=v4.5.0 base=v4.5.0 bump=patch commits=1

new_repo
commit 'chore: init'
tag v4.4.0
commit 'feat!: b'
tag v5.0.0
commit 'fix: c'
run HEAD
check "pending major is not counted again" 0 version=v5.0.1 previous=v5.0.0 base=v5.0.0 bump=patch commits=1

new_repo
commit 'chore: init'
tag v4.4.0
commit 'fix: a'
tag v4.4.1
run HEAD
check "pending prerelease is not cut again" 3 previous=v4.4.1 base=v4.4.1 commits=0
run --bump minor --notes "$tmp/notes3.md" HEAD
check "explicit bump re-cuts at the same commit" 0 version=v4.5.0 base=v4.4.1 commits=0 bump=minor
if [ -f "$tmp/notes3.md" ] && [ ! -s "$tmp/notes3.md" ]; then
  passed=$((passed + 1))
else
  echo "FAIL: empty notes: got: $(cat "$tmp/notes3.md" 2>&1)" >&2
  failed=$((failed + 1))
fi
echo v4.4.0 >"$tmp/tags.txt"
run --final-tags "$tmp/tags.txt" HEAD
check "final-tags option is rejected" 1

new_repo
commit 'chore: init'
tag v4.4.0
commit 'Relax config'
run --notes "$tmp/notes2.md" HEAD
check "unconventional counts as patch" 0 bump=patch
if grep -qF -- '- unchecked: Relax config (' "$tmp/notes2.md"; then
  passed=$((passed + 1))
else
  echo "FAIL: unconventional notes: got: $(cat "$tmp/notes2.md")" >&2
  failed=$((failed + 1))
fi

new_repo
commit 'chore: init'
tag v4.4.0
commit 'fix: a'
tag v4.4.1
commit 'fix: b'
run HEAD
check "rejected release kept" 0 previous=v4.4.1 base=v4.4.1 commits=1 version=v4.4.2

new_repo
commit 'fix: a'
run HEAD
check "no release tag" 1

echo "release-version: $passed passed, $failed failed"
[ "$failed" -eq 0 ]
