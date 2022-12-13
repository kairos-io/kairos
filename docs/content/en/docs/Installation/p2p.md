---
title: "P2P support"
linkTitle: "P2P support"
weight: 6
date: 2022-11-13
description: >
  Install Kairos with p2p support
---

{{% alert title="Note" %}}

This feature is crazy and experimental! Do not run in production servers. 
Feedback and bug reports are welcome, as we are improving the p2p aspects of Kairos.

{{% /alert %}}

Deploying Kubernetes at the Edge can be a complex and time-consuming process, especially when it comes to setting up and managing multiple clusters. To make this process easier, Kairos leverages peer-to-peer technology to automatically coordinate and create Kubernetes clusters without the need for a control management interface.

With this feature, users can simply avoid specifying any setting, or just set the number of master nodes in a HA cluster they want to create and the necessary configuration details, and Kairos will take care of the rest. The peer-to-peer technology allows the nodes in the cluster to communicate and coordinate with each other, ensuring that the clusters are set up correctly and efficiently with K3s.

This not only saves users time and effort, but also makes it easier to deploy and manage Kubernetes clusters at the Edge, allowing users to focus on running and scaling their applications.

This feature is currently experimental and can be optionally enabled by adding the following configuration to the node deployment file, if you are not familiar with the installation process, it is suggested to follow the [quickstart](/docs/getting-started):

```yaml
p2p:
  # Disabling DHT makes co-ordination to discover nodes only in the local network
 disable_dht: true #Enabled by default
 # Automatic cluster deployment configuration
 auto:
   ha:
     # Enables HA controlplane
     enable: true
     # number of HA master node (beside the one used for init) for the control-plane
     master_nodes: 2
 # network_token is the shared secret used by the nodes to co-ordinate with p2p.
 # Setting a network token implies auto.enable = true.
 # To disable, just set auto.enable = false
 network_token: "YOUR_TOKEN_GOES_HERE"


```

To enable the automatic cluster deployment with peer-to-peer technology, specify a `p2p.network_token`. To enable HA, set `p2p.auto.ha.master_nodes` to the number of wanted HA/master nodes. Additionally, the p2p block can be used to configure the VPN and other settings as needed.

With these settings used to deploy all the nodes, those will automatically communicate and coordinate with each other to deploy and manage the Kubernetes cluster without the need for a control management interface and user intervention.

## Configuration

A minimum configuraton file, that bootstraps a cluster with a simple single-master topology, can look like the following:

```yaml
#cloud-config
hostname: "kubevip-{{ trunc 4 .MachineID }}"

users:
- name: "kairos"
  passwd: "kairos"
  ssh_authorized_keys:
  - github:mudler
p2p:
 network_token: "YOUR_TOKEN_GOES_HERE"
```

The `p2p` block is used to configure settings to the mesh functionalities. The minimum required argument is the `network_token` and there is no need to configure `k3s` manually with the `k3s` block as it is already implied.

{{% alert title="Note" %}}

The `k3s` block can still be used to override other `k3s` settings, e.g. `args`.

{{% /alert %}}

