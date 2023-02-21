---
title: "Meta-Distribution"
linkTitle: "Meta-Distribution"
weight: 4
date: 2022-11-13
description: >
---

We like to define Kairos as a meta-Linux Distribution, as its goal is to convert other distros to an immutable layout with Kubernetes Native components.

## Kairos

The Kairos stack is composed of the following:

- A core OS image release for each flavor in ISO, qcow2, and other similar formats (see [the list of supported distributions](/docs/reference/image_matrix)) provided for user convenience
- A release with K3s embedded.
- A set of Kubernetes Native API components (CRDs) to install into the control-plane node, to manage deployment, artifacts creation, and lifecycle (WIP).
- A set of Kubernetes Native API components (CRDs) to install into the target nodes to manage and control the node after deployment (WIP).
- An agent installed into the nodes to be compliant with Kubernetes Native API components mentioned above.

Every component is extensible and modular such as it can be customized and replaced in the stack and built off either locally or with Kubernetes.

### Internal components

Kairos encompasses several components, some externally, most notably:

- [K3s](https://k3s.io) as a Kubernetes distribution
- [edgevpn](https://mudler.github.io/edgevpn) (optional) as fabric for the distributed network, node coordination and bootstrap. Provides also embedded DNS capabilities for the cluster. Internally uses [libp2p](https://github.com/libp2p/go-libp2p) for the P2P mesh capabilities.
- [elemental-toolkit](https://rancher.github.io/elemental-toolkit/docs/) as a fundament to build the Linux derivative. Indeed, any `Elemental` docs applies to `Kairos` as well.
- [nohang](https://github.com/hakavlad/nohang) A sophisticated low memory handler for Linux.
