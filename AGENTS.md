# Working on kairos-io

Kairos builds immutable Linux distributions for edge and Kubernetes.

## Where the code is

`kairos-io/kairos` is a monorepo. The components that used to have a repository
of their own were moved into it as directories, and it is also the issue tracker
for the whole org. Changes typically land in one of three repositories:

| Repository | What it is |
|---|---|
| `kairos` | The OS. `sdk/` shared library, `agent/` install and upgrade and reset, `immucore/` initramfs and mounts, `kairos-init/` image builder, `installer/`, `provider/` for k3s and k0s, `kcrypt/`, `tests/` |
| `AuroraBoot` | Builds ISOs, raw disks and netboot artifacts |
| `hadron` | The minimal base OS, built from source |

Extensions and additions go in `hadron-layers`. Also here: `kairos-operator`,
`cluster-api-provider-kairos`, `kairos-docs`, `packages`, and `mudler/yip`, the
cloud-config engine, maintained by the same people.

`kairos` is one Go module, so a change to `sdk/` and its caller in `agent/` is
one commit in one pull request. `kairos/tests` is a separate module,
`kairos-tests`.

Fix things where they are broken, upstream included. Do not work around a bug
locally because you assume the real fix will be slow to merge.

## Commits and pull requests

- Sign off every commit: `git commit -s`. DCO is a required check.
- Disclose in the pull request body that AI was used, and say whether a human
  read the code before it was opened.
- Conventional Commits for the subject line: `fix(iso): ...`, `docs: ...`.
- Every pull request references an issue. Write
  `Fixes kairos-io/kairos#NNNN` when the pull request completes the issue, or
  `Part of kairos-io/kairos#NNNN` when it is one step of a larger piece of
  work. Use `Part of` when you are not sure, so that merging one slice does not
  close a parent issue that is still open.
- Search the open issues for the area you are touching before you file a new
  one, and search by the area rather than by your own wording for the problem.
  Most work already has an issue. Parent issues often have child issues that
  are a closer match to your change than the parent is.
- If no issue matches, do not stretch one that is merely close, and do not
  invent a reference. Say so, and file an issue that states the problem the
  change solves, so the work is tracked before it is reviewed.
- File the issue in `kairos-io/kairos`. Most of the other repositories have
  their issue tracker turned off on purpose, so that the project has one
  Issues tab.
- Open a pull request for every change. Push the branch to a fork if you do not
  have write access to the repository, otherwise to the repository. Never push
  to the default branch.
- Add a test for any behaviour change. A boot-path change needs a boot test.
- Keep pull requests small and single-purpose.
- Never force-push or rebase someone else's branch.
- Never enable GPG signing, `-S`. It blocks on a passphrase nobody will type.
  That is a different flag from `-s`.

## The project board

The issue tracking board holds issues only. Do not add a pull request to it. A
pull request reaches the board through the issue it references, which is why
the reference is required.

## Writing

Text you write into files, commits and pull request descriptions: no em dashes,
no emojis, plain words rather than jargon and acronyms.

## Things that will trip you up

- Read the default branch before you create one. It is not the same in every
  repository:

  ```
  git remote set-head origin --auto
  git symbolic-ref --short refs/remotes/origin/HEAD
  ```

- Pull requests from forks cannot read repository secrets, so image-build jobs
  fail within seconds at "Login to registry". That is structural, not something
  your change caused.
- Do not fetch sources from upstream during a build. In `hadron`, add the
  tarball to `sources.yaml` with its checksum, and the `populate-sources`
  workflow republishes it under `ghcr.io/kairos-io/hadron-sources`.
- Unit tests cannot prove a boot works. If you changed the boot path, boot it.

## Shared skills

Tested procedures for the hard parts live in
[kairos-io/skills](https://github.com/kairos-io/skills): driving QEMU
headlessly, testing immucore in a real boot, testing the installer on a Hadron
image, performing QA on a ticket, cutting a backport release. Prefer one over
improvising.

## QA

QA is requested by moving an issue into the QA column of
[project 1](https://github.com/orgs/kairos-io/projects/1/views/1). An issue
sitting there is a live request, including one that was tested before and sent
back.

A verdict is a comment on the issue that starts `### QA Result:` and says PASS,
FAIL or BLOCKED, with the evidence behind it. Read those before you work on the
issue. A `QA: fail` verdict names a real reproduction that somebody still has to
fix, and it does not reach you any other way.

A passing issue is labelled `QA: pass` and moves to `QA OK`. A failing one is
labelled `QA: fail` and stays in QA.

The `performing-kairos-qa` skill has the procedure for producing a verdict.

## Go

- Build and test through the repository's `Makefile` target where there is one.
  `go build ./...` on its own can fail on a fresh clone in repositories whose
  packages embed generated files.
- Do not bump the Go version or dependencies as a side effect of an unrelated
  change. Renovate handles routine bumps.
- Check whether `kairos/sdk` already wraps a dependency before adding it.
- Write tests in the ginkgo/gomega style the rest of the tree uses.

## Dockerfiles

- One logical step per `RUN`. Do not collapse unrelated commands to save a
  layer.
- Pin versions with a build argument rather than inlining them, so Renovate can
  see them.
- Do not add a `wget` or `curl` of an upstream tarball. Use `sources.yaml` in
  `hadron`, as above.
- `hadolint` runs in CI. Run it locally before pushing.

## Patching third-party source

- A build failure in a third-party package is usually a bug in that package.
  Fix it upstream first. Carry a patch here only to unblock the build while
  upstream reviews the fix.
- Put the patch in `patches/` in `hadron`, as a file. Do not edit third-party
  source with a `sed` in a Dockerfile. A `sed` has no header to read, no name
  that says what it repairs, and nothing that tells the next person when to
  remove it.
- Give every patch a header that says what breaks, why it breaks on this target
  and not on the others, where the fix went upstream, and the condition for
  dropping the patch. `patches/0001-audit-syslog-plugin-include-unistd.patch`
  is the model to copy.
- Where you cannot send the fix upstream yourself, say so in the patch header
  and in the pull request, and write the upstream report so that a maintainer
  only has to send it. A patch that nobody reported is carried for ever by
  whoever inherits it.
- Disclose AI use in the upstream report too, the same way you disclose it
  here, and say how the patch was verified: which target it was built on, which
  test failed before and passes after. Many projects now reject an undisclosed
  AI patch on sight, and an unverified one wastes a maintainer's time. State
  plainly when the fix was not built or run on the affected target.

## This repository, specifically

- `go build ./...` and `go test ./...` fail on a fresh clone with
  `pattern binaries/edgevpn: no matching files found`. `kairos-init/pkg/bundled`
  embeds binaries that are built rather than committed. Run `make test` and
  `make lint-go`, which depend on `kairos-init-embed-stubs`. Nothing in that
  package needs fixing.
- Lint with the version in `.golangci-version`. Whatever is on your PATH
  reports a different set of findings, in both directions.
- `tests/` is a separate module, so a green root `go test ./...` says nothing
  about whether the system still boots.
- Ignore `CONTRIBUTING.md`'s emoji commit prefixes and its ten emoji pull
  request labels. `master` uses Conventional Commits, and recent merges carry
  `QA: pass` and nothing else. `CONTRIBUTING.md` is right about `-s`.
- Where there is no C compiler, `CGO_ENABLED=0 go build ./...` builds the whole
  tree. Do not commit a build tag or a dependency swap to work around a missing
  local compiler.
