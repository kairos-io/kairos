#!/usr/bin/env bash
#
# Picks the next release version from the commits that landed since the last
# release.
#
#   release-version.sh [--bump auto|patch|minor|major] [--notes <file>] <to-ref>
#
# Run it inside a git checkout that has all tags. It prints key=value lines,
# ready to append to $GITHUB_OUTPUT:
#
#   version   the next tag, counted up from `previous`
#   previous  the highest release tag in the repository, on any branch, so a
#             backport tag such as v4.4.1 on a release branch is counted
#   base      the highest release tag that is an ancestor of <to-ref>; the
#             commits to release are the ones after it
#   bump      major, minor or patch
#   commits   how many non-merge commits lie between base and <to-ref>
#
# A release tag is exactly vMAJOR.MINOR.PATCH. Release candidates such as
# v4.5.0-rc1 are never a previous or a base. A tag stays in use after its
# release was rejected, so the next version counts up from it.
#
# The bump is the highest one any commit asks for, as semver-bump.sh reads it.
# A commit whose message semver-bump.sh rejects counts as patch. --bump other
# than auto replaces that choice.
#
# --notes writes one line per commit, newest first:
#
#   - <bump>: <subject> (<short sha>)
#
# with `unchecked` in place of the bump for a rejected message.
#
# Exit status:
#   0  a release can be cut
#   3  no commit landed since base
#   4  auto mode found a breaking change; bump and version are still printed
#      so the caller can say what a maintainer should cut
#   1  any error

set -euo pipefail

BUMP_SCRIPT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/semver-bump.sh"
TAG_RE='^v[0-9]+\.[0-9]+\.[0-9]+$'

usage() {
  echo "usage: release-version.sh [--bump auto|patch|minor|major] [--notes <file>] <to-ref>" >&2
  exit 1
}

bump_mode=auto
notes_file=""
to_ref=""
while [ $# -gt 0 ]; do
  case "$1" in
    --bump)
      [ $# -ge 2 ] || usage
      bump_mode="$2"
      shift 2
      ;;
    --notes)
      [ $# -ge 2 ] || usage
      notes_file="$2"
      shift 2
      ;;
    -*) usage ;;
    *)
      [ -z "$to_ref" ] || usage
      to_ref="$1"
      shift
      ;;
  esac
done
[ -n "$to_ref" ] || usage
case "$bump_mode" in
  auto | patch | minor | major) ;;
  *) usage ;;
esac

rank() {
  case "$1" in
    major) echo 3 ;;
    minor) echo 2 ;;
    *) echo 1 ;;
  esac
}

mapfile -t tags < <(git tag -l | grep -E "$TAG_RE" | sort -V || true)
if [ "${#tags[@]}" -eq 0 ]; then
  echo "no release tag (vMAJOR.MINOR.PATCH) found; fetch the tags first" >&2
  exit 1
fi

previous="${tags[${#tags[@]} - 1]}"
base=""
for ((i = ${#tags[@]} - 1; i >= 0; i--)); do
  if git merge-base --is-ancestor "${tags[$i]}" "$to_ref"; then
    base="${tags[$i]}"
    break
  fi
done
if [ -z "$base" ]; then
  echo "no release tag is an ancestor of $to_ref" >&2
  exit 1
fi

mapfile -t shas < <(git rev-list --no-merges "$base..$to_ref")

[ -z "$notes_file" ] || : >"$notes_file"
auto_bump="patch"
for sha in "${shas[@]}"; do
  msg="$(git log -1 --format=%B "$sha")"
  subject="$(git log -1 --format=%s "$sha")"
  if commit_bump="$("$BUMP_SCRIPT" "$msg" 2>/dev/null)"; then
    label="$commit_bump"
  else
    commit_bump="patch"
    label=unchecked
  fi
  if [ "$(rank "$commit_bump")" -gt "$(rank "$auto_bump")" ]; then
    auto_bump="$commit_bump"
  fi
  if [ -n "$notes_file" ]; then
    echo "- $label: $subject ($(git rev-parse --short=7 "$sha"))" >>"$notes_file"
  fi
done

bump="$auto_bump"
[ "$bump_mode" = auto ] || bump="$bump_mode"

IFS=. read -r major minor patch <<<"${previous#v}"
case "$bump" in
  major) version="v$((major + 1)).0.0" ;;
  minor) version="v$major.$((minor + 1)).0" ;;
  *) version="v$major.$minor.$((patch + 1))" ;;
esac

echo "version=$version"
echo "previous=$previous"
echo "base=$base"
echo "bump=$bump"
echo "commits=${#shas[@]}"

if [ "${#shas[@]}" -eq 0 ]; then
  exit 3
fi
if [ "$bump_mode" = auto ] && [ "$bump" = major ]; then
  exit 4
fi
