<h1 align="center">
  <br>
     <img width="184" alt="kairos-white-column 5bc2fe34" src="https://user-images.githubusercontent.com/2420543/193010398-72d4ba6e-7efe-4c2e-b7ba-d3a826a55b7d.png">
    <br>
<br>
</h1>

<h3 align="center">Kairos - Kubernetes-focused, Cloud Native Linux meta-distribution</h3>
<p align="center">
  <a href="https://github.com/kairos-io/kairos/issues"><img src="https://img.shields.io/github/issues/kairos-io/kairos"></a>
  <a href="https://github.com/kairos-io/kairos/actions/workflows/image.yaml"> <img src="https://github.com/kairos-io/kairos/actions/workflows/image.yaml/badge.svg"></a>
</p>

<p align="center">
     <br>
    The immutable Linux meta-distribution for edge Kubernetes.
</p>

<hr>


With Kairos you can build immutable, bootable Kubernetes and OS images for your edge devices as easily as writing a Dockerfile. Optional P2P mesh with distributed ledger automates node bootstrapping and coordination. Updating nodes is as easy as CI/CD: push a new image to your container registry and let secure, risk-free A/B atomic upgrades do the rest. 

Kairos (formerly `c3os`) is an open-source project which brings Edge, cloud, and bare metal lifecycle OS management into the same design principles with a unified Cloud Native API.

At-a-glance:

- :bowtie: Community Driven
- :octocat: Open Source
- :lock: Linux immutable, meta-distribution
- :key: Secure
- :whale: Container based
- :penguin: Distribution agnostic

Kairos can be used to:

- Easily spin-up a Kubernetes cluster, with the Linux distribution of your choice :penguin:
- Manage the cluster lifecycle with Kubernetes—from building, to provisioning, and upgrading :rocket:
- Create a multiple—node, single cluster that spans up across regions :earth_africa:

