<!-- BEGIN GENERATED FROM kairos-io/community/agent-conventions. DO NOT EDIT THIS BLOCK BY HAND. -->
<!-- Edit the sources there, not this file. -->

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

Between repositories the bump is still a step. `AuroraBoot` depends on released
versions of `kairos` and `kairos-operator`, so pin a tag, not a pseudo-version.

Fix things where they are broken, upstream included. Do not work around a bug
locally because you assume the real fix will be slow to merge.

## Commits and pull requests

- Sign off every commit: `git commit -s`. DCO is a required check.
- Disclose in the pull request body that AI was used, and say whether a human
  read the code before it was opened.
- Conventional Commits for the subject line: `fix(iso): ...`, `docs: ...`.
- Open a pull request for every change. Push the branch to a fork if you do not
  have write access to the repository, otherwise to the repository. Never push
  to the default branch.
- Add a test for any behaviour change. A boot-path change needs a boot test.
- Keep pull requests small and single-purpose.
- Never force-push or rebase someone else's branch, and never stack a pull
  request on another unmerged branch.
- Never pass `-S`/`--gpg-sign`, and never change git config to turn signing on
  or off. Signing is the contributor's own setup, and forcing it on blocks on a
  passphrase nobody will type. `-S` is a different flag from `-s`.

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
image, cutting a backport release. Prefer one over improvising.

## Go

- Build and test through the repository's `Makefile` target where there is one.
  `go build ./...` on its own can fail on a fresh clone in repositories whose
  packages embed generated files. If a module proxy is unreachable in your
  environment, say so rather than claiming the tests passed.
- Do not bump the Go version or dependencies as a side effect of an unrelated
  change. Renovate handles routine bumps.
- Check whether `kairos/sdk` already wraps a dependency before adding it.
- Write tests in the ginkgo/gomega style the rest of the tree uses.

## Dockerfiles

- One logical step per `RUN`. Do not collapse unrelated commands to save a
  layer.
- Pin versions with a build argument rather than inlining them, so Renovate can
  see them.
- Do not add a `wget` or `curl` of an upstream tarball. In `hadron`, add it to
  `sources.yaml` with its checksum and candidate URLs, and the
  `populate-sources` workflow publishes it as
  `ghcr.io/kairos-io/hadron-sources/<pkg>:<version>`.
- `hadolint` runs in CI. Run it locally before pushing.

<!-- END GENERATED -->
