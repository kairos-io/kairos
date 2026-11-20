#!/usr/bin/env bash
#
# Lists the advisories in an osv-scanner JSON report with a severity for each.
#
#   osv-findings.sh <osv.json>
#
# Prints a JSON array with one object per (advisory, module@version) pair:
#
#   {"id": "GHSA-...", "module": "go.opentelemetry.io/otel@v1.46.0",
#    "severity": "MEDIUM", "blocking": false}
#
# The severity is the scanner's CVSS score for the advisory group, mapped as
# CRITICAL (9.0 and above), HIGH (7.0), MEDIUM (4.0) and LOW. When the scanner
# gives no score, the severity the advisory database states is used, with
# MODERATE read as MEDIUM. When neither exists the severity is UNKNOWN.
#
# `blocking` is true for CRITICAL, HIGH and UNKNOWN. An advisory whose severity
# cannot be read blocks, so a gap in the data never lets a serious one through.
# release-scan.yml uses it to decide what stops a release.

set -euo pipefail

if [ "$#" -ne 1 ]; then
  echo "usage: $0 <osv.json>" >&2
  exit 2
fi

jq '
  def band($score):
    if $score >= 9 then "CRITICAL"
    elif $score >= 7 then "HIGH"
    elif $score >= 4 then "MEDIUM"
    else "LOW" end;

  [ .results[].packages[]
    | . as $pkg
    | .vulnerabilities[]?
    | . as $vuln
    | (([$pkg.groups[]? | select(.ids | index($vuln.id)) | .max_severity | tonumber? ] | max)
        // null) as $score
    | { id: $vuln.id,
        module: "\($pkg.package.name)@\($pkg.package.version)",
        severity:
          (if $score != null then band($score)
           else ((($vuln.database_specific.severity // "") | ascii_upcase) as $s
                 | if $s == "MODERATE" then "MEDIUM"
                   elif ($s | IN("LOW", "MEDIUM", "HIGH", "CRITICAL")) then $s
                   else "UNKNOWN" end)
           end) }
    | .blocking = (.severity | IN("CRITICAL", "HIGH", "UNKNOWN"))
  ] | unique' "$1"
