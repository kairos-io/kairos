#!/usr/bin/env bash
#
# Says which parts of the repository a change reaches.
#
# usage: scripts/changed-parts.sh <base commit>
#        <list of file names> | scripts/changed-parts.sh
#
# With a base commit it lists the files that differ between it and HEAD, with
# rename detection off so that both the old and the new path of a moved file are
# seen: moving a file out of sdk/ into the docs still reaches the SDK. Without an
# argument it reads the file names on stdin. It prints two lines, for the
# workflow to append to $GITHUB_OUTPUT:
#
#   kairos=true|false      the change can affect the Kairos images and their
#                          tests: any file that is not listed as inert below
#   auroraboot=true|false  the change can affect AuroraBoot: auroraboot/, the
#                          root packages it builds and tests against (sdk/,
#                          agent/pkg/, agent/tests/ and internal/version/, see
#                          `go list -deps -test ./...` in auroraboot/), the root
#                          go.mod and go.sum, and the workflows and actions that
#                          run its tests
#
# Markdown files count for neither. The web UI and the Helm chart of AuroraBoot
# reach AuroraBoot only: no ISO or image of Kairos contains them. An empty list
# answers true for both, so an unknown change runs everything.

set -euo pipefail

inert='(\.md$|^docs/|^LICENSE$|^\.github/(ISSUE_TEMPLATE/|CODEOWNERS$)|^auroraboot/(ui|deploy/helm)/)'
auroraboot='^(auroraboot/|sdk/|agent/pkg/|agent/tests/|internal/version/|go\.(mod|sum)$|\.github/(actions/(prefer-ipv4|find-ovmf-firmware)/|workflows/(_auroraboot-tests|_build-image-auroraboot|_changes)\.yaml$))'

if [ "$#" -gt 1 ]; then
  echo "usage: $0 [<base commit>]" >&2
  exit 2
fi

if [ "$#" -eq 1 ]; then
  files="$(git diff --name-only --no-renames "$1" HEAD)"
else
  files="$(grep -v '^$' || true)"
fi

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
