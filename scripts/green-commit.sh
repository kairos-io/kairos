#!/usr/bin/env bash
#
# Picks the newest master commit whose CI has a finished verdict.
#
# Usage: green-commit.sh <commits.json> <runs.json>
#
# commits.json is the GitHub "list commits" array, newest first. runs.json is
# `gh run list --json workflowName,headSha,status,conclusion,url` output for
# push runs on master.
#
# A commit is red as soon as any completed run on it, of any workflow
# including `master`, has a red conclusion, even while other runs on it are
# still going. A commit is green when its `master` run has completed without
# being cancelled, every run on it has completed, and none is red. Any other
# commit has no verdict. The newest commit with a verdict decides. If it is
# red, older commits are walked while they are red or have no verdict, so
# `since` names the oldest commit of the streak.
#
# Exit 1 on bad arguments, a missing file or invalid JSON.
#
# Prints key=value lines:
#   green: state=green sha=<sha>                          exit 0
#   red:   state=red sha= since= workflow= url=           exit 2
#   none:  state=none                                     exit 3

set -euo pipefail

if [ "$#" -ne 2 ]; then
  echo "usage: ${0##*/} <commits.json> <runs.json>" >&2
  exit 1
fi

if ! result="$(jq -n -r --slurpfile commits "$1" --slurpfile runs "$2" '
  ["failure", "timed_out", "startup_failure", "action_required"] as $red
  | ($runs[0]) as $all
  | [ $commits[0][]
      | .sha as $sha
      | [ $all[] | select(.headSha == $sha) ] as $r
      | [ $r[] | select(.status == "completed" and (.conclusion as $c | $red | index($c))) ] as $reds
      | {
          sha: $sha,
          verdict: (
            ($reds | length > 0)
            or (
              ([ $r[] | select(.workflowName == "master" and .status == "completed" and .conclusion != "cancelled") ] | length > 0)
              and ([ $r[] | select(.status != "completed") ] | length == 0)
            )
          ),
          reds: $reds
        }
    ] as $list
  | ([ $list[] | select(.verdict) ] | first) as $first
  | if $first == null then
      "state=none"
    elif ($first.reds | length) == 0 then
      "state=green\nsha=\($first.sha)"
    else
      ([ $list | to_entries[] | select(.value.verdict and (.value.reds | length) == 0) | .key ] | first // ($list | length)) as $stop
      | [ $list[:$stop][] | select(.verdict and (.reds | length > 0)) ] as $streak
      | $first.reds[0] as $run
      | "state=red\nsha=\($first.sha)\nsince=\($streak[-1].sha)\nworkflow=\($run.workflowName)\nurl=\($run.url)"
    end
')"; then
  echo "${0##*/}: cannot read or parse the input files" >&2
  exit 1
fi

printf '%s\n' "$result"

case "$result" in
  state=green*) exit 0 ;;
  state=red*) exit 2 ;;
  *) exit 3 ;;
esac