For comprehensive docs, tutorials, and examples see our [documentation](https://kairos.io).

## Project status

- (Sep 29 2022) announcing Kairos 1.0 GA availability. Kairos is now backed by Spectro Cloud, which contributes to the project. Kairos will remain fully community-driven and has its own governance. See the [announcement](https://github.com/kairos-io/kairos/discussions/159)
- (Sep 15 2022) the c3OS project has a new name: Kairos! For full details, see https://github.com/c3os-io/c3os/issues/88 and https://github.com/c3os-io/c3os/discussions/84. 

## What is it ?

Kairos is a Cloud Native, meta-Linux distribution that can be built, managed, and ran with Kubernetes.

Why/when should I use it?

- Build your Cloud on-premise, no vendor-lock in—completely Open Source
- Brings the same convenience as a public cloud on—premises
- Node provisioning, by bringing your image or using the Kairos releases.
- For appliances that don't have to be Kubernetes application, specific-its design fits multiple use case scenarios

## Features

- At the current state, Kairos can create a multiple-node Kubernetes cluster with [k3s](https://k3s.io)—all k3s features are supported.
- Upgrades can be done manually via CLI or with Kubernetes. Distribution of upgrades are done via container registries.
- An immutable distribution that you can configure to your needs while maintaining its immutability.
- Node configuration via a single, cloud-init config file.
- Handle airgap upgrades with in—cluster, container registries.
- Extend the image in runtime or build time via Kubernetes Native API.
- Plans to support CAPI, with full device lifecycle management.
- Plans to support up to RKE2, kubeadm, and much more!
- Nodes can optionally connect autonomously via a fully meshed peer-to-peer (P2P) hybrid VPN network. It allows you to stretch a cluster up to 10000 km!
  Kairos can create private virtual network segments to enhance your cluster perimeter without any single point of failure (SPOF).

## More than a Linux distribution

Kairos is available as ISO, qcow2, and NetBoot artifact for user convenience, but it is more than that. It allows turning any Linux distribution into a uniform, conformant distribution with an immutable design. As such, any distribution which is *converted* will share the same, common feature set between all of them, and they are managed in the same way by Kubernetes Native API components.

Any input OS will inherit:

- Immutability
- A/B upgrades
- Booting mechanism fallback
- Boot assessment
- Single image, container-based atomic upgrades
- Cloud-init support
- All the Kairos feature-set

Kairos treats all the operating environments homogeneously in a distribution-agnostic fashion.

The OS is a container image. That means that upgrades to nodes are distributed via container registries.

Installations medium and other assets, required to boot bare metal or Edge devices, are built dynamically by the Kubernetes Native API components provided by Kairos.

![livecd](https://user-images.githubusercontent.com/2420543/189219806-29b4deed-b4a1-4704-b558-7a60ae31caf2.gif)

## Goals

The Kairos ultimate goal is to bridge the gap between Cloud and Edge by creating a smooth user experience. Several areas in the ecosystem can be improved for edge deployments to make it in pair with the cloud.

The Kairos project encompasses all the tools and architectural pieces needed to fill those gaps. This spans between providing Kubernetes Native API components to assemble OSes, deliver upgrades, and control nodes after deployment.

Kairos is distribution-agnostic and embraces openness: the user can provide their own underlying base image, and Kairos onboards it and takes it over to make it cloud-native, immutable, and plugs into an already rich ecosystem by leveraging containers as a distribution medium.

## Contribute

Kairos is an open-source project, and any contribution is more than welcome! The project is big and narrows to various degrees of complexity and problem space. Feel free to join our chat, discuss in our forums and join us during Office hours.

We have an open roadmap, so you can always have a look at what's going on and actively contribute to it.

Useful links:

- [Upcoming releases](https://github.com/kairos-io/kairos/issues?q=is%3Aissue+is%3Aopen+label%3Arelease)

## Community

You can find us at:

- [#kairos-io at matrix.org](https://matrix.to/#/#kairos-io:matrix.org)
- [IRC #kairos in libera.chat](https://web.libera.chat/#kairos)
- [GitHub Discussions](https://github.com/kairos-io/kairos/discussions)

### Project Office Hours

Project Office Hours is an opportunity for attendees to meet the maintainers of the project, learn more about the project, ask questions, and learn about new features and upcoming updates.

[Add to Google Calendar](https://calendar.google.com/calendar/embed?src=c_6d65f26502a5a67c9570bb4c16b622e38d609430bce6ce7fc1d8064f2df09c11%40group.calendar.google.com&ctz=Europe%2FRome)

Office hours are happening weekly on Wednesday, 5:30 – 6:00 pm CEST (Central European Summer Time). [Meeting link](https://meet.google.com/aus-mhta-azb)

Besides, we have monthly meetups to participate actively in the roadmap planning and presentation:

#### Roadmap planning

We will discuss agenda items and groom issues, where we plan where they fall into the release timeline.

Occurring: Monthly on the first Wednesday, 5:30 – 6:30 pm CEST. [Meeting link](https://meet.google.com/fkp-wyjo-qwz)

#### Roadmap presentation

We will discuss the items of the roadmaps and the expected features in the next releases.

Occurring: Monthly on the second Wednesday, 5:30 pm CEST [Meeting link](https://meet.google.com/cjs-ngcd-ngt)

## Alternatives

Other projects are similar to Kairos which are great and worth mentioning, and actually, Kairos took to some degree inspiration.
However, Kairos has different goals and takes completely unique approaches to the underlying system, upgrade, and node lifecycle management.

- [k3os](https://github.com/rancher/k3os)
- [Talos](https://github.com/siderolabs/talos)
- [FlatCar](https://flatcar-linux.org/)
- [CoreOS](https://getfedora.org/it/coreos?stream=stable)

## Development

### Building Kairos

Requirements: Needs only docker.

Run `./earthly.sh +all --FLAVOR=opensuse`, which should produce a Docker image along with a working ISO.


