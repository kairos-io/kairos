#!/usr/bin/env bash
#
# Runs populate_dep_versions from scripts/kairos-diff.sh against refs of a
# throwaway git repository laid out like the monorepo. Needs only git.

set -euo pipefail

SCRIPT="${KAIROS_DIFF_SCRIPT:-$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/kairos-diff.sh}"

# shellcheck source=/dev/null
source "$SCRIPT"

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

# Every ref below lives in the local repository. Never reach GitHub.
gh() { return 1; }

KAIROS_REPO_ROOT="$WORK/repo"
git init -q "$KAIROS_REPO_ROOT"
git -C "$KAIROS_REPO_ROOT" config user.email test@example.com
git -C "$KAIROS_REPO_ROOT" config user.name test

failed=0
passed=0

# commit_ref <tag> <go.mod edgevpn version> <EDGEVPN_VERSION file or ""> <Makefile line or "">
commit_ref() {
  local tag="$1" gomod_version="$2" file_version="$3" makefile_line="$4"
  local root="$KAIROS_REPO_ROOT"
  git -C "$root" rm -rq --ignore-unmatch . >/dev/null
  mkdir -p "$root/kairos-init"
  printf 'package main\n' >"$root/kairos-init/main.go"
  printf 'module github.com/kairos-io/kairos\n\nrequire (\n\tgithub.com/mudler/edgevpn %s\n)\n' "$gomod_version" >"$root/go.mod"
  [[ -n "$file_version" ]] && printf '%s\n' "$file_version" >"$root/kairos-init/EDGEVPN_VERSION"
  [[ -n "$makefile_line" ]] && printf '%s\n' "$makefile_line" >"$root/kairos-init/Makefile"
  git -C "$root" add -A
  git -C "$root" commit -qm "$tag"
  git -C "$root" tag "$tag"
}

# expect_edgevpn <name> <tag> <expected version>
expect_edgevpn() {
  local name="$1" tag="$2" want="$3" got
  declare -A deps=()
  # Called the way the script calls it, under ||, which also turns errexit off
  # inside it.
  if ! populate_dep_versions "$tag" deps; then
    echo "FAIL: $name: populate_dep_versions failed"
    failed=$((failed + 1))
    return 0
  fi
  got="${deps[edgevpn]:-}"
  if [[ "$got" == "$want" ]]; then
    echo "PASS: $name"
    passed=$((passed + 1))
  else
    echo "FAIL: $name: edgevpn is '$got', want '$want'"
    failed=$((failed + 1))
  fi
}

commit_ref no-makefile v0.35.3 v0.35.5 ""
expect_edgevpn "no Makefile: the EDGEVPN_VERSION file beats the go.mod library pin" no-makefile v0.35.5

# shellcheck disable=SC2016 # the Makefile line is literal
commit_ref shell-makefile v0.35.3 v0.35.5 'EDGEVPN_VERSION := $(shell cat EDGEVPN_VERSION)'
expect_edgevpn "\$(shell cat) Makefile: the EDGEVPN_VERSION file wins" shell-makefile v0.35.5

commit_ref no-file v0.35.3 "" ""
expect_edgevpn "no EDGEVPN_VERSION file: go.mod is all there is" no-file v0.35.3

echo "$passed passed, $failed failed"
[[ "$failed" -eq 0 ]]
