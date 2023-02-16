---
title: "P2P Network"
linkTitle: "P2P Network"
weight: 5
date: 2023-02-15
description: >
    How Kairos leverage Peer-to-peer (P2P) to self-coordinate clusters at the edge.
---

## Introduction

As more organizations seek to take advantage of the benefits of Kubernetes for their edge applications, the difficulties of managing large-scale clusters become apparent. Managing, configuring, and coordinating multiple clusters can be a complex and time-consuming process. We need solutions that offer zero-touch configuration and self-coordination.

To address these challenges, Kairos provides an easy and robust solution for deploying Kubernetes workloads at the edge. By utilizing peer-to-peer (p2p) technology, Kairos can automatically coordinate and create Kubernetes clusters without requiring a control management interface. This frees users up to concentrate on running and scaling their applications instead of spending time on cluster management.

In this document, we will examine the advantages of using Kairos to deploy Kubernetes clusters at the edge, and how p2p technology facilitates self-coordination for a zero-touch configuration experience. We will also explore how Kairos' highly adaptable and container-based approach, combined with an immutable OS and meta-distribution, makes it an excellent choice for edge deployments.

## Overview: P2P for self-coordination

<img align="right" width="200" src="https://user-images.githubusercontent.com/2420543/219048504-986da0e9-aca3-4c9e-b980-ba2a6dc03bf7.png">

Kairos creates self-coordinated, fully meshed clusters at the edge by using a combination of P2P technology, VPN, and Kubernetes. 

This design is made up of several components:

