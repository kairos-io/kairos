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
check "sdk" "true true" sdk/utils/x.go
check "a workflow" "true false" .github/workflows/pr.yaml
check "a workflow of the AuroraBoot tests" "true true" .github/workflows/_auroraboot-tests.yaml
check "web UI and agent together" "true true" auroraboot/ui/src/App.tsx agent/pkg/x.go
check "web UI and a root workflow" "true true" auroraboot/ui/src/App.tsx .github/workflows/pr.yaml
check "web UI and root Markdown" "false true" auroraboot/ui/src/App.tsx README.md
check "no files at all runs everything" "true true"

echo "$passed passed, $failed failed"
[ "$failed" = 0 ]
