---
title: "MetalLB"
linkTitle: "MetalLB"
weight: 3
description: > 
    This section describe examples on how to deploy Kairos with k3s and MetalLB
---

In the example below we will use a bare metal host to provision a Kairos node in the local network with K3s and MetalLB using the `192.168.1.10-192.168.1.20` IP range.

[MetalLB](https://metallb.universe.tf/) is a load-balancer implementation for bare metal Kubernetes clusters, using standard routing protocols. Can be used with [k3s](https://k3s.io) in Kairos to provide Load Balancing for baremetal and manage IPs in a cluster.

## Installation

Use the [provider-kairos](https://github.com/kairos-io/provider-kairos) artifacts which contains `k3s`.

We will use the [k3s manifest method](/docs/reference/configuration#kubernetes-manifests) to deploy `MetaLB`.

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

# Additional manifests that are applied by k3s on boot
write_files:
- path: /var/lib/rancher/k3s/server/manifests/metallb.yaml
  permissions: "0644"
  content: |
        apiVersion: v1
        kind: Namespace
        metadata:
          name: metallb-system
        ---
        apiVersion: helm.cattle.io/v1
        kind: HelmChart
        metadata:
          name: metallb
          namespace: metallb-system
        spec:
          chart: https://github.com/metallb/metallb/releases/download/metallb-chart-0.13.7/metallb-0.13.7.tgz
- path: /var/lib/rancher/k3s/server/manifests/addresspool.yaml
  permissions: "0644"
  content: |
        apiVersion: metallb.io/v1beta1
        kind: IPAddressPool
        metadata:
          name: default
          namespace: metallb-system
        spec:
          addresses:
          - 192.168.1.10-192.168.1.20
        ---
        apiVersion: metallb.io/v1beta1
        kind: L2Advertisement
        metadata:
          name: default
          namespace: metallb-system
        spec:
          ipAddressPools:
          - default
```

Notably:

- we use the `k3s` block to disable `traefik` and `servicelb` (the default `k3s` load balancer)
- we use `write_files` to write manifests to the default `k3s` manifest directory (`/var/lib/rancher/k3s/server/manifests/`) see [docs](/docs/reference/configuration#kubernetes-manifests) to deploy `MetalLB` and configure it with the `192.168.1.10-192.168.1.20` IP range. Make sure to pick up a range which doesn't interfere with your local DHCP network.

## Resources

- [TNS blog post](https://thenewstack.io/livin-kubernetes-on-the-immutable-edge-with-kairos-project/)