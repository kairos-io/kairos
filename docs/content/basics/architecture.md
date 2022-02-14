+++
title = "Architecture"
date = 2022-02-09T17:56:26+01:00
weight = 1
pre = "<b>- </b>"
+++


C3OS comes as ISO and as a CLI which can be downloaded from the [release page](https://github.com/mudler/c3os/releases). The CLI setups `k3s` and is also used to automatically register nodes in a private, user-defined network. 

Currently **Alpine**-based and **openSUSE**-based flavors are available, the **openSUSE**-based flavor supports autonomous kubernetes bootstrapping with the `c3os` CLI.

C3OS nodes based on **openSUSE** autonomously connect and configure each other via P2P, no network setup and no central server is needed. 

Nodes can discovery each other also if they are in different networks and behind NAT.

c3OS uses [edgevpn](https://github.com/mudler/edgevpn) to coordinate, automatically discover/configure and establish a p2p vpn network between the cluster nodes.

The connection happens in 3 stages, where the discovery is driven by DHT and mDNS (which can be selectively disabled/enabled)

- Discovery
- Gossip network
- Full connectivity

![](https://mudler.github.io/edgevpn/docs/concepts/architecture/edevpn_bootstrap_hu8e61a09dccbf3a67bf1fc604ae4924fd_64246_1200x550_fit_catmullrom_3.png)

The initial installation is done with pairing via QR code. A QR code is displayed when booting from ISO, to allow deployment on situations where sending files or connecting remotely is inconvienent.

For mass-installation cloud-init can be used to drive automated installs.