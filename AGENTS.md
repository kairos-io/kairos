<!-- BEGIN GENERATED FROM kairos-io/community/agent-conventions. DO NOT EDIT THIS BLOCK BY HAND. -->
<!-- Edit the sources there, not this file. -->

# Working on kairos-io

Kairos builds immutable Linux distributions for edge and Kubernetes. Changes
usually cross repository boundaries, so orient yourself before editing.

## Repository map

Most of what used to be one repository per component now lives inside `kairos`.
An assistant that learned this project before the move will look for
repositories that no longer accept changes, so find the directory first.

| Where | What it is |
|---|---|
| `kairos` | The main repository, and **the issue tracker for the whole org** |
| `kairos/sdk` | Shared Go library. Changes here reach every other directory |
| `kairos/agent` | The in-system agent: install, upgrade, reset |
| `kairos/immucore` | Initramfs and mount logic. Boot-critical |
| `kairos/kairos-init` | Turns a base image into a Kairos image |
| `kairos/installer` | The interactive installer |
| `kairos/provider` | The Kubernetes provider, for k3s and k0s |
| `kairos/kcrypt` | Network-based unlock for encrypted partitions |
| `kairos/tests` | The end-to-end and boot suites. A separate Go module |
| `AuroraBoot` | Builds ISOs, raw disks and netboot artifacts |
| `hadron`, `hadron-layers` | Minimal base OS built from source, and its layers |
| `kairos-operator` | The Kubernetes operator |
| `cluster-api-provider-kairos` | The Cluster API provider |
| `kairos-docs` | The website and the documentation |
| `packages` | Package specifications |
| `mudler/yip` | The cloud-config engine Kairos runs on. Outside the org, same people |

The repositories those components came from are archived and read-only:
`kairos-sdk`, `kairos-agent`, `immucore`, `kairos-init`, `provider-kairos`,
`kcrypt-discovery-challenger` and `osbuilder`. A pull request against one of
them cannot merge. If a search result or a stale memory sends you to one of
them, the change belongs in the matching directory of `kairos` instead.

**`kairos` is a single Go module.** Everything above under `kairos/` builds
from one `go.mod` at the repository root, so a change to `sdk/` and its caller
in `agent/` is one commit in one pull request. There is no release step between
them and nothing to pin. `kairos/tests` is a separate module on purpose, to
keep the test suite's dependencies out of the shipped binary.

**Between repositories, the bump is still a step.** `AuroraBoot` depends on
released versions of `kairos` and `kairos-operator`, and a change to either is
not finished until the consumer is raised to a released tag. Where the
dependency publishes tags, pin a tag, not a pseudo-version.

**Fix things where they are broken.** Every repository above is maintained by
the same people, `mudler/yip` included. If the correct fix belongs upstream,
send it upstream. Do not work around it locally because you assume getting
it merged will be slow. It will not be.

**Issues live in `kairos-io/kairos`.** Most other repos have their issue
tracker disabled. File and search there, not in the repo you are editing.

## Is a human driving?

Some rules below depend on the answer. If you do not know, assume the first and
ask.

**A human is driving.** They review as you go. Do not push anything, to any
remote: not a fork, not upstream. Pushing is theirs to do once they have read
the commits. They add the sign-off at that point.

**Nobody is watching.** Push to a fork and open a pull request from it, never
to a branch on the upstream repository. Say plainly in the pull request body
that no human read the code before it was opened, so the reviewer knows what
they are looking at.

## Commits and pull requests

- **`Signed-off-by:` is a human's certification, not a formality.** DCO is
  enforced by a required check, so nothing merges without it. The person whose
  name is on it is stating they reviewed the result. An agent must not add it
  on someone's behalf; whoever reviews adds it when they have read the diff.
  Note that `-s` (the DCO trailer) is a different thing from `-S` (GPG
  signing). Never enable GPG signing yourself: it blocks waiting for a
  passphrase nobody is there to type.
- **Disclose AI involvement** with a `Co-developed-by:` trailer naming the
  model. The two trailers say different things. This one says AI was used; the
  sign-off says a human drove it and vouches for the result.
- **Conventional Commits** for the subject line: `fix(iso): ...`, `docs: ...`.
- **Set a new branch to track a remote branch of the same name.** A local
  branch left tracking `main` turns a later bare `git push` into a push
  straight to `main`.
- **Add a test** for any behaviour change. Boot-path changes need a boot test,
  not only a unit test. See the skills below. New tests follow the
  ginkgo/gomega style used across the kairos repositories.
- **Keep pull requests small and single-purpose.** Reviewer attention is the
  scarce resource here.
- **Never force-push or rebase someone else's branch**, and never stack a pull
  request on another unmerged branch.

## Writing

Text you write into files, commits and pull request descriptions should not
advertise itself as machine-written. No em dashes, no emojis. Prefer plain
words to acronyms and jargon, and do not assume the reader already knows the
system by heart.

## Things that will trip you up

