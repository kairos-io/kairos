#!/usr/bin/env bash
#
# Says which parts of the repository a change reaches.
#
# usage: git diff --name-only <base> HEAD | scripts/changed-parts.sh
#
# Reads the changed file names on stdin and prints two lines, for the workflow
# to append to $GITHUB_OUTPUT:
#
#   kairos=true|false      the change can affect the Kairos images and their
#                          tests: any file that is not listed as inert below
#   auroraboot=true|false  the change can affect AuroraBoot: auroraboot/, the
#                          root packages it builds against (sdk/, agent/pkg/ and
#                          internal/version/), the root go.mod and go.sum, and the
#                          workflows and actions that run its tests
#
# Markdown files count for neither. The web UI and the Helm chart of AuroraBoot
# reach AuroraBoot only: no ISO or image of Kairos contains them. An empty list
# answers true for both, so an unknown change runs everything.

set -euo pipefail

inert='(\.md$|^docs/|^LICENSE$|^\.github/(ISSUE_TEMPLATE/|CODEOWNERS$)|^auroraboot/(ui|deploy/helm)/)'
auroraboot='^(auroraboot/|sdk/|agent/pkg/|internal/version/|go\.(mod|sum)$|\.github/(actions/(prefer-ipv4|find-ovmf-firmware)/|workflows/(_auroraboot-tests|_build-image-auroraboot|_changes)\.yaml$))'

files="$(grep -v '^$' || true)"

if [ -z "$files" ]; then
  echo "kairos=true"
  echo "auroraboot=true"
  exit 0
fi

# grep -c exits 1 on no match; the count is what matters here.
kairos_files="$(grep -Evc "$inert" <<<"$files" || true)"
auroraboot_files="$(grep -Ev '\.md$' <<<"$files" | grep -Ec "$auroraboot" || true)"

echo "kairos=$([ "${kairos_files:-0}" -gt 0 ] && echo true || echo false)"
echo "auroraboot=$([ "${auroraboot_files:-0}" -gt 0 ] && echo true || echo false)"