- The Kairos base OS with support for different distribution flavors and k3s combinations (see our support matrix [here](/docs/reference/image_matrix)).
- A Virtual private network interface ([EdgeVPN](https://github.com/mudler/edgevpn) which leverages [libp2p](https://github.com/libp2p/go-libp2p)).
- K3s/CNI configured to work with the VPN interface.
- A shared ledger accessible to all nodes in the p2p private network.

By using libp2p as the transport layer, Kairos can abstract connections between the nodes and use it as a coordination mechanism. The shared ledger serves as a cache to store additional data, such as node tokens to join nodes to the cluster or the cluster topology, and is accessible to all nodes in the P2P private network. The VPN interface is automatically configured and self-coordinated, requiring zero-configuration and no user intervention.

Moreover, any application at the OS level can use P2P functionalities by using Virtual IPs within the VPN. The user only needs to provide a generated shared token containing OTP seeds for rendezvous points used during connection bootstrapping between the peers. It's worth noting that the VPN is optional, and the shared ledger can be used to coordinate and set up other forms of networking between the cluster nodes, such as KubeVIP. (See this example [here](/docs/examples/multi-node-p2p-ha-kubevip))

## Implementation

Peer-to-peer (P2P) networking is used to coordinate and bootstrap nodes. When this functionality is enabled, there is a distributed ledger accessible over the nodes that can be programmatically accessed and used to store metadata.

Kairos can automatically set up a VPN between nodes using a shared secret. This enables the nodes to automatically coordinate, discover, configure, and establish a network overlay spanning across multiple regions. [EdgeVPN](https://github.com/mudler/edgevpn) is used for this purpose.

The private network is bootstrapped in three phases, with discovery driven by a distributed hash table (DHT) and multicast DNS (mDNS), which can be selectively disabled or enabled. The three phases are:

1. Discovery
1. Gossip network
1. Full connectivity

During the discovery phase, which can occur via mDNS (for LAN) or DHT (for WAN), nodes discover each other by broadcasting their presence to the network.

In the second phase, rendezvous points are rotated by OTP (one-time password). A shared token containing OTP seeds is used to generate these rendezvous points, which serve as a secure way to bootstrap connections between nodes. This is essential for establishing a secure and self-coordinated P2P network.

In the third phase, a gossip network is formed among nodes, which shares shared ledger blocks symmetrically encrypted with AES. The key used to encrypt these blocks is rotated via OTP. This ensures that the shared ledger is secure and that each node has access to the most up-to-date version of the shared configuration. The ledger is used to store arbitrary metadata from the nodes of the network. On each update, a new block is created with the new information and propagated via gossip.

Optionally, full connectivity can be established by bringing up a TUN interface, which routes packets via the libp2p network. This enables any application at the OS level to leverage P2P functionalities by using VirtualIPs accessible within the VPN.

The coordination process in Kairos is designed to be resilient and self-coordinated, with no need for complex network configurations or control management interfaces. By using this approach, Kairos simplifies the process of deploying and managing Kubernetes clusters at the edge, making it easy for users to focus on running and scaling their applications.

<p align="center">
<img src="https://mudler.github.io/edgevpn/docs/concepts/architecture/edevpn_bootstrap_hu8e61a09dccbf3a67bf1fc604ae4924fd_64246_1200x550_fit_catmullrom_3.png">
</p>

### Why Peer-to-Peer?

Kairos has chosen Peer-to-Peer as an internal component to enable automatic coordination of Kairos nodes. To understand why [EdgeVPN](https://github.com/mudler/edgevpn) has been selected, see the comparison table below, which compares EdgeVPN with other popular VPN solutions:

|      | Wireguard | OpenVPN     | EdgeVPN                                            |
|------|-----------|-------------|----------------------------------------------------|
| Memory Space | Kernel-module | Userspace   | Userspace                                          |
| Protocol     | UDP         | UDP, TCP    | TCP, UDP/quick, UDP, ws, everything supported by libp2p |
| P2P          | Yes         | Yes         | Yes                                                |
| Fully meshed | No          | No          | Yes                                                |
| Management Server (SPOF) | Yes         | Yes         | No                                                 |
| Self-coordinated         | No          | No          | Yes                                                |

Key factors, such as self-coordination and the ability to share metadata between nodes, have led to the selection of EdgeVPN. However, there are tradeoffs and considerations to note in the current architecture, such as:

- Routing all traffic to a VPN can introduce additional latency
- Gossip protocols can be chatty, especially if using DHT, creating VPNs that span across regions
- EdgeVPN is in user-space, which can be slower compared to kernel-space solutions such as Wireguard
- For highly trafficked environments, there will be an increase in CPU usage due to the additional encryption layers introduced by EdgeVPN

Nonetheless, these tradeoffs can be overcome, and new features can be added due to EdgeVPN's design. For example:

- There is no need for any server to handle traffic (no SPOF), and no additional configuration is necessary
- The p2p layer is decentralized and can span across different networks by using DHT and a bootstrap server
- Self-coordination simplifies the provisioning experience
- Internal cluster traffic can also be offloaded to other mechanisms if network performance is a prerequisite
- For instance, with [KubeVIP](/docs/examples/multi-node-p2p-ha-kubevip), new nodes can join the network and become cluster members even after the cluster provisioning phase, making EdgeVPN a scalable solution.

### Why a VPN ?

A VPN allows for the configuration of a Kubernetes cluster without depending on the underlying network configuration. This design model is popular in certain use cases at the edge where fixed IPs are not a viable solution. We can summarize the implications as follows:

|          | K8s Without VPN     | K8s With VPN                                            |
|----------|---------------------|----------------------------------------------------------|
| IP management    | Needs to have static IP assigned by DHCP or manually configured (can be automated) | Automatically coordinated Virtual IPs for nodes. Or manually assign them |
| Network Configuration | `etcd` needs to be configured with IPs assigned by your network/fixed | Automatically assigned, fixed VirtualIPs for `etcd`. |
| Networking | Cluster IPs, and networking is handled by CNIs natively (no layers) | Kubernetes Network services will have Cluster IPs sitting below the VPN. <br> Every internal kubernetes communication goes through VPN. <br> The additional e2e encrypted network layer might add additional latency, 0-1ms in LAN.|

The use of a VPN for a Kubernetes cluster has significant implications. With a VPN, IP management is automatic and does not require static IP addresses assigned by DHCP or manually configured. Nodes can be assigned virtual IPs that are automatically coordinated or manually assigned, which eliminates the need for manual configuration of IP addresses. Additionally, EdgeVPN implements distributed DHCP, so there are no Single point of Failures.

Additionally, network configuration is simplified with a VPN. Without a VPN, `etcd` needs to be configured with IPs assigned by your network or fixed. With a VPN, virtual IPs are automatically assigned for `etcd`.

In terms of networking, a Kubernetes cluster without a VPN handles cluster IPs and networking natively without additional layers. However, with a VPN, Kubernetes network services will have Cluster IPs below the VPN. This means that all internal Kubernetes communication goes through the VPN. While the additional end-to-end encrypted network layer might add some latency, it is observed typically to be only 0-1ms in LAN. However, due to the Encryption layers, the CPU usage might be high if used for high-demanding traffic.

It's also worth noting that while a VPN provides a unified network environment, it may not be necessary or appropriate for all use cases. Users can choose to opt-out of using the VPN and leverage only the coordination aspect, for example, with KubeVIP. Ultimately, the decision to use a VPN should be based on the specific needs and requirements of your Kubernetes cluster, and as such you can just use the co-ordination aspect and leverage for instance [KubeVIP](/docs/examples/multi-node-p2p-ha-kubevip).

### Packet flow

The Virtual Private Network used is [EdgeVPN](https://github.com/mudler/edgevpn), which leverages [libp2p](https://github.com/libp2p/go-libp2p) for the transport layer.

To explain how the packet flow works between two nodes, Node A and Node B, refer to the diagram below:

<p align="center">
<img src="https://user-images.githubusercontent.com/2420543/219048445-300de7e8-428f-4ded-848d-bf73c56acca1.png">
</p>

While participating actively on a network, each node keeps the shared ledger up-to-date with information about itself and how to be reached by advertizing its own IP and the libp2p identity, allowing nodes to discover each other and how to route packets.

Assuming that we want to establish an SSH connection from Node A to Node B through the VPN network, which exposes the `sshd` service, the process is as follows:

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
<img src="https://user-images.githubusercontent.com/2420543/195459436-236139cf-605d-4608-9018-ea80381d4e77.png">
</p>

The use of p2p technology to enable self-coordination of Kubernetes clusters in Kairos offers a number of benefits:

1. **Simplified deployment**: Deploying Kubernetes clusters at the edge is greatly simplified. Users don’t need to specify any network settings or use a control management interface to set up and manage their clusters.
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

