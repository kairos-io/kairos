<h1 align="center">
  <br>
     <img src="https://user-images.githubusercontent.com/2420543/153508410-a806a385-ae3e-417e-b87e-7472f21689e3.png" width=500>
	<br>
<br>
</h1>

<h3 align="center">Kubernetes-focused, Cloud Native Linux meta-distribution</h3>
<p align="center">
  <a href="https://github.com/c3os-io/c3os/issues"><img src="https://img.shields.io/github/issues/c3os-io/c3os"></a>
  <a href="https://quay.io/repository/c3os/c3os"> <img src="https://quay.io/repository/mudler/c3os/status"></a>
</p>

<p align="center">
	 <br>
    Kubernetes-focused Linux Distro - K3s - Automatic Node discovery/VPN
</p>

<hr>

c3OS is an open-source project which brings Edge, cloud and bare metal lifecycle OS management into the same design principles with a unified Kubernetes native API.

In a glance:

- Community Driven
- Open Source
- Linux immutable meta-Distro
- Secure
- Container based
- Distro agnostic


[Documentation available here](https://docs.c3os.io).

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

## More than a Linux distribution

c3OS is available as ISO, qcow2 and netboot artifact for user convenience, but it is actually more than that. It allows to turn any Linux distribution into a uniform, comformant distro with immutable design. As such, any distro which is "converted" will share the same, common feature set between all of them, and they are managed in the same way by Kubernetes Native API components.

Any input OS will inherit:

- Immutability
- A/B upgrades
- Booting mechanism Fallback
- Boot assessment
- Single image, container based atomic upgrades
- All the c3OS feature-set

C3os treats all the OSes homogeneously in a distro-agnostic fashion. 

The OS is a container image. That means that upgrades to nodes are distributed via container registries.

Installations medium and other assets required to boot baremetal or Edge devices are built dynamically by the Kubernetes Native API components provided by c3os. 

## What is an Immutable system?

An immutable OS is a carefully engineered system which boots in a restricted, permissionless mode, where certain paths of the system are not writeable. For instance, after installation it's not possible to install additional packages in the system, and any configuration change is discarded after reboot.

A running Linux based OS system will look like with the following paths:

```
/usr/local - persistent ( partition label COS_PERSISTENT)
/oem - persistent ( partition label COS_OEM)
/etc - ephemeral
/usr - read only
/ immutable
```

`/usr/local` will contain all the persistent data which will be carried over in-between upgrades, instead, any change to `/etc` will be discarded.

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
- [Github Discussions](https://github.com/c3os-io/c3os/discussions)

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

### Internal components

C3OS encompassess several components, most notably:

- [k3s](https://k3s.io) as a Kubernetes distribution
- [edgevpn](https://mudler.github.io/edgevpn) (optional) as fabric for the distributed network, node coordination and bootstrap. Provides also embedded DNS capabilities for the cluster.
- [elemental-toolkit](https://rancher.github.io/elemental-toolkit/docs/) as a fundament to build the Linux derivative. Indeed, any `Elemental` docs applies to `c3os` as well.
- [nohang](https://github.com/hakavlad/nohang) A sophisticated low memory handler for Linux 
