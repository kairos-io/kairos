![background](https://user-images.githubusercontent.com/2420543/153506895-fb978c1e-8197-42e2-9ce2-3be6e0907acc.jpg?classes=shadow&width=50pc)

# Welcome

Welcome to the c3os documentation!

c3OS is an open-source project which brings Edge, cloud and bare metal lifecycle OS management into the same design principles with a unified Kubernetes native API.

In a glance:

- Community Driven
- Open Source
- [Meta-Distribution](/architecture/meta), Distro agnostic
- [Immutable](/architecture/immutable)
- Secure
- [Container based](/architecture/container)
- [P2P Mesh](/architecture/network)

c3OS can be used to:

- Easily spin up a Kubernetes cluster, with the Linux distribution of your choice
- Manage the cluster lifecycle with Kubernetes - from building, to provisioning and upgrading
- Create a multiple-node single cluster which spans up across regions

To get familiar with c3os, check out the [quickstart](/quickstart/installation).

## What is it ?

c3OS is a Kubernetes native, meta-Linux distribution that can be built, managed, and run with Kubernetes.

Why/when should I use it?

- Build your Cloud on-prem, no vendor-lock in, completely Open Source
- Brings same convenience of public cloud on premises
- Node provisioning, by bringing your own image or just use the c3os releases.
- For appliances that doesn't have to be Kubernetes application specific - its design fits multiple use case scenarios

## Features

- At the current state c3OS can create multiple-node Kubernetes cluster with [k3s](https://k3s.io) - all k3s features are supported
- Upgrades can be done manually via CLI or with Kubernetes. Distribution of upgrades are done via container registries.
- An Immutable distribution which you can configure to your needs, while keep staying immutable
- Node configuration via a single cloud-init config file.
- Handle airgap upgrades with in-cluster container registries
- Extend the image in runtime or build time via Kubernetes Native API
- Plans to support CAPI, with full device lifecycle management
- Plans to support up to rke2, kubeadm, and much more!
- Nodes can optionally connect autonomously via full-mesh p2p hybrid VPN network. It allows to stretch a cluster up to 10000 km!
  c3OS can create private virtual network segments to enhance your cluster perimeter without any SPOF.

## More than a Linux distribution

c3OS is available as ISO, qcow2 and netboot artifact for user convenience based from Alpine and openSUSE, but it is actually more than that. It allows to turn any Linux distribution into a uniform, comformant distro with immutable design. As such, any distro which is "converted" will share the same, common feature set between all of them, and they are managed in the same way by Kubernetes Native API components.

Any input OS will inherit:

- Immutability
- A/B upgrades
- Booting mechanism Fallback
- Boot assessment
- Single image, container based atomic upgrades
- Cloud init support
- All the c3OS feature-set

C3os treats all the OSes homogeneously in a distro-agnostic fashion. 

The OS is a container image. That means that upgrades to nodes are distributed via container registries.

Installations medium and other assets required to boot baremetal or Edge devices are built dynamically by the Kubernetes Native API components provided by c3os. 

![livecd](https://user-images.githubusercontent.com/2420543/189219806-29b4deed-b4a1-4704-b558-7a60ae31caf2.gif)

## Goals

The c3OS ultimate goal is to bridge the gap between Cloud and Edge by creating a smooth user experience. There are several areas in the ecosystem that can be improved for edge deployments to make it in pair with the cloud. 

The c3OS project encompassess all the tools and architetural pieces needed to fill those gaps. This spans between providing Kubernetes Native API components to assemble OSes, deliver upgrades, and control nodes after deployment.

c3OS is distro-agnostic, and embraces openness: the user can provide their own underlaying base image, and c3os onboards it and takes it over to make it Cloud Native, Immutable that plugs into an already rich ecosystem by leveraging containers as distribution medium.

## Contribute

c3OS is an open source project, and any contribution is more than welcome! The project is big and narrows to various degree of complexity and problem space. Feel free to join our chat, discuss in our forums and join us in the Office hours

We have an open roadmap, so you can always have a look on what's going on, and actively contribute to it. 

Useful links:

- [Upcoming releases](https://github.com/c3os-io/c3os/issues?q=is%3Aissue+is%3Aopen+label%3Arelease)


## Community

You can find us at:

- [#c3os at matrix.org](https://matrix.to/#/#c3os:matrix.org) 
- [IRC #c3os in libera.chat](https://web.libera.chat/#c3os)
- [Github Discussions](https://github.com/c3os-io/c3os/discussions)

### Project Office Hours

Project Office Hours is an opportunity for attendees to meet the maintainers of the project, learn more about the project, ask questions, learn about new features and upcoming updates.

Office hours are happening weekly on Wednesday - 5:30 – 6:00pm CEST. [Meeting link](https://meet.google.com/aus-mhta-azb) 

Besides we have monthly meetup to partecipate actively into the roadmap planning and presentation:

#### Roadmap planning

We will discuss on agenda items and groom issues, where we plan where they fall into the release timeline.

Occurring: Monthly on the first Wednesday - 5:30 – 6:30pm CEST. [Meeting link](https://meet.google.com/fkp-wyjo-qwz)

#### Roadmap presentation

We will discuss the items of the roadmaps and the expected features on the next releases

Occurring: Monthly on the second Wednesday - 5:30pm CEST [Meeting link](https://meet.google.com/cjs-ngcd-ngt)

## Alternatives

There are other projects that are similar to c3os which are great and worth to mention, and actually c3os took to some degree inspiration from. 
However, c3os have different goals and takes completely unique approaches to the underlying system, upgrade and node lifecycle management.

- [k3os](https://github.com/rancher/k3os)
- [Talos](https://github.com/siderolabs/talos)
- [FlatCar](https://flatcar-linux.org/)
- [CoreOS](https://getfedora.org/it/coreos?stream=stable)

## Development

### Building c3os

Requirements: Needs only docker.

Run `./earthly.sh +all --FLAVOR=opensuse`, should produce a docker image along with a working ISO




## What's next?

See the [quickstart](/quickstart/) to install c3os on a VM and create a Kubernetes cluster!