
# C3os

![background](https://user-images.githubusercontent.com/2420543/153506895-fb978c1e-8197-42e2-9ce2-3be6e0907acc.jpg?classes=shadow&width=50pc)


C3OS is a lightweight Kubernetes distro that supports automatic node discovery, automatic role assignment and VPN out of the box. C3OS nodes connect autonomously via P2P VPN, without any central server. 

C3OS is focused on creating private disposable distributed kubernetes clusters.

Configuration and installation is done via Decentralized Device Pairing or via cloud-init.

c3OS is:
- Immutable
- cloud-init driven
- P2P first
- Automatized in every aspect

By default cluster nodes are connected each other via a p2p VPN which will also coordinates and prepare the nodes roles automatically, transparently to the user. There is no central server needed, and nodes will try to automatically connect each other by holepunching, snatting, and creating intermediate hops as necessary.

c3OS is composed of:
- [k3s](https://k3s.io) as a Kubernetes distribution
- [edgevpn](https://mudler.github.io/edgevpn) as fabric for the distributed network, node coordination and bootstrap
- [cOS-toolkit](https://rancher-sandbox.github.io/cos-toolkit-docs/docs/) as a fundament to build the Linux derivative. Indeed, any `cOS` docs applies to `c3os` as well.

