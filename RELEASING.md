# Releasing Kairos

Kairos is released every Monday at 06:00 UTC by the `weekly-release` workflow
(`.github/workflows/weekly-release.yaml`). The workflow builds the release.
A maintainer approves it before it is published.

## What happens on Monday

1. The workflow picks the newest commit on `master` whose CI has finished
   (`scripts/green-commit.sh`). If a newer commit is still running CI, it is
   skipped in favour of an older commit with a result.
2. It reads the commit messages since the last final release and chooses the
   next version (`scripts/release-version.sh`). Messages follow Conventional
   Commits: a breaking change asks for a major bump, `feat` for a minor bump,
   and anything else for a patch bump. The next version counts up from the
   highest `vX.Y.Z` tag in the repository, including tags on release branches.
3. It creates the tag and a GitHub prerelease on that commit, then starts
   `release.yaml` on the tag. `release.yaml` builds and pushes every image to
   quay.io and attaches the artifacts to the prerelease.
4. It posts to Slack that the release is building.

The prerelease is not shown as Latest and `:latest` on quay.io still points at
the previous release until a maintainer approves it. While a prerelease waits
for approval, the Monday run does not cut a new release unless new commits
landed on `master` since its tag.

The workflow ends successfully without a release, and posts to Slack, when
`master` is red, when no commit has a finished CI result, or when a breaking
change is found. The job summary of the run says which one happened. For a
breaking change the message names the version to cut, and a maintainer starts
the release by hand (see "Cut a minor or major release"). When nothing landed
since the last release, only the job summary says so.

## Approve a release

1. Open the `release` run of the new tag under Actions.
2. When the build jobs are done, the `promote` job waits for approval. Click
   "Review deployments", tick `release`, and approve. The reviewers are the
   maintainers team.

`promote` then marks the release as a full release. If the tag is the highest
`vX.Y.Z` tag in the repository, it also points `:latest` of `kairos-init` and
`kcrypt-challenger` at the tag and marks the release as Latest. An older tag,
such as a backport `v4.3.2` cut after `v4.4.0`, becomes a full release and
leaves `:latest` and Latest alone. Last, `promote` starts
`release-notes-diff.yaml`, so the release notes diff appears after approval.

## Reject a release

Reject the `promote` deployment and leave the tag and the prerelease in place.
The run is then marked failed, which is expected. The same happens when nobody
approves it.

The tag keeps its version number, because its images already exist on quay.io
under that version. The next release counts up from it, and its release notes
include the commits of the rejected release, because only a final release
starts the commit range. The next Monday run cuts that release when new commits
have landed since the rejected tag. To re-cut now with another bump:

```
gh workflow run weekly-release.yaml -f bump=<patch|minor|major>
```

An explicit `bump` cuts a release even when nothing landed since the rejected
tag.

## Cut a minor or major release

The weekly run chooses the bump from commit messages and stops at a breaking
change. To choose the bump yourself:

```
gh workflow run weekly-release.yaml -f bump=minor
gh workflow run weekly-release.yaml -f bump=major
```

The release is built from the newest green `master` commit and approved the
same way as the weekly one.

## Check what a run would do

```
gh workflow run weekly-release.yaml -f dry_run=true
```

A dry run writes the chosen commit, the previous version, the next version and
the commit list to the job summary. It creates nothing and posts nothing to
Slack: a message it would have posted goes to the job summary instead. It
accepts `bump` too.

## If the build did not start

If `weekly-release` created the prerelease but failed to start the build,
running it again finds nothing new. Start the build by hand:

```
gh workflow run release.yaml --ref vX.Y.Z
```

## Release by hand

Push only the tag:

```
git tag vX.Y.Z <commit>
git push origin vX.Y.Z
```

`release.yaml` creates the prerelease itself. Do not create the release in the
GitHub UI as a full release: GitHub marks it Latest immediately, before it is
built.

`release.yaml` accepts only `vX.Y.Z` and `vX.Y.Z-rcN` tags and fails on
anything else. A release candidate such as `v4.5.0-rc1` builds as a prerelease
and is never promoted. Release candidates are not counted when the next
version is chosen.

## Setup an administrator must keep in place

- The `release` environment (Settings, Environments). Required reviewers: the
  maintainers team. Deployment branches and tags rule: tags matching `v*`. It
  exists.
- The repository secret `SLACK_RELEASE_WEBHOOK`, a Slack incoming webhook URL.
  It does not exist yet. Without it the Slack messages appear only as warnings
  in the workflow run, and the release still works.
