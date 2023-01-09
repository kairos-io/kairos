---
title: "Bundles"
linkTitle: "Bundles"
weight: 4
description: > 
    This section describe examples on how to use Kairos bundles to apply custom configurations
---

In the example below we will use a bare metal host to provision a Kairos node in the local network with K3s and MetalLB using the `192.168.1.10-192.168.1.20` IP range. Instead of doing it manually as in the [MetalLB](/docs/examples/metallb) example, we will use a bundle to set up MetalLB automatically. See also the [bundle](/docs/advanced/bundles) documentation section to learn how to build bundles and how to use them. This section provides a simple example that uses a pre-configured bundle to setup `MetalLB`.

[MetalLB](https://metallb.universe.tf/) is a load-balancer implementation for bare metal Kubernetes clusters, using standard routing protocols. Can be used with [k3s](https://k3s.io) in Kairos to provide Load Balancing for baremetal and manage IPs in a cluster.

## Installation

Use the [provider-kairos](https://github.com/kairos-io/provider-kairos) artifacts which contains `k3s`.

We will use the MetalLB community [bundle](/docs/advanced/bundles) to deploy `MetaLB`.

Follow the [Installation](/docs/installation) documentation, and use the following cloud config file with Kairos:

```yaml
#cloud-config

hostname: metal-{{ trunc 4 .MachineID }}
users:
- name: kairos
  # Change to your pass here
  passwd: kairos
  ssh_authorized_keys:
  # Add your github user here!
  - github:mudler

k3s:
  enabled: true
  args:
  - --disable=traefik,servicelb

# Specify the bundle to use
bundles:
- targets:
  - run://quay.io/kairos/community-bundles:metallb_latest

# Specify metallb settings, available only with the bundle.
metallb:
  version: 0.13.7
  address_pool: 192.168.1.10-192.168.1.20
```

Notably:

- we use the `metallb` block that is provided by the metallb bundle and set the `MetalLB` versin that we want to deploy and the `address_pool` available for our services.
- we use the `bundles` block to enable the `run` [bundle](/docs/advanced/bundles) type. The bundle used is part of [community-bundles](https://github.com/kairos-io/community-bundles)
