![background](https://user-images.githubusercontent.com/2420543/153506895-fb978c1e-8197-42e2-9ce2-3be6e0907acc.jpg?classes=shadow&width=50pc)

# C3os

C3OS is a lightweight Kubernetes GNU/Linux distro that supports automatic node discovery, automatic role assignment and VPN out of the box with no kubernetes networking configuration required. 

C3OS creates multi-nodes Kubernetes cluster with [k3s](https://k3s.io) that connects autonomously in a hybrid P2P VPN which bridges nodes without any central server also behind nat.

- No infrastructure is required. C3OS can be used to bootstrap a cluster entirely from the ground-up.
- LAN, remote networks, multi-region/zones, NAT - No network configuration or opening port outside is required. Nodes will connect each other via holepunching and using hops wherever necessary.
- Zero kubernetes configuration - Nodes autonomously discover and configure themselves to form a Kubernetes cluster. The same configuration/bootstrapping process applies wether creating new clusters or joining nodes to existing one.
  
Configuration and installation is done via **Decentralized Device Pairing** or **cloud-init** for manual/automated mass-installs.

c3OS have:
- an Immutable layout
- cloud-init support
- P2P layer (which can be disabled)
- Strong enphasis on automation - the only configuration which is required is to generate a network token
- Embedded cluster DNS (optional)

c3OS is composed of:
- [k3s](https://k3s.io) as a Kubernetes distribution
- [edgevpn](https://mudler.github.io/edgevpn) as fabric for the distributed network, node coordination and bootstrap. Provides also embedded DNS capabilities for the cluster.
- [cOS-toolkit](https://rancher-sandbox.github.io/cos-toolkit-docs/docs/) as a fundament to build the Linux derivative. Indeed, any `cOS` docs applies to `c3os` as well.
- [nohang](https://github.com/hakavlad/nohang) A sophisticated low memory handler for Linux 