- **The default branch is not the same everywhere.** `kairos` and `go-nodepair`
  use `master`, and most of the rest use `main`. Do not assume either: read
  `git symbolic-ref refs/remotes/origin/HEAD` before you branch.
- **Pull requests from forks cannot read repository secrets.** Image-build jobs
  fail within seconds at "Login to registry". That failure is structural, not
  something your change caused. Do not try to fix it, and do not tell a
  contributor their patch broke the build.
- **Do not fetch sources from upstream during a build.** Upstream mirrors go
  down and rate-limit, and a red mirror is usually throttling rather than a
  dead URL, so swapping in another single hardcoded URL fixes nothing. Builds
  pull from our own cache instead. In `hadron`, add the tarball to
  `sources.yaml` and the `populate-sources` workflow republishes it under
  `ghcr.io/kairos-io/hadron-sources`.
- **Unit tests cannot prove a boot works.** If you changed anything in the boot
  path, boot it.

## Shared skills

Reusable, tested procedures for the hard parts live in the `kairos-io/skills`
repository: driving QEMU headlessly, testing immucore in a real boot, testing
the installer on a Hadron image, cutting a backport release. Prefer an existing
skill over improvising. The repository is private, which is why it is named
here and not linked.

## AI-assisted contributions

Whoever signs off on a pull request is responsible for every line in it,
however it was produced. Read generated output before vouching for it: the
common failure is code that is locally correct but misreads why the system is
the way it is.

Be explicit rather than leaving a reader to wonder. `Co-developed-by:` records
that AI was involved; `Signed-off-by:` records that a human drove the work and
reviewed the result. A change that has had no human review yet should say so in
the pull request and should not carry anyone's sign-off until it has.

## Go

- Build and test the whole tree before opening a pull request. Use the
  repository's `Makefile` target where there is one: `go build ./...` on its
  own can fail on a fresh clone in repositories whose packages embed generated
  files. If a module proxy is unreachable in your environment, say so in the
  pull request rather than claiming the tests passed.
- Do not bump the Go version or dependencies as a side effect of an unrelated
  change. Renovate handles routine bumps and a manual bump buried in a feature
  branch is hard to review and hard to revert.
- When adding a dependency, check whether the SDK, `kairos/sdk`, already
  wraps it.
- When creating new tests, follow the ginkgo/gomega style as the rest of the
  tests in kairos projects do.

## Dockerfiles

- Keep one logical step per `RUN`. Do not collapse unrelated commands into a
  single long line to save a layer. It makes the diff unreadable and the
  failure unattributable.
- Pin versions with a build argument rather than inlining them, so Renovate can
  see and update them.
- **Do not add a `wget` or `curl` of an upstream tarball to a build.** Source
  fetching is deliberately off the critical path: sources are mirrored into our
  own registry and pulled from there. In `hadron`, add the tarball to
  `sources.yaml` with its checksum and candidate URLs, and the
  `populate-sources` workflow publishes it as
  `ghcr.io/kairos-io/hadron-sources/<pkg>:<version>` for the build to consume.
- `hadolint` runs in CI. Run it locally before pushing.

<!-- END GENERATED -->

<!-- Anything below this line belongs to this repository and is preserved on sync. -->

## This repository, specifically

- **`go build ./...` and `go test ./...` fail on a fresh clone.**
  `kairos-init/pkg/bundled` embeds binaries that are built rather than
  committed, so the package cannot compile until those files exist and the
  error is `pattern binaries/edgevpn: no matching files found`. Run `make test`
  and `make lint-go` instead: both depend on `kairos-init-embed-stubs`, which
  touches zero-byte placeholders. Nothing in that package needs fixing.
- **Lint with the pinned version.** `.golangci-version` holds the exact
  `golangci-lint` release CI runs. Whatever is on your PATH will report a
  different set of findings, in both directions.
- **`tests/` is a separate Go module**, `kairos-tests`. The root
  `go test ./...` does not run the end-to-end and boot suites, so a green run
  says nothing about whether the system still boots.
- **`CONTRIBUTING.md` still describes the emoji conventions, which `master`
  has mostly left behind.** It asks for a commit like
  `:seedling: Drop foo flag (fixes #123)` and for one of ten emoji labels on
  every pull request. Of the last 300 commits on `master`, 240 are
  Conventional Commits and 29 carry an emoji prefix, 25 of those from Renovate,
  which gets `:arrow_up:` from `commitMessagePrefix` in `renovate.json`. The
  emoji labels are not in use either: recent merges carry `QA: pass` and
  nothing else. Write Conventional Commits, as above. `CONTRIBUTING.md` is
  right about the `-s` sign-off.
- **If your environment has no C compiler**, `CGO_ENABLED=0 go build ./...`
  builds the whole tree. Something in the dependency graph pulls `runtime/cgo`
  by default. Continuous integration has a compiler, so do not commit a build
  tag or a dependency swap to work around a missing local one.
