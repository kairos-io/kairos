---
title: "High Availability K3s deployments"
linkTitle: "HA with K3s"
weight: 3
description: > 
    This section contains instructions how to deploy Kairos with a High Available control-plane for K3s 
---

Please refer to the [k3s HA](https://docs.k3s.io/installation/ha-embedded) documentation. 

This document describes how to configure Kairos with `k3s` by following the same documentation outline, to show how to apply `k3s` configuration to `Kairos`. It is implied that you are using a Kairos version with `k3s` included.

## New cluster

To run Kairos and k3s in this mode, you must have an odd number of server nodes. K3s documentation recommends starting with three nodes.

To get started, first launch a server node with the cluster-init flag added in `k3s.args` to enable clustering. A token here can be specified, and will be used as a shared secret to join additional servers to the cluster. Note, if you don't provide one, a token will be generated automatically on your behalf and available at `/var/lib/rancher/k3s/server/node-token`.

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
  - --cluster-init
  # Token will be generated if not specified at /var/lib/rancher/k3s/server/node-token
  env:
    K3S_TOKEN: "TOKEN_GOES_HERE"
```

After launching the first server, join the other servers to the cluster using the shared secret (`K3S_TOKEN`):

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
  - --server https://<ip or hostname of server1>:6443
  env:
    K3S_TOKEN: "TOKEN_GOES_HERE"
```

Now you have a highly available control plane. Any successfully clustered servers can be used in the `--server` argument to join additional server and worker nodes. 

### Joining a worker

Joining additional worker nodes to the cluster follows the same procedure as a single server cluster.

To join a worker when deploying a Kairos node, use the `k3s-agent` block:

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
    K3S_TOKEN: "TOKEN_GOES_HERE"
    K3S_URL: "https://<ip or hostname of server1>:6443"
```

## External DB

K3s requires two or more server nodes for this HA configuration. See the [K3s requirements guide](https://docs.k3s.io/installation/requirements) for minimum machine requirements.

When running the k3s as a server, you must set the datastore-endpoint parameter so that K3s knows how to connect to the external datastore. 

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
  - --datastore-endpoint mysql://username:password@tcp(hostname:3306)/database-name
  # Token will be generated if not specified at /var/lib/rancher/k3s/server/node-token
  env:
    K3S_TOKEN: "TOKEN_GOES_HERE"
```
## Resources

- [High Availability with Embedded DB](https://docs.k3s.io/installation/ha-embedded)
- [High Availability with External DB](https://docs.k3s.io/installation/ha)