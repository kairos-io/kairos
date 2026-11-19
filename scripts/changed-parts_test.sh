#!/usr/bin/env bash
#
# Runs scripts/changed-parts.sh on file lists and checks the two answers it
# prints: which parts of the repository the change reaches.

set -euo pipefail

SCRIPT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/changed-parts.sh"

failed=0
passed=0

# check <name> <expected "kairos auroraboot"> <file>...
check() {
  local name="$1" want="$2"
  shift 2
  local out got
  out="$(printf '%s\n' "$@" | "$SCRIPT")"
  got="$(sed -n 's/^kairos=//p' <<<"$out") $(sed -n 's/^auroraboot=//p' <<<"$out")"
  if [ "$got" = "$want" ]; then
    echo "ok   $name"
    passed=$((passed + 1))
  else
    echo "FAIL $name: want '$want', got '$got'"
    failed=$((failed + 1))
  fi
}

check "web UI only" "false true" auroraboot/ui/src/pages/ArtifactBuilder.tsx
check "Helm chart only" "false true" auroraboot/deploy/helm/auroraboot/values.yaml
check "root Markdown only" "false false" README.md
check "AuroraBoot Markdown only" "false false" auroraboot/README.md auroraboot/docs/rfc/x.md
check "root docs, license and templates" "false false" docs/guide.txt LICENSE .github/ISSUE_TEMPLATE/bug.yml .github/CODEOWNERS
check "AuroraBoot Go code" "true true" auroraboot/internal/builder/auroraboot/builder.go
check "generated API docs, which are Go code" "true true" auroraboot/docs/docs.go
check "root module go.mod" "true true" go.mod
check "agent command" "true false" agent/main.go
check "agent package AuroraBoot imports" "true true" agent/pkg/uki/uki.go
check "agent test mocks the AuroraBoot tests import" "true true" agent/tests/mocks/runner_mock.go
check "sdk" "true true" sdk/utils/x.go
check "a workflow" "true false" .github/workflows/pr.yaml
check "a workflow of the AuroraBoot tests" "true true" .github/workflows/_auroraboot-tests.yaml
check "web UI and agent together" "true true" auroraboot/ui/src/App.tsx agent/pkg/x.go
check "web UI and a root workflow" "true true" auroraboot/ui/src/App.tsx .github/workflows/pr.yaml
check "web UI and root Markdown" "false true" auroraboot/ui/src/App.tsx README.md
check "no files at all runs everything" "true true"

# With a base commit the script lists the changed files itself, and it must see
# both sides of a rename: moving a file out of auroraboot/ui/ into the docs
# reaches AuroraBoot even though the destination is inert. The expected answer
# differs from the one for an empty list (true, true) and from the one for the
# destination alone (false, false), so the case fails if either happens.
#
# The repository ignores the user's and the system's git configuration, so a
# commit.gpgsign=true there cannot start a passphrase prompt.
repo="$(mktemp -d)"
trap 'rm -rf "$repo"' EXIT
g() { GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1 git -C "$repo" "$@"; }
g init -q
g config user.email test@example.com
g config user.name test
mkdir -p "$repo/auroraboot/ui" "$repo/docs"
printf 'export {}\n' >"$repo/auroraboot/ui/a.tsx"
g add -A
g commit -q -m base
base="$(g rev-parse HEAD)"
g mv auroraboot/ui/a.tsx docs/a.tsx
g commit -q -m rename
out="$(
  cd "$repo" || exit 1
  GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1 timeout 60 "$SCRIPT" "$base" </dev/null || true
)"
got="$(sed -n 's/^kairos=//p' <<<"$out") $(sed -n 's/^auroraboot=//p' <<<"$out")"
if [ "$got" = "false true" ]; then
  echo "ok   a file moved out of the web UI into the docs"
  passed=$((passed + 1))
else
  echo "FAIL a file moved out of the web UI into the docs: want 'false true', got '$got'"
  failed=$((failed + 1))
fi

echo "$passed passed, $failed failed"
[ "$failed" = 0 ]
