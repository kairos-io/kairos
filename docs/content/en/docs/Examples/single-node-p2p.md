---
title: "P2P single-node cluster"
linkTitle: "P2P single-node cluster"
weight: 6
date: 2022-11-13
description: >
   This documentation page provides instructions on how to install Kairos with P2P support on a single-node cluster
---

{{% alert title="Note" color="warning" %}}

This feature is crazy and experimental! Do not run in production servers. 
Feedback and bug reports are welcome, as we are improving the p2p aspects of Kairos.

{{% /alert %}}

Installing Kairos with P2P support on a single-node cluster requires a few specific steps. To begin, it's important to note that in a single-node scenario, the role must be enforced to a specific role. In a non-HA (high availability) setup, that role can be either `master` or `worker`. In a single-node cluster, there will be only one master node that needs to be configured explicitly.

To set up a single-node cluster over P2P, consider the following example, which uses cloud-config to automatically configure the cluster:

```yaml
#cloud-config

hostname: kairoslab-{{ trunc 4 .MachineID }}
users:
- name: kairos
  ssh_authorized_keys:
  # Add your github user here!
  - github:mudler

p2p:
 role: "master"
 # Disabling DHT makes co-ordination to discover nodes only in the local network
 disable_dht: true #Enabled by default

 # network_token is the shared secret used by the nodes to co-ordinate with p2p.
 # Setting a network token implies auto.enable = true.
 # To disable, just set auto.enable = false
 network_token: ""

```

{{% alert title="Note" %}}

One important note is that this example requires the YAML format when editing the configuration file, and that the indentation needs to be accurate, otherwise the configuration will fail.

{{% /alert %}}

The above cloud-config configures the hostname, creates a new user `kairos`, and sets the `role` to `master`. Additionally, it disables DHT (distributed hash table) to make the VPN functional only within the local network and use *mDNS* for discovery. If you wish to make the VPN work across different networks, you can set `disable_dht` to `false` or unset it.

The `network_token` field is a shared secret used by the nodes to coordinate with P2P. Setting a network token implies `auto.enable`. If you wish to disable it, simply set `auto.enable` to false. To generate a network token, see [documentation](/docs/installation/p2p/#network_token).

Keep in mind that, this example is a minimal configuration, and you can add more options depending on your needs. The above configuration can be used as a starting point and can be customized further.

