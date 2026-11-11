# Releasing Kairos

The `weekly-release` workflow (`.github/workflows/weekly-release.yaml`) builds
a release every Monday at 06:00 UTC. A maintainer approves it before it is
published.

## What happens on Monday

1. `scripts/green-commit.sh` looks at the newest finished `master.yaml` push
   run on `master`. Cancelled, skipped and in-progress runs are ignored. When
   that run succeeded, its commit is released. Any other result skips the
   week. The workflow never falls back to an older green commit.
2. `scripts/release-version.sh` reads the commit messages since the newest
   `vX.Y.Z` tag the commit contains, whether that release is final, pending
   approval or rejected. A breaking change asks for a major bump, `feat` for a
   minor bump and anything else for a patch bump. The version counts up from
   the highest `vX.Y.Z` tag in the repository, release branches included.
3. The workflow checks that neither `quay.io/kairos/kairos-init:<version>` nor
   `quay.io/kairos/kcrypt-challenger:<version>` exists. If one does, the
   version was built before and its tag was deleted, so the run fails. It also
   fails when quay.io cannot answer. A maintainer reruns it later, recreates
   the tag or releases by hand.
4. It creates the tag and a GitHub prerelease on the commit, starts
   `release.yaml` on the tag and posts to Slack that the release is building.
   `release.yaml` builds every image, pushes it to quay.io and attaches the
   artifacts to the prerelease.

The run ends successfully without a release when `master.yaml` did not
succeed, when no run has finished, when no commit landed since the newest
release tag, and when a commit is a breaking change. The job summary says
which one happened. For a breaking change the Slack message names the version
to cut, and a maintainer starts it with `bump=major` (see "Cut a minor or
major release"). A prerelease that waits for approval counts as a release
tag, so the Monday run does not cut it again.

`:latest` on quay.io and the Latest mark on GitHub stay on the previous
release until a maintainer approves the new one.

## Approve a release

1. Open the `release` run of the new tag under Actions.
2. When the build jobs are done, the `promote` job waits for approval. Click
   "Review deployments", tick `release`, and approve. The reviewers are the
   maintainers team.

The `publish` job then marks the release as a full release. When the tag is
the highest among the final releases plus itself, it also points `:latest` of
`kairos-init` and `kcrypt-challenger` at the tag and marks the release as
Latest. An older tag, such as a backport `v4.3.2` cut after `v4.4.0`, becomes a
full release and leaves `:latest` and Latest alone, provided its release branch
carries this `release.yaml` (see "Release by hand"). Publishing runs one
release at a time. Last, `publish` starts `release-notes-diff.yaml`.

When several approvals land within seconds, GitHub may cancel a waiting
`publish` job. Re-run that job from the release run.

`upload-cloud-images.yaml` uploads the Latest release to the cloud providers.
It runs every night and on demand, so the images appear up to a day after
approval.

## Reject a release

Reject the `promote` deployment and leave the tag and the prerelease in place.
The run is marked failed, which is expected. The same happens when nobody
approves it.

The tag keeps its version, because its images already exist on quay.io. The
next release counts up from it, and its notes list only the commits after the
rejected tag. The next Monday run cuts a release when commits have landed
since. To re-cut with another bump:

```
gh workflow run weekly-release.yaml -f bump=<patch|minor|major>
```

An explicit `bump` cuts a release even when nothing landed since the tag. It
cuts at the newest green commit, which is the rejected one when nothing landed
since.

## Release after a skipped Monday

Fix `master` and wait for the `master.yaml` run on the fixing commit to finish
green. A run that is still going is ignored, so starting early decides on the
previous finished run, which is the red one. Then start the release by hand:

```
gh workflow run weekly-release.yaml
```

It picks the commit and the version the same way the Monday run does, and the
release is approved as usual.

## Cut a minor or major release

The weekly run chooses the bump from commit messages and stops at a breaking
change. To choose the bump yourself:

```
gh workflow run weekly-release.yaml -f bump=minor
gh workflow run weekly-release.yaml -f bump=major
```

The release is built from the newest finished `master.yaml` run when it
succeeded, and approved like the weekly one.

## Check what a run would do

```
gh workflow run weekly-release.yaml -f dry_run=true
```

A dry run writes the chosen commit, the previous version, the next version and
the commit list to the job summary. It creates nothing and posts nothing to
Slack, and writes the message it would have posted to the job summary. It
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

`release.yaml` creates the prerelease itself, with release notes of only
"Release <tag>". Do not create the release in the GitHub UI as a full release,
because GitHub marks it Latest before it is built. A tag pushed by hand also
skips the quay.io reuse check that `weekly-release` runs, so make sure the
version was never built before.

A tag runs the `release.yaml` stored at its commit. A release branch cut before
the approval step existed still has a `release.yaml` that pushes `:latest` at
build time with no approval. Cherry-pick the current `release.yaml` onto the
release branch before tagging a backport on it.

The `guard` job of `release.yaml` accepts only `vX.Y.Z` and `vX.Y.Z-rcN` tags.
A release candidate such as `v4.5.0-rc1` builds as a prerelease, skips
`promote` and is never published. Release candidates are not counted when the
next version is chosen.

## Setup an administrator must keep in place

- The `release` environment (Settings, Environments). Required reviewers: the
  maintainers team. Deployment branches and tags rule: tags matching `v*`.
  `promote` waits on it. Without the environment or its reviewers, `promote`
  and `publish` run as soon as the build is done and nobody approves the
  release.
- The repository secret `SLACK_RELEASE_WEBHOOK`. It holds the URL of a
  Workflow Builder webhook in the CNCF Slack. The Slack workflow belongs to
  Dimitris Karakasilis, and its collaborators are Ettore Di Giacinto, Itxaka
  Serrano Garcia, Mauro Morales and William Rizzo. Any of them can edit it in
  Workflow Builder. Its trigger has one text variable, `text`, and its only
  step sends `{{text}}` to the channel. A new workflow needs no app
  installation, only its URL in the secret
  (`gh secret set SLACK_RELEASE_WEBHOOK`). Without the secret the messages
  appear only as warnings in the workflow run, and the release still works.
