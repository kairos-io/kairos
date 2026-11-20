#!/usr/bin/env bash
#
# Runs scripts/osv-findings.sh against small osv-scanner reports and checks the
# severity and blocking flag it prints for each finding.

set -euo pipefail

SCRIPT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/osv-findings.sh"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

failed=0
passed=0

# check <name> <report> <jq filter over the output> <want>
check() {
  local name="$1" report="$2" filter="$3" want="$4" got
  printf '%s' "$report" > "$TMP/osv.json"
  got="$("$SCRIPT" "$TMP/osv.json" | jq -c "$filter")"
  if [ "$got" = "$want" ]; then
    passed=$((passed + 1))
  else
    echo "FAIL: $name: want $want, got $got" >&2
    failed=$((failed + 1))
  fi
}

# One package with one advisory, scored by the scanner.
scored() {
  cat <<JSON
{"results":[{"packages":[{"package":{"name":"example.com/mod","version":"v1.0.0"},
 "vulnerabilities":[{"id":"GHSA-aaaa"}],
 "groups":[{"ids":["GHSA-aaaa"],"max_severity":"$1"}]}]}]}
JSON
}

check 'clean report' '{"results":[{"packages":[{"package":{"name":"m","version":"v1"}}]}]}' 'length' '0'

check 'score 5.3 is medium and does not block' "$(scored 5.3)" '[.[] | [.severity, .blocking]]' '[["MEDIUM",false]]'
check 'score 2.0 is low and does not block' "$(scored 2.0)" '[.[] | [.severity, .blocking]]' '[["LOW",false]]'
check 'score 7.0 is high and blocks' "$(scored 7.0)" '[.[] | [.severity, .blocking]]' '[["HIGH",true]]'
check 'score 9.8 is critical and blocks' "$(scored 9.8)" '[.[] | [.severity, .blocking]]' '[["CRITICAL",true]]'

check 'module carries the version' "$(scored 5.3)" '.[0].module' '"example.com/mod@v1.0.0"'

check 'database severity MODERATE is medium' \
  '{"results":[{"packages":[{"package":{"name":"m","version":"v1"},
    "vulnerabilities":[{"id":"GHSA-bbbb","database_specific":{"severity":"MODERATE"}}]}]}]}' \
  '[.[] | [.severity, .blocking]]' '[["MEDIUM",false]]'

check 'database severity HIGH blocks' \
  '{"results":[{"packages":[{"package":{"name":"m","version":"v1"},
    "vulnerabilities":[{"id":"GHSA-cccc","database_specific":{"severity":"HIGH"}}]}]}]}' \
  '[.[] | [.severity, .blocking]]' '[["HIGH",true]]'

check 'no severity anywhere is unknown and blocks' \
  '{"results":[{"packages":[{"package":{"name":"m","version":"v1"},
    "vulnerabilities":[{"id":"GO-2026-0001"}]}]}]}' \
  '[.[] | [.severity, .blocking]]' '[["UNKNOWN",true]]'

check 'the scanner score wins over the database severity' \
  '{"results":[{"packages":[{"package":{"name":"m","version":"v1"},
    "vulnerabilities":[{"id":"GHSA-dddd","database_specific":{"severity":"CRITICAL"}}],
    "groups":[{"ids":["GHSA-dddd"],"max_severity":"4.3"}]}]}]}' \
  '[.[].severity]' '["MEDIUM"]'

check 'the same advisory in two versions is two findings' \
  '{"results":[{"packages":[
    {"package":{"name":"m","version":"v1"},"vulnerabilities":[{"id":"GHSA-eeee"}],"groups":[{"ids":["GHSA-eeee"],"max_severity":"5"}]},
    {"package":{"name":"m","version":"v2"},"vulnerabilities":[{"id":"GHSA-eeee"}],"groups":[{"ids":["GHSA-eeee"],"max_severity":"5"}]}]}]}' \
  'length' '2'

check 'findings of another group do not set the score' \
  '{"results":[{"packages":[{"package":{"name":"m","version":"v1"},
    "vulnerabilities":[{"id":"GHSA-ffff"},{"id":"GHSA-gggg"}],
    "groups":[{"ids":["GHSA-ffff"],"max_severity":"9.9"},{"ids":["GHSA-gggg"],"max_severity":"3.1"}]}]}]}' \
  '[.[] | [.id, .severity]] | sort' '[["GHSA-ffff","CRITICAL"],["GHSA-gggg","LOW"]]'

echo "passed: $passed, failed: $failed"
[ "$failed" -eq 0 ]
