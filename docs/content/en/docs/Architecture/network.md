---
title: "P2P Network"
linkTitle: "P2P Network"
weight: 5
date: 2023-02-15
description: >
    How Kairos leverage Peer-to-peer (P2P) to self-coordinate clusters at the edge.
---

## Introduction

As more organizations seek to take advantage of the benefits of Kubernetes for their edge applications, the difficulties of managing large-scale clusters become apparent. Managing, configuring, and coordinating multiple clusters can be a complex and time-consuming process, especially when zero-touch configuration and self-coordination are necessary.

To address these challenges, Kairos provides an easy and robust solution for deploying Kubernetes workloads at the edge. By utilizing peer-to-peer (p2p) technology, Kairos can automatically coordinate and create Kubernetes clusters without requiring a control management interface. This frees users to concentrate on running and scaling their applications instead of spending time on cluster management.

In this document, we will examine the advantages of using Kairos to deploy Kubernetes clusters at the edge, and how p2p technology facilitates self-coordination for a zero-touch configuration experience. We will also explore how Kairos' highly adaptable and container-based approach, combined with an immutable OS and meta-distribution, makes it an excellent choice for edge deployments.

## Overview: P2P for self-coordination

<img align="right" width="200" src="https://user-images.githubusercontent.com/2420543/219048504-986da0e9-aca3-4c9e-b980-ba2a6dc03bf7.png">

Kairos creates self-coordinated fully meshed clusters at the edge by using a combination of P2P technology, VPN, and Kubernetes. 

This design is made up of several components:

