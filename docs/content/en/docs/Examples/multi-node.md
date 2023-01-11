---
title: "Multi Node k3s cluster"
linkTitle: "Multi node k3s cluster"
weight: 1
description: > 
    This section describe examples on how to deploy Kairos with k3s as a multi-node cluster
---

In the example below we will use a bare metal host to provision a Kairos cluster in the local network with K3s and one master node.

## Installation

Use the [provider-kairos](https://github.com/kairos-io/provider-kairos) artifacts which contains `k3s`.

Follow the [Installation](/docs/installation) documentation, and use the following cloud config file with Kairos for the master and worker:

{{< tabpane text=true right=true  >}}
{{% tab header="server" %}}
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
{{% /tab %}}
{{% tab header="worker" %}}
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

k3s-agent:
  enabled: true
  env:
   K3S_TOKEN: ...
   K3S_URL: ...
```
{{% /tab %}}
{{< /tabpane >}}

Deploy first the server; the value to use for `K3S_TOKEN` in the worker is stored at /var/lib/rancher/k3s/server/node-token on your server node.

Notably:

- we use the `k3s` block to disable `traefik` and `servicelb` (the default `k3s` load balancer)
- You can add additional configuration as args to k3s here, see [k3s](https://docs.k3s.io/reference/server-config#listeners) documentation
