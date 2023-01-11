---
title: "P2P multi-node cluster"
linkTitle: "P2P multi-node cluster"
weight: 6
date: 2022-11-13
description: >
  Install Kairos with p2p support, on a multi-node cluster
---

{{% alert title="Note" %}}

This feature is crazy and experimental! Do not run in production servers. 
Feedback and bug reports are welcome, as we are improving the p2p aspects of Kairos.

{{% /alert %}}

A multi-node scenario with non-HA is the default peer-to-peer (P2P) configuration in Kairos. To set this up, you will need to configure the `network_token` under the `p2p` configuration in your cloud-config file. Once you have set this, Kairos will handle the configuration of each node.

Consider the following example, which uses cloud-config to automatically configure the cluster:


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

```

To set up a multi-node P2P scenario with non-HA in Kairos, start by adding your desired `network_token` under the p2p configuration in the cloud-config file. To generate a network token, see [documentation](/docs/installation/p2p/#network_token).

Be sure to set `disable_dht` to true. This will ensure that coordination to discover nodes only happens on the local network.

Once you done with the above step, you can also customize the hostname to your liking by modifying the `hostname` field, adding your github user to the `ssh_authorized_keys` field, and adding any other necessary configurations.