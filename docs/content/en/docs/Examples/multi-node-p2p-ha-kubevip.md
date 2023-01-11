---
title: "Deploying a High-Availability K3s Cluster with KubeVIP"
linkTitle: "Deploying a High-Availability K3s Cluster with KubeVIP"
weight: 6
date: 2022-11-13
description: >
    This guide walks through the process of deploying a highly-available, P2P self-coordinated k3s cluster with KubeVIP, which provides a high available Elastic IP for the control plane. 
---

{{% alert title="Note" %}}

This feature is crazy and experimental! Do not run in production servers. 
Feedback and bug reports are welcome, as we are improving the p2p aspects of Kairos.

{{% /alert %}}

K3s is a lightweight Kubernetes distribution that is easy to install and operate. It's a great choice for small and edge deployments, but it can also be used to create a high-availability (HA) cluster with the help of KubeVIP. In this guide, we'll walk through the process of deploying a highly-available k3s cluster with KubeVIP, which provides a high available ip for the control plane.

The first step is to set up the cluster. Kairos automatically deploys an HA k3s cluster with KubeVIP to provide a high available ip for the control plane. KubeVIP allows to setup an ElasticIP that is advertized in the node's network and, as managed as a daemonset in kubernetes it is already running in HA.



The difference between this setup is that we just use the p2p network to automatically co-ordinate nodes, while the connection of the cluster is not being routed to a VPN. The p2p network is used for co-ordination, self-management, and used to add nodes on day 2.

In order to deploy this setup you need to configure the cloud-config file. You can see the example of the yaml file below. You need to configure the hostname, user and ssh_authorized_keys. You need also to configure kubevip with the elastic ip and the p2p network with the options you prefer.

```yaml
#cloud-config

hostname: kairoslab-{{ trunc 4 .MachineID }}
users:
- name: kairos
  ssh_authorized_keys:
  # Add your github user here!
  - github:mudler

kubevip:
  eip: "192.168.1.110"

p2p:
 # Disabling DHT makes co-ordination to discover nodes only in the local network
 disable_dht: true #Enabled by default

 vpn:
   create: false # defaults to true
   use: false # defaults to true
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

When configuring the `p2p` section, start by adding your desired `network_token` under the p2p configuration in the cloud-config file. To generate a network token, see [documentation](/docs/installation/p2p/#network_token).

Next, set up an Elastic IP (`kubevip.eip`) with a free IP in your network. KubeVIP will advertise this IP, so make sure to select an IP that is available for use on your network.

In the VPN configuration, the create and use options are disabled, so the VPN setup is skipped and not used to route any traffic into.