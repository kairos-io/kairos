---
title: "FAQ"
linkTitle: "Frequently asked questions"
weight: 9
date: 2022-11-13
description: >
---

## What is the difference between Kairos compared to Talos/Sidero Metal and Flatcar?

Kairos is distro-agnostic by design. Currently, you can pick among a list from the [supported matrix](/docs/reference/image_matrix/#image-flavors), but we are working on CRDs to let assemble OSes from other bases in a Kubernetes native way.

The key difference, is that the OS is distributed as a standard container, similar to how apps are distributed with container registries. You can also use `docker run` locally and inspect the OS, and similarly, push customizations by pointing nodes to a new image.

Also, Kairos is easy to setup. The P2P capabilities allow nodes to self-coordinate, simplifying the setting up of a multi-node cluster.

## What would be the difference between Kairos and Fedora Coreos?

Kairos is distribution agnostic. It supports all the distributions in the [supported matrix](/docs/reference/image_matrix/#image-flavors). In addition, we plan to have K3s automatically deploy Kubernetes (even by self-coordinating nodes).

Additionally, Kairos is OCI-based, and the system is based from a container image. This makes it possible to also run it locally with `docker run` to inspect it, as well to customize and upgrade your nodes by just pointing at it. Think of it like containers apps, but bootable.

## If the OS is a container, what is running the container runtime beneath?

There is no real container runtime. The container is used to construct an image internally, that is then used to boot the system in an A/B fashion, so there is no overhead at all. The system being booted is actually a snapshot of the container.

## Does this let the OS "containers" install extra kernel extensions/drivers?

Every container/OS ships its own kernels and drivers within a single image, so you can customize that down the road quite easily. Since every release is a standard container, you can customize it just by writing your own Dockerfile and point your nodes at it. You can also use the CRDs, that allow you to do that natively inside Kubernetes to automate the process even further.

Kairos also supports live overlaying, but that doesn't apply to kernel modules. However, that is somewhat discouraged, as it introduces snowflakes in your clusters unless you have a management cluster.

## How is the P2P mesh formed? Is there an external service for discovery?

The P2P mesh is optional and internally uses libp2p. You can use your own discovery bootstrap server or use the default already baked in the library. Furthermore you can limit and scope that only to local networks. For machines behinds a NAT, nodes operate automatically as relay servers (hops) when they are detected to be capable of it. You can limit that to specific nodes, or let automatic discovery handle it.
