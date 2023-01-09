---
title: "MetalLB"
linkTitle: "MetalLB"
weight: 4
description: > 
    This section describe examples on how to deploy Kairos with k3s and MetalLB
---

Welcome to the guide on using MetalLB with Kairos and K3s on a bare metal host!

In this tutorial, we'll walk through the steps of setting up a Kairos node on your local network using the `192.168.1.10-192.168.1.20` IP range, with MetalLB and K3s.

But first, let's talk a little bit about what [MetalLB](https://metallb.universe.tf/) and [K3s](https://k3s.io/) are. MetalLB is a load balancer implementation for bare metal Kubernetes clusters that uses standard routing protocols. It's particularly useful when used with K3s in Kairos, as it provides load balancing for bare metal clusters and helps manage IP addresses within the cluster. K3s is a lightweight Kubernetes distribution that is easy to install and maintain.

Now that you have an understanding of what we'll be working with, let's dive into the installation process.


To get started, you'll need to use the [provider-kairos](https://github.com/kairos-io/provider-kairos) artifacts, which include k3s. We'll be using the [k3s manifest method](/docs/reference/configuration#kubernetes-manifests) to deploy MetalLB.

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

There are a few things to note in this configuration file:

- In the `k3s` block, we use the `--disable` flag to disable `traefik` and `servicelb`, which are the default load balancers for k3s.
- In the `write_files` block, we write manifests (in `/var/lib/rancher/k3s/server/manifests/` see [docs](/docs/reference/configuration#kubernetes-manifests)) to deploy MetalLB and configure it to use the `192.168.1.10-192.168.1.20` IP range. Make sure to choose an IP range that doesn't interfere with your local DHCP network.

And that's it! You should now have MetalLB and K3s set up on your Kairos node.

## Resources

- [TNS blog post](https://thenewstack.io/livin-kubernetes-on-the-immutable-edge-with-kairos-project/)