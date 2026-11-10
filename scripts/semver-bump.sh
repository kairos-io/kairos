#!/usr/bin/env bash
#
# Prints the semver bump one commit asks for: major, minor or patch.
#
#   semver-bump.sh <commit message or pull request title>
#
# The first line must be a Conventional Commit subject,
# `type(scope)!: description`, or a GitHub revert title, `Revert "..."`.
# Anything else is an error: the script exits 1 and says why on stderr.
#
#   major  a `!` after the type or scope, or a `BREAKING CHANGE:` (or
#          `BREAKING-CHANGE:`) footer at the start of a line in the body
#   minor  type `feat`
#   patch  every other type, and reverts
#
# The pull request title check uses it to reject titles that would leave the
# squashed commit on master without a type, and the release workflow uses it
# to choose the next version from the commits since the last release.

set -euo pipefail

TYPES='build|chore|ci|docs|feat|fix|perf|refactor|revert|style|test'

msg="${1-}"
subject="${msg%%$'\n'*}"
body=""
if [[ "$msg" == *$'\n'* ]]; then
  body="${msg#*$'\n'}"
fi

if [[ "$subject" =~ ^Revert\ \".+\"$ ]]; then
  echo patch
  exit 0
fi

re="^($TYPES)(\([^()[:space:]][^()]*\))?(!)?: [^[:space:]]"
if ! [[ "$subject" =~ $re ]]; then
  {
    echo "not a Conventional Commit subject: '$subject'"
    echo "expected 'type(scope): description' or 'type: description', with type one of: ${TYPES//|/, }"
    echo "add '!' before the colon for a breaking change, as in 'feat(agent)!: ...'"
  } >&2
  exit 1
fi

type="${BASH_REMATCH[1]}"
bang="${BASH_REMATCH[3]}"

if [ -n "$bang" ] || grep -qE '^BREAKING[ -]CHANGE: ' <<<"$body"; then
  echo major
elif [ "$type" = feat ]; then
  echo minor
else
  echo patch
fi
