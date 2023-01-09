---
title: "Bundles"
linkTitle: "Bundles"
weight: 4
description: > 
    This section describe examples on how to use a Kairos bundle to deploy MetalLB on top of K3s
---

Welcome to the guide on setting up MetalLB on a Kairos cluster with K3s! This tutorial will walk you through the steps of using a Kairos [bundle](/docs/advanced/bundles) to automatically configure MetalLB on your local network with an IP range of `192.168.1.10-192.168.1.20`. Check out the [MetalLB](/docs/examples/metallb) example to configure it without a [bundle](/docs/advanced/bundles).

For those unfamiliar with [MetalLB](https://metallb.universe.tf/), it is an open-source load balancer implementation for bare metal Kubernetes clusters that utilizes standard routing protocols. When used with K3s on Kairos, it provides load balancing capabilities and helps manage IP addresses within a cluster. 


## Prerequisites

Before we begin, you will need to have the following:

1. Kairos [provider-kairos](https://github.com/kairos-io/provider-kairos) artifacts which includes K3s
1. A baremetal node to run the installation

## Installation

1. Follow the [Installation](/docs/installation) documentation for Kairos.
1. Use the following cloud configuration file when setting up Kairos:

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

There are a few key points to note in the configuration file:

- The `metallb` block is provided by the MetalLB bundle and allows us to specify the version of MetalLB that we want to deploy, as well as the `address_pool` available for our services.
- The `bundles` block enables the `run` [bundle](/docs/advanced/bundles) type. The bundle we are using is part of the [community-bundles](https://github.com/kairos-io/community-bundles) repository.

And that's it! With these steps, you should now have MetalLB configured and ready to use on your Kairos cluster. If you have any questions or run into any issues, don't hesitate to check out the [bundle documentation](/docs/advanced/bundles) or reach out to the community for support.