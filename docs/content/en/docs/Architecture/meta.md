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

Kairos encompasses several components, external and internal.

Internal:
- [kairos](https://github.com/kairos-io/kairos) is the main repository, building the `kairos-agent` and containing the image definitions which runs on our CI pipelines.
- [immucore](https://github.com/kairos-io/immucore) is the immutability management interface.
- [AuroraBoot](https://github.com/kairos-io/AuroraBoot) is the Kairos Node bootstrapper
- [elemental-cli](https://github.com/kairos-io/elemental-cli) manages the installation, reset, and upgrade of the Kairos node.
- [system packages](https://github.com/kairos-io/packages) contains additional packages, cross-distro, partly used in framework images
- [kcrypt](https://github.com/kairos-io/kcrypt) is the component responsible for encryption and decryption of data at rest
- [kcrypt-challenger](https://github.com/kairos-io/kcrypt-challenger) is the `kairos` plugin that works with the TPM chip to unlock LUKS partitions
- [osbuilder](https://github.com/kairos-io/osbuilder) is used to build bootable artifacts from container images
- [entangle](https://github.com/kairos-io/entangle) a CRD to interconnect Kubernetes clusters
- [entangle-proxy](https://github.com/kairos-io/entangle-proxy) a CRD to control interconnetted clusters

External:
- [K3s](https://k3s.io) as a Kubernetes distribution
- [edgevpn](https://mudler.github.io/edgevpn) (optional) as fabric for the distributed network, node coordination and bootstrap. Provides also embedded DNS capabilities for the cluster. Internally uses [libp2p](https://github.com/libp2p/go-libp2p) for the P2P mesh capabilities.
- [nohang](https://github.com/hakavlad/nohang) A sophisticated low memory handler for Linux.
