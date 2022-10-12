---
layout: "../../layouts/docs/Layout.astro"
title: "P2P Network"
index: 5
---

# P2P Network

Optionally Kairos supports full p2p connectivity.

Nodes automatically co-ordinates themselves and bootstrap from the ground-up using a distributed ledger:

![p2p](https://user-images.githubusercontent.com/2420543/195459436-236139cf-605d-4608-9018-ea80381d4e77.png)

The P2P networking is used for co-ordinating and to bootstrap nodes, however, when this functionality is enabled there is also a distributed ledger accessible over the nodes that can be programmatically be accessed and used to store metadata.

Kairos can automatically set up a VPN between the nodes by using a shared secret. This also allows the nodes to automatically coordinate, discover/configure and establish a network overlay spanning across multiple regions. [Edgevpn](https://github.com/mudler/edgevpn) is used for such purpose.

The connection happens in three stages, where the discovery is driven by DHT (distributed hash table) and (multicast DNS) mDNS (which can be selectively disabled/enabled):

- Discovery
- Gossip network
- Full connectivity

![](https://mudler.github.io/edgevpn/docs/concepts/architecture/edevpn_bootstrap_hu8e61a09dccbf3a67bf1fc604ae4924fd_64246_1200x550_fit_catmullrom_3.png)
