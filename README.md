<h1 align="center">
  <br>
     <img width="184" alt="kairos-white-column 5bc2fe34" src="https://user-images.githubusercontent.com/2420543/193010398-72d4ba6e-7efe-4c2e-b7ba-d3a826a55b7d.png">
    <br>
<br>
</h1>

<h3 align="center">Kairos - Kubernetes-focused, Cloud Native Linux meta-distribution</h3>
<p align="center">
  <a href="https://github.com/kairos-io/kairos/issues"><img src="https://img.shields.io/github/issues/kairos-io/kairos"></a>
  <a href="https://github.com/kairos-io/kairos/actions/workflows/master.yaml"> <img src="https://github.com/kairos-io/kairos/actions/workflows/master.yaml/badge.svg"></a>
  <a href="https://www.bestpractices.dev/projects/9100"><img src="https://www.bestpractices.dev/projects/9100/badge"></a>
  <a href="https://clomonitor.io/projects/cncf/kairos"><img src="https://img.shields.io/endpoint?url=https://clomonitor.io/api/projects/cncf/kairos/badge"></a>
  <a href="https://scorecard.dev/viewer/?uri=github.com/kairos-io/kairos"><img src="https://api.scorecard.dev/projects/github.com/kairos-io/kairos/badge"></a>
</p>

<p align="center">
     <br>
    The immutable Linux meta-distribution for edge Kubernetes.
</p>

<hr>

