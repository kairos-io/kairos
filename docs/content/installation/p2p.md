+++
title = "Full P2P mesh support"
date = 2022-02-09T17:56:26+01:00
weight = 4
chapter = false
pre = "<b>- </b>"
+++

{{% notice note %}}
This feature is crazy and experimental!
{{% /notice %}}

This section will guide on how to leverage the p2p full-mesh capabilities of c3os.

c3OS supports p2p full-mesh out of the box. That allows to seamelessly interconnect clusters and nodes from different regions into an unified overlay network,
additionally, the same network is used for co-ordinating nodes automatically, allowing self-automated node bootstrap.

A hybrid network is automatically set up between all the nodes, as such there is no need to expose them over the Internet, and either expose the Kubernetes management API outside, reducing attacker's exploiting surface.

C3os can be configured to automatically bootstrap a kubernetes cluster with the full-mesh functionalities, or just either add an additional interface to the machines to let them communicate within a new network segment.

If you are not familiar with the process, it is suggested to follow the [quickstart](/quickstart/installation) first, and the steps below in sequence.
The section below just explains the difference in the configuration options to enable p2p full-mesh during the installation phase.

## Prerequisites

- `c3os-cli`

## Configuration

In order to configure a node to join over the same p2p network during installation add a `c3os` block in the configuration, like the following:

```yaml
c3os:
  network_token: "...."
  # Optionally, set a network id (for multiple clusters in the same network)
  # network_id: "dev"
  # Optionally set a role
  # role: "master"

```

The `c3os` block is used to configure settings to the mesh functionalities. The minimum required argument is the `network_token`. 

### `network_token`

The `network_token` is a unique, shared secret which is spread over the nodes and can be generated with the `c3os-cli`. 
It will make all the node connect automatically to the same network. Every node will generate a set of private/public key keypair automatically on boot that are used to communicate securely within a e2e encrypted channel.

To generate a new network token, you can use the `c3os-cli`:

```bash
c3os generate-token
```

### `network_id`

An optional, unique identifier for the cluster. This allows to bootstrap multiple cluster over the same underlaying network.

### `role`

Force a role for the node. Available: `worker`, `master`.

For a full reference of all the supported use cases, see [cloud-init](https://rancher.github.io/elemental-toolkit/docs/reference/cloud_init/).

## Join new nodes

To join new nodes, simply re-apply the process to new nodes by specifying the same `config.yaml` for all the machines. Unless you have specified a role for each of the nodes, the configuration doesn't need any further change. The machines will connect automatically between themselves, either remotely on local network.

## Connect to the nodes

The `c3os-cli` can be used to establish a tunnel with the nodes network given a `network_token`.


```bash
sudo c3os bridge --network-token <TOKEN>
```

This command will create a tun device in your machine and will make possible to contact each node in the cluster.


{{% notice note %}}
The command requires root permissions in order to create a tun/tap device on the host
{{% /notice %}}

An API will be also available at [localhost:8080](http://localhost:8080) for inspecting the network status. 


## Get kubeconfig

To get the kubeconfig it is sufficient or either login to the master node and get it from the engine ( e.g. k3s puts it `/etc/rancher/k3s/k3s.yaml`) or using the `c3os` cli.

By using the CLI, you need to be connected to the bridge, or either logged in from one of the nodes and perform the commands in the console.

If you are using the CLI, you need to run the bridge in a separate window. 

To get the kubeconfig, run the following:

```bash
c3os get-kubeconfig > kubeconfig
```

{{% notice note %}}
`c3os bridge` acts like `kubectl proxy`. you need to keep it open to operate the kubernetes cluster and access the API.
{{% /notice %}}