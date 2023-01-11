---
title: "Configuring Automatic High Availability in Kairos"
linkTitle: "Configuring Automatic High Availability in Kairos"
weight: 6
date: 2022-11-13
description: >
  Kairos makes it easy to configure automatic High Availability (HA) in your cluster by using cloud-config. With just a few simple steps, you can have a fully-functioning HA setup in your cluster.
---

{{% alert title="Note" %}}

This feature is crazy and experimental! Do not run in production servers. 
Feedback and bug reports are welcome, as we are improving the p2p aspects of Kairos.

{{% /alert %}}

To enable automatic HA rollout, enable the `p2p.auto.ha.enable` option in your cloud-config, and set up a number of `master_nodes`. The number of `master_nodes` is the number of additional masters in addition to the initial HA role. There will always be a minimum of 1 master, which is already taken into account. For example, setting up `master_nodes` to two will result in a total of 3 master nodes in your cluster.

To make this process even easier, Kairos automatically configures each node in the cluster from a unique cloud-config. This way, you don't have to manually configure each node, but provide instead a config file for all of the machines during [Installation](/docs/installation).

Here is an example of what your cloud-config might look like:
```yaml
#cloud-config

hostname: kairoslab-{{ trunc 4 .MachineID }}
users:
- name: kairos
  ssh_authorized_keys:
  # Replace with your github user and un-comment the line below:
  # - github:mudler

p2p:
 # Disabling DHT makes co-ordination to discover nodes only in the local network
 disable_dht: true #Enabled by default

 # network_token is the shared secret used by the nodes to co-ordinate with p2p.
 # Setting a network token implies auto.enable = true.
 # To disable, just set auto.enable = false
 network_token: ""

 # Automatic cluster deployment configuration
 auto:
   # Enables Automatic node configuration (self-coordination)
   # for role assignment
   enable: true
   # HA enables automatic HA roles assignment.
   # A master cluster init is always required,
   # Any additional master_node is configured as part of the 
   # HA control plane.
   # If auto is disabled, HA has no effect.
   ha:
     # Enables HA control-plane
     enable: true
     # Number of HA additional master nodes.
     # A master node is always required for creating the cluster and is implied.
     # The setting below adds 2 additional master nodes, for a total of 3.
     master_nodes: 2
```

Note: In order for the automatic HA rollout to work, you need to generate a network token. You can find more information on how to do this in the [dedicated section](/docs/installation/p2p/#network_token).

