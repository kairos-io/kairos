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
# A commit has a verdict when its `master` workflow run has completed without
# being cancelled and no run of any workflow on it is still unfinished. The
# newest commit with a verdict decides. If it is red, older commits are walked
# while they are red or have no verdict, so `since` names the oldest commit of
# the streak.
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

result="$(jq -n -r --slurpfile commits "$1" --slurpfile runs "$2" '
  ["failure", "timed_out", "startup_failure", "action_required"] as $red
  | ($runs[0]) as $all
  | [ $commits[0][]
      | .sha as $sha
      | [ $all[] | select(.headSha == $sha) ] as $r
      | {
          sha: $sha,
          verdict: (
            ([ $r[] | select(.workflowName == "master" and .status == "completed" and .conclusion != "cancelled") ] | length > 0)
            and ([ $r[] | select(.status != "completed") ] | length == 0)
          ),
          reds: [ $r[] | select(.status == "completed" and (.conclusion as $c | $red | index($c))) ]
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
')"

printf '%s\n' "$result"

case "$result" in
  state=green*) exit 0 ;;
  state=red*) exit 2 ;;
  *) exit 3 ;;
esac
