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

This section will guide you on how to leverage the peer-to-peer (P2P), full-mesh capabilities of Kairos.

Kairos supports P2P full-mesh out of the box. That allows to seamlessly interconnect clusters and nodes from different regions into a unified overlay network. Additionally, the same network is used for coordinating nodes automatically allowing self-automated, node bootstrap.

A hybrid network is automatically set up between all the nodes, so there is no need to expose them over the Internet or expose the Kubernetes management API outside, reducing the attacker's exploiting surface.

Kairos can be configured to automatically bootstrap a Kubernetes cluster with the full-mesh functionalities, or it can include an additional interface to the machines to let them communicate within a new network segment.

If you are not familiar with the process, it is suggested to follow the [quickstart](/docs/getting-started) first and the steps below, in sequence.

The section below explains the difference in the configuration options to enable P2P full-mesh during the installation phase.

## Prerequisites

- Kairos CLI

## Configuration

To configure a node to join over the same P2P network during installation, add a `kairos` block in the configuration, like the following:

```yaml
kairos:
  network_token: "...."
  # Optionally, set a network id (for multiple clusters in the same network)
  # network_id: "dev"
  # Optionally set a role
  # role: "master"
```

The `kairos` block is used to configure settings to the mesh functionalities. The minimum required argument is the `network_token`.

### `network_token`

The `network_token` is a unique, shared secret which is spread over the nodes and can be generated with the Kairos CLI.
It will make all the node connect automatically to the same network. Every node will generate a set of private/public key pair automatically on boot that are used to communicate securely within an end-to-end encryption (E2EE) channel.

To generate a new network token, you can use the Kairos CLI:

```bash
kairos generate-token
```

### `network_id`

This is an optional, unique identifier for the cluster. It allows bootstrapping of multiple cluster over the same underlying network.

### `role`

Force a role for the node. Available: `worker`, `master`.


## Join new nodes

To join new nodes, reapply the process to new nodes by specifying the same `config.yaml` for all the machines. Unless you have specified a role for each of the nodes, the configuration doesn't need any further change. The machines will connect automatically between themselves, either remotely on local network.

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