The network token is a shared secret available to all the nodes of the cluster. It allows the node to co-ordinate and automatically assign roles. To generate a network token, see [documentation](/docs/installation/p2p/#network_token).

Simply applying the same configuration file to all the nodes should eventually bring one master and all the other nodes as workers. Adding nodes can be done also in a later step, which will automatically setup the node without any further configuration.

Full example:


```yaml
#cloud-config

install:
  auto: true
  device: "auto"
  reboot: true

hostname: "kubevip-{{ trunc 4 .MachineID }}"
users:
- name: "kairos"
  passwd: "kairos"
  ssh_authorized_keys:
  - github:mudler

## Sets the Elastic IP used in KubeVIP
kubevip:
  eip: "192.168.1.110"
  # Specify a manifest URL for KubeVIP. Empty uses default
  manifest_url: ""
  # Enables KubeVIP
  enable: true
  # Specifies a KubeVIP Interface
  interface: "ens18"

p2p:
 role: "" # Set an hardcoded role, optional
  # Disabling DHT makes co-ordination to discover nodes only in the local network
 disable_dht: true #Enabled by default
 # Configures a VPN for the cluster nodes
 vpn:
   create: false # defaults to true
   use: false # defaults to true
   env:
      .....
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
     # Enables HA controlplane
     enable: true
     # number of HA master node (beside the one used for init) for the control-plane
     master_nodes: 2
     # Use an External database for the HA control plane
     external_db: "external-db-string"
 # network_token is the shared secret used by the nodes to co-ordinate with p2p
 network_token: "YOUR_TOKEN_GOES_HERE"
```

In the YAML configuration example, there are several important keywords that control the behavior of the automatic cluster deployment:

| Keyword | Description |
| --- | --- |
| `p2p` | Configures the peer to peer networking of the cluster |
| `p2p.disable_dht` | Disables the distributed hash table for cluster discovery |
| `p2p.network_token` | The shared secret used by the nodes to coordinate with p2p |
| `p2p.network_id` | Optional, unique identifier for the kubernetes cluster. It allows bootstrapping of multiple cluster using the same network token |
| `p2p.role` | Force a specific role for the node of the cluster |
| `p2p.vpn` | Configures a VPN for the cluster nodes |
| `p2p.vpn.create` | Enables the creation of the VPN |
| `p2p.vpn.use` | Enables the use of the VPN for routing Kubernetes traffic of the cluster |
| `p2p.vpn.env` | Configures the environment variables used to start for the VPN |
| `p2p.vpn.auto` | Configures the automatic deployment of the cluster |
| `p2p.vpn.auto.enable` | Enables automatic node configuration for role assignment |
| `p2p.vpn.auto.ha` | Configures the high availability settings for the cluster |
| `p2p.vpn.auto.ha.enable` | Enables the high availability settings |
| `p2p.vpn.auto.ha.master_nodes` | The number of expected HA master nodes in the cluster |
| `p2p.vpn.auto.ha.external_db` | The external database used for high availability |

## Elastic IP

If deploying a cluster in a Local network, it might be preferable to disable the VPN functionalities.

We use KubeVIP to provide an elastic ip for the control plane that can be configured via a specific block:

```yaml

p2p:
 network_token: ".."
 vpn:
   # Disable VPN, so traffic is not configured with a VPN
   create: false
   use: false

## Sets the Elastic IP used in KubeVIP
kubevip:
  eip: "192.168.1.110"
  # Specify a manifest URL for KubeVIP. Empty uses default
  manifest_url: ""
  # Enables KubeVIP
  enable: true
  # Specifies a KubeVIP Interface
  interface: "ens18"
```


| Keyword | Description |
| --- | --- |
| `kubevip` | Block to configure KubeVIP for the cluster |
| `kubevip.eip` | The Elastic IP used for KubeVIP. Specifying one automatically enables KubeVIP |
| `kubevip.manifest_url` | The URL for the KubeVIP manifest |
| `kubevip.enable` | Enables KubeVIP for the cluster |
| `kubevip.interface` | The interface used for KubeVIP |

A full example, with KubeVIP and HA:

```yaml

#cloud-config

install:
  auto: true
  device: "auto"
  reboot: true

hostname: "kubevip-{{ trunc 4 .MachineID }}"
users:
- name: "kairos"
  passwd: "kairos"
  ssh_authorized_keys:
  - github:mudler

p2p:
 network_token: "..."
 ha:
   master_nodes: 2
 vpn:
   # Disable VPN, so traffic is not configured with a VPN
   create: false
   use: false

kubevip:
  eip: "192.168.1.110"
```
### `network_token`

The `network_token` is a unique, shared secret which is spread over the nodes and can be generated with the Kairos CLI.
It will make all the node connect automatically to the same network. Every node will generate a set of private/public key pair automatically on boot that are used to communicate securely within an end-to-end encryption (E2EE) channel.

To generate a new network token, you can use the Kairos CLI or Docker:
{{< tabpane text=true right=true  >}}
{{% tab header="docker" %}}
```bash
docker run -ti --rm quay.io/mudler/edgevpn -b -g
```
{{% /tab %}}
{{% tab header="CLI" %}}
```bash
kairos generate-token
```
{{% /tab %}}
{{< /tabpane >}}

## Join new nodes

To join new nodes, reapply the process to new nodes by specifying the same configuration file for all the machines. Unless you have specified a role for each of the nodes, the configuration doesn't need any further change. The machines will connect automatically between themselves, either remotely on local network.

## Connect to the nodes

The `kairos-cli` can be used to establish a tunnel with the nodes network given a `network_token`.

```bash
sudo kairos bridge --network-token <TOKEN>
```

This command will create a TUN device in your machine and will make possible to contact each node in the cluster.

{{% alert title="Note" color="info" %}}
The command requires root permissions in order to create a TUN/TAP device on the host.
{{% /alert %}}

An API will be also available at [localhost:8080](http://localhost:8080) for inspecting the network status.

## Get kubeconfig

To retrieve `kubeconfig`, it is sufficient to either log in to the master node and get it from the engine (e.g., K3s places it `/etc/rancher/k3s/k3s.yaml`) or use the Kairos CLI.

By using the CLI, you need to be connected to the bridge or logged in from one of the nodes and perform the commands in the console.

If you are using the CLI, you need to run the bridge in a separate window.

To retrieve `kubeconfig`, run the following:

```bash
kairos get-kubeconfig > kubeconfig
```

{{% alert title="Note" color="info" %}}
`kairos bridge` acts like `kubectl proxy`. You need to keep it open to operate the Kubernetes cluster and access the API.
{{% /alert %}}
