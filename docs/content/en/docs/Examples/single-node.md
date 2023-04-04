---
title: "Single Node k3s cluster"
linkTitle: "Single node k3s cluster"
weight: 1
description: > 
    This section describe examples on how to deploy Kairos with k3s as a single-node cluster
---

In the example below we will use a bare metal host to provision a Kairos node in the local network with K3s.

## Installation

Use the [provider-kairos](https://github.com/kairos-io/provider-kairos) artifacts which contains `k3s`.

Follow the [Installation](/docs/installation) documentation, and use the following cloud config file with Kairos:

```yaml
#cloud-config

hostname: metal-{{ trunc 4 .MachineID }}
users:
- name: kairos
  # Change to your pass here
  passwd: kairos
  ssh_authorized_keys:
  # Replace with your github user and un-comment the line below:
  # - github:mudler

k3s:
  enabled: true
  args:
  - --disable=traefik,servicelb
```

Notably:

- We use the `k3s` block to disable `traefik` and `servicelb` (the default `k3s` load balancer).
- In a single-node setup, you may wish to use a non-generated node name. This can be achieved with these options:
  ```
    k3s:
    enabled: true
    replace_args: true
    args:
    - --node-name=my-node
  ```
  {{% alert title="Note" %}}
  `replace_args` replaces all arguments otherwise passed to k3s by Kairos with those supplied here. Make sure you pass all the arguments you need.
  {{% /alert %}}
- We use `write_files` to write manifests to the default `k3s` manifest directory (`/var/lib/rancher/k3s/server/manifests/`) see [docs](/docs/reference/configuration#kubernetes-manifests) to deploy `MetalLB` and configure it with the `192.168.1.10-192.168.1.20` IP range. Make sure to pick up a range which doesn't interfere with your local DHCP network.