- the Kairos base OS with support for different distribution flavors and k3s combinations (see our support matrix [here](/docs/reference/image_matrix)).
- a Virtual private network interface ([EdgeVPN](https://github.com/mudler/edgevpn) which leverages [libp2p](https://github.com/libp2p/go-libp2p))
- K3s/CNI configured to work with the VPN interface
- a shared ledger accessible to all nodes in the p2p private network

By using libp2p as the transport layer, Kairos can abstract connections between the nodes and use it as a coordination mechanism. The shared ledger serves as a cache to store additional data, such as node tokens to join nodes to the cluster or the cluster topology, and is accessible to all nodes in the P2P private network. The VPN interface is automatically configured and self-coordinated, requiring zero-configuration and no user intervention.

Moreover, any application at the OS level can use P2P functionalities by using Virtual IPs within the VPN. The user only needs to provide a generated shared token containing OTP seeds for rendezvous points used during connection bootstrapping between the peers. It's worth noting that the VPN is optional, and the shared ledger can be used to coordinate and set up other forms of networking between the cluster nodes, such as KubeVIP. (See this example [here](/docs/examples/multi-node-p2p-ha-kubevip))

## Implementation

Peer-to-peer (P2P) networking is used to coordinate and bootstrap nodes. When this functionality is enabled, there is a distributed ledger accessible over the nodes that can be programmatically accessed and used to store metadata.

Kairos can automatically set up a VPN between nodes using a shared secret. This enables the nodes to automatically coordinate, discover, configure, and establish a network overlay spanning across multiple regions. [EdgeVPN](https://github.com/mudler/edgevpn) is used for this purpose.

The private network is bootstrapped in three phases, with discovery driven by a distributed hash table (DHT) and multicast DNS (mDNS), which can be selectively disabled or enabled. The three phases are:

1. Discovery
1. Gossip network
1. Full connectivity

Kairos uses a three-phase coordination process to create and manage Kubernetes clusters at the edge. The first phase is the discovery phase, which can occur via mDNS (for LAN) or DHT (for WAN). During this phase, nodes discover each other by broadcasting their presence to the network.

In the second phase, rendezvous points are rotated by OTP (one-time password). A shared token containing OTP seeds is used to generate these rendezvous points, which serve as a secure way to bootstrap connections between nodes. This is essential for establishing a secure and self-coordinated P2P network.

In the third phase, a gossip network is formed among nodes, which shares shared ledger blocks symmetrically encrypted with AES. The key used to encrypt these blocks is rotated via OTP. This ensures that the shared ledger is secure and that each node has access to the most up-to-date version of the shared configuration. The ledger is used to store arbitrary metadata from the nodes of the network. On each update, a new block is created with the new information and propagated via gossip.

Optionally, full connectivity can be established by bringing up a TUN interface, which routes packets via the libp2p network. This enables any application at the OS level to leverage P2P functionalities by using VirtualIPs accessible within the VPN.

The coordination process in Kairos is designed to be resilient and self-coordinated, with no need for complex network configurations or control management interfaces. By using this approach, Kairos simplifies the process of deploying and managing Kubernetes clusters at the edge, making it easy for users to focus on running and scaling their applications.

<p align="center">
<img width="700" src="https://mudler.github.io/edgevpn/docs/concepts/architecture/edevpn_bootstrap_hu8e61a09dccbf3a67bf1fc604ae4924fd_64246_1200x550_fit_catmullrom_3.png">
</p>

### Packet flow

The Virtual Private Network used is [EdgeVPN](https://github.com/mudler/edgevpn), which leverages [libp2p](https://github.com/libp2p/go-libp2p) for the transport layer.

To explain how the packet flow works between two nodes, Node A and Node B, refer to the diagram below:

<p align="center">
<img width="700" src="https://user-images.githubusercontent.com/2420543/219048445-300de7e8-428f-4ded-848d-bf73c56acca1.png">
</p>

While partecipating actively to a network, each node keeps the shared ledger up-to-date with information about itself and how to be reachable by advertizing its own IP and the libp2p identity, allowing nodes to discover each other and know how to route packets. Assuming that we want to establish an SSH connection from Node A to Node B through the VPN network, which exposes the `sshd` service, the process is as follows:

1. Node A (`10.1.0.1`) uses `ssh` to dial the VirtualIP of the Node B (`10.1.0.2`) in the network.
2. EdgeVPN reads the frame from the TUN interface.
3. If EdgeVPN finds a match in the ledger between the VirtualIP and an associated Identity, it opens a p2p stream to Node B using the libp2p Identity.
4. Node B receives the incoming p2p stream from EdgeVPN.
5. Node B performs a lookup in the shared ledger.
6. If a match is found, Node B routes the packet back to the TUN interface, up to the application level.

### Controller

A set of Kubernetes Native Extensions ([Entangle](/docs/reference/entangle)) provides peer-to-peer functionalities also to existing clusters by allowing to bridge connection with the same design architecture described above.

It can be used to:

- Bridge services between clusters
- Bridge external connections to cluster
- Setup EdgeVPN as a daemonset between cluster nodes

See also the Entangle [documentation](/docs/reference/entangle) to learn more about it.

## Benefits

<p align="center">
<img width="700" src="https://user-images.githubusercontent.com/2420543/195459436-236139cf-605d-4608-9018-ea80381d4e77.png">
</p>

The use of p2p technology to enable self-coordination of Kubernetes clusters in Kairos offers a number of benefits:

1. **Simplified deployment**: With Kairos, deploying Kubernetes clusters at the edge is greatly simplified. Users don’t need to specify any network settings or use a control management interface to set up and manage their clusters.
1. **Easy customization**: Kairos offers a highly customizable approach to deploying Kubernetes clusters at the edge. Users can choose from a range of meta distributions, including openSUSE, Ubuntu, Alpine and [many others](/docs/reference/image_matrix), and customize the configuration of their clusters as needed.
1. **Automatic coordination**: With Kairos, the coordination of Kubernetes clusters is completely automated. The p2p network is used as a coordination mechanism for the nodes, allowing them to communicate and coordinate with each other without the need for any external management interface. This means that users can set up and manage their Kubernetes clusters at the edge with minimal effort, freeing up their time to focus on other tasks.
1. **Secure and replicated**: The use of rendezvous points and a shared ledger, encrypted with AES and rotated via OTP, ensures that the p2p network is secure and resilient. This is especially important when deploying Kubernetes clusters at the edge, where network conditions can be unpredictable.
1. **Resilient**: Kairos ensures that the cluster remains resilient, even in the face of network disruptions or failures. By using VirtualIPs, nodes can communicate with each other without the need for static IPs, and the cluster's etcd database remains unaffected by any disruptions.
1. **Scalable**: Kairos is designed to be highly scalable. With the use of p2p technology, users can easily add or remove nodes from the cluster, without the need for any external management interface.

By leveraging p2p technology, Kairos makes it easy for users to deploy and manage their clusters without the need for complex network configurations or external management interfaces. The cluster remains secure, resilient, and scalable, ensuring that it can handle the challenges of deploying Kubernetes at the edge.

## Conclusions

In conclusion, Kairos offers an innovative approach to deploying and managing Kubernetes clusters at the edge. By leveraging peer-to-peer technology, Kairos eliminates the need for a control management interface and enables self-coordination of clusters. This makes it easier to deploy and manage Kubernetes clusters at the edge, saving users time and effort.

The use of libp2p, shared ledger, and OTP for bootstrapping and coordination thanks to [EdgeVPN](https://github.com/mudler/edgevpn) make the solution secure and resilient. Additionally, the use of VirtualIPs and the option to establish a TUN interface ensures that the solution is flexible and can be adapted to a variety of network configurations without requiring exotic configurations.

With Kairos, users can boost large-scale Kubernetes adoption at the edge, achieve zero-touch configuration, and have their cluster's lifecycle completely managed, all while enjoying the benefits of self-coordination and zero network configuration. This allows users to focus on running and scaling their applications, rather than worrying about the complexities of managing their Kubernetes clusters.