With Kairos you can build immutable, bootable Kubernetes and OS images for your edge devices as easily as writing a Dockerfile. Optional P2P mesh with distributed ledger automates node bootstrapping and coordination. Updating nodes is as easy as CI/CD: push a new image to your container registry and let secure, risk-free A/B atomic upgrades do the rest. Kairos is part of the Secure Edge-Native Architecture (SENA) to securely run workloads at the Edge ([whitepaper](https://github.com/kairos-io/kairos/files/11250843/Secure-Edge-Native-Architecture-white-paper-20240417.3.pdf)).

Kairos (formerly `c3os`) is an open-source project which brings Edge, cloud, and bare metal lifecycle OS management into the same design principles with a unified Cloud Native API.

At-a-glance:

- :bowtie: Community Driven
- :octocat: Open Source
- :lock: Linux immutable, meta-distribution
- :key: Secure
- :whale: Container-based
- :penguin: Distribution agnostic

Kairos can be used to:

- Easily spin-up a Kubernetes cluster, with the Linux distribution of your choice :penguin:
- Create your Immutable infrastructure, no more infrastructure drift! :lock:
- Manage the cluster lifecycle with Kubernetes—from building to provisioning, and upgrading :rocket:
- Create a multiple—node, a single cluster that spans up across regions :earth_africa:

For comprehensive docs, tutorials, and examples see our [documentation](https://kairos.io/getting-started/).

## Repository layout

Kairos is a monorepo. The device-runtime binaries, the SDK, and the
image initializer all live in one source tree, share a single
`go.mod` at the root, and ship on one release tag.

- `cmd/kairos/` -- the multi-call `kairos` binary. Dispatches on
  `argv[0]` to immucore, kairos-agent, or kcrypt-discovery-challenger,
  so one binary is deployed and the historical names point at it via
  symlinks.
- `cmd/kcrypt-challenger/` -- the in-cluster kcrypt-challenger
  server. Not linked into the multi-call binary (different deps and
  lifecycle); ships as its own container image.
- `agent/` -- kairos-agent source (was `kairos-io/kairos-agent`).
- `immucore/` -- immucore source (was `kairos-io/immucore`).
- `kcrypt/discovery/` -- device-side kcrypt discovery
  (was `kairos-io/kcrypt-discovery-challenger`).
- `kcrypt/challenger/` -- in-cluster kcrypt-challenger package
  (was `kairos-io/kcrypt-challenger`).
- `sdk/` -- Kairos SDK, importable externally at
  `github.com/kairos-io/kairos/v4/sdk` (was `kairos-io/kairos-sdk`).
- `kairos-init/` -- image initializer that installs the multi-call
  binary plus symlinks into a base image
  (was `kairos-io/kairos-init`).
- `installer/` -- interactive terminal-UI installer embedded by
  kairos-init at `/system/installer/kairos-installer` and invoked by
  `kairos-agent interactive-install` (was `kairos-io/kairos-installer`).
- `tests/` -- monorepo end-to-end test suite.

Each absorbed subdirectory keeps a thin `main.go` at its root, so
`go build ./agent`, `go build ./immucore`, and
`go build ./kcrypt/cmd/discovery` still produce standalone binaries
from any commit; consumers that are not ready for the multi-call
binary can pin one of those.

The historical repos (`kairos-agent`, `immucore`,
`kcrypt-discovery-challenger`, `kcrypt-challenger`, `kairos-sdk`,
`kairos-init`, `kairos-installer`, `kairos-factory-action`) are
archived. Their tagged releases remain resolvable so pre-migration
pins keep working; post-migration fixes land here and ship on the
monorepo release tag.

The absorbed `kairos-factory-action` reusable workflow lives at
`.github/workflows/reusable-factory.yaml` (reusable workflows have to
live at `.github/workflows/` of the repo root -- GitHub does not
accept them in subdirectories), so it does not follow the subdir
pattern the other absorbed components did. External consumers rewrite
`uses: kairos-io/kairos-factory-action/.github/workflows/reusable-factory.yaml@<ref>`
to `uses: kairos-io/kairos/.github/workflows/reusable-factory.yaml@<ref>`
and repin at a monorepo ref (SHA, `master`, or a Kairos release tag).

## Project status

Check the [Roadmap](https://github.com/orgs/kairos-io/projects/2) for a high-level view of what features are coming to Kairos.

Or go to the [Project Board](https://github.com/orgs/kairos-io/projects/1/views/1) to check what the team is working on right now!

To stay up-to-date, check out the [Kairos Blog](https://kairos.io/blog/). You will find also release announcements and deep-dive into Kairos features!

## Community

You can find us at:

- [Cloud Native Slack #kairos channel](https://cloud-native.slack.com/archives/C0707M8UEU8)
- [#kairos-io at matrix.org](https://matrix.to/#/#kairos-io:matrix.org)
- [IRC #kairos in libera.chat](https://web.libera.chat/#kairos)
- [GitHub Discussions](https://github.com/kairos-io/kairos/discussions)

The [:handshake: community repository](https://github.com/kairos-io/community) contains information about how to get involved, Code of conduct, Maintainers, Contribution guidelines, including also links to our weekly meeting notes, roadmap, and more.

Looking for something to work on? Browse Kairos issues that need a hand on [CLOTributor](https://clotributor.dev/search?project=kairos&foundation=cncf).

## Governance

The Kairos project governance can be found [in the community repository](https://github.com/kairos-io/community/blob/main/GOVERNANCE.md). 

**Note:** Kairos adopts the CNCF Code of conduct - please make sure to read the CNCF [Code of Conduct document](https://github.com/kairos-io/community/blob/main/CODE_OF_CONDUCT.md).

### Project Office Hours

Project Office Hours is an opportunity for attendees to meet the maintainers of the project, learn more about the project, ask questions, and learn about new features and upcoming updates.

[Add to Google Calendar](https://calendar.google.com/calendar/embed?src=c_6d65f26502a5a67c9570bb4c16b622e38d609430bce6ce7fc1d8064f2df09c11%40group.calendar.google.com&ctz=Europe%2FRome)

---

Kairos is a [Cloud Native Computing Foundation (CNCF) Sandbox project](https://www.cncf.io/sandbox-projects/) and was contributed by [Spectrocloud](https://spectrocloud.com).

