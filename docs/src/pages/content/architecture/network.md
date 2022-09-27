---
layout: "../../../layouts/docs/Layout.astro"
title: "P2P Network"
index: 5
---

Kairos can automatically setup a VPN between the nodes using [edgevpn](https://github.com/mudler/edgevpn). This also allows the nodes to automatically coordinate, discover/configure and establish a network overlay spanning across multiple regions.

The connection happens in 3 stages, where the discovery is driven by DHT and mDNS (which can be selectively disabled/enabled)

- Discovery
- Gossip network
- Full connectivity

![](https://mudler.github.io/edgevpn/docs/concepts/architecture/edevpn_bootstrap_hu8e61a09dccbf3a67bf1fc604ae4924fd_64246_1200x550_fit_catmullrom_3.png)
