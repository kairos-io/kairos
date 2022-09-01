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

To get familiar with c3os, check out the [quickstart](/quickstart/installation).

## What is c3OS? Why should I use it ?

c3OS is a Kubernetes native, meta-Linux distribution that can be built, managed, and run with Kubernetes.

Why/when should I use it?

- Build your Cloud on-prem, no vendor-lock in, completely Open Source
- Brings same convenience of public cloud on premises
- Node provisioning, by bringing your own image or just use the c3os releases.
- For appliances that doesn't have to be Kubernetes application specific - its design fits multiple use case scenarios

## Features

- At the current state c3OS can create multiple-node Kubernetes cluster with [k3s](https://k3s.io) - all k3s features are supported
- Nodes can optionally connect autonomously via full-mesh p2p hybrid VPN network. It allows to stretch a cluster up to 10000 km!
  c3OS can create private virtual network segments to enhance your cluster perimeter without any SPOF.
- Upgrades can be done manually via CLI or with Kubernetes. Distribution of upgrades are done via container registries.
- An Immutable distribution which you can configure to your needs, while keep staying immutable
- Extend the image in runtime or build time via Kubernetes Native API
- Plans to support CAPI, with full device lifecycle management
- Plans to support up to rke2, kubeadm, and much more!

## More than a standard Linux distribution

c3OS is available as ISO, qcow2 and netboot artifact derived from Alpine and openSUSE for user convenience, but it is actually more than that. It allows to turn any Linux distribution into a uniform, comformant Linux distribution with an immutable design. As such, any "converted" distro will share the same, common feature set between all of them, and they are managed in the same way by Kubernetes Native API components.

Any input OS will inherit:

- Immutability
- A/B upgrades
- Booting mechanism Fallback
- Boot assessment
- Single image, container based atomic upgrades
- All the c3OS feature-set

C3os treats all the OSes homogeneously in a distro-agnostic fashion.  The OS is a container image and upgrades to nodes are distributed via container registries.

Installations medium and other assets required to boot baremetal or Edge devices are built dynamically by the Kubernetes Native API components provided by c3os. 

## Goals

The c3OS ultimate goal is to bridge the gap between Cloud and Edge by creating a smooth user experience. There are several areas in the ecosystem that can be improved for edge deployments to make it in pair with the cloud. 

The c3OS project encompassess all the tools and architetural pieces needed to fill those gaps. This spans between providing Kubernetes Native API components to assemble OSes, deliver upgrades, and control nodes after deployment.

c3OS is distro-agnostic, and embraces openness: the user can provide their own underlaying base image, and c3os onboards it and takes it over to make it Cloud Native, Immutable that plugs into an already rich ecosystem by leveraging containers as distribution medium.

## Contribute

c3OS is an open source project, and any contribution is more than welcome! The project is big and narrows to various degree of complexity and problem space. Feel free to join our chat, discuss in our forums and join us in the Office hours

We have an open roadmap, so you can always have a look on what's going on, and actively contribute to it. 

## Community

You can find us at:

- [#c3os at matrix.org](https://matrix.to/#/#c3os:matrix.org) 
- [IRC #c3os in libera.chat](https://web.libera.chat/#c3os)
- [Discussions](https://github.com/c3os-io/c3os/discussions)

## Alternatives

There are other projects that are similar to c3os which are great and worth to mention, and actually c3os took to some degree inspiration from. 
However, c3os have different goals and takes completely unique approaches to the underlying system, upgrade and node lifecycle management.

- [Talos](https://github.com/siderolabs/talos)
- [FlatCar](https://flatcar-linux.org/)
- [CoreOS](https://getfedora.org/it/coreos?stream=stable)
- [k3os](https://github.com/rancher/k3os)

## Internal components

C3OS encompassess several components, most notably:

- [k3s](https://k3s.io) as a Kubernetes distribution
- [edgevpn](https://mudler.github.io/edgevpn) (optional) as fabric for the distributed network, node coordination and bootstrap. Provides also embedded DNS capabilities for the cluster.
- [elemental-toolkit](https://rancher.github.io/elemental-toolkit/docs/) as a fundament to build the Linux derivative. Indeed, any `Elemental` docs applies to `c3os` as well.
- [nohang](https://github.com/hakavlad/nohang) A sophisticated low memory handler for Linux 

## What's next?

See the [quickstart](/quickstart/) to install c3os on a VM and create a Kubernetes cluster!