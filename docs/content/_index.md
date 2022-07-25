![background](https://user-images.githubusercontent.com/2420543/153506895-fb978c1e-8197-42e2-9ce2-3be6e0907acc.jpg?classes=shadow&width=50pc)

# C3os

C3OS is a lightweight Kubernetes-focused GNU/Linux derivative built with [Elemental-toolkit](https://github.com/rancher/elemental-toolkit) that optionally supports automatic node discovery, automatic role assignment and optionally VPN out of the box with no Kubernetes networking configuration required. 

C3OS can also create multi-nodes Kubernetes cluster with [k3s](https://k3s.io) that connects autonomously in a hybrid P2P mesh VPN which bridges nodes without any central server, also behind nat, or it can be just used standalone as a k3s server.

C3OS is entirely backed up by community, It's Free and Open Source, under the Apache 2.0 License. Feel free to open issues or contribute with PRs!

- No infrastructure is required. C3OS can be used to bootstrap a cluster entirely from the ground-up.
- LAN, remote networks, multi-region/zones, NAT - No network configuration or opening port outside is required. Nodes will connect each other via holepunching and using hops wherever necessary.
- Zero Kubernetes configuration - Nodes autonomously discover and configure themselves to form a Kubernetes cluster. The same configuration/bootstrapping process applies wether creating new clusters or joining nodes to existing one.
- Secure P2P Remote recovery to restore failed nodes or lost credentials
- Hybrid P2P mesh between nodes (optional)

It comes in two variants, based on openSUSE and Alpine.
  
Configuration and installation is done via **Decentralized Device Pairing**, **cloud-init** for manual/automated mass-installs or interactively.

c3OS have:
- an Immutable layout
- cloud-init support
- P2P hybrid layer (optional, which can be disabled)
- Strong emphasis on automation - the only configuration which is required is to generate a network token (optional)
- Embedded cluster DNS (optional)

c3OS is composed of:
- [k3s](https://k3s.io) as a Kubernetes distribution
- [edgevpn](https://mudler.github.io/edgevpn) as fabric for the distributed network, node coordination and bootstrap. Provides also embedded DNS capabilities for the cluster.
- [element-toolkit](https://rancher.github.io/elemental-toolkit/docs/) as a fundament to build the Linux derivative. Indeed, any `Elemental` docs applies to `c3os` as well.
- [nohang](https://github.com/hakavlad/nohang) A sophisticated low memory handler for Linux 
