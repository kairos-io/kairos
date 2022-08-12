<h1 align="center">
  <br>
     <img src="https://user-images.githubusercontent.com/2420543/153508410-a806a385-ae3e-417e-b87e-7472f21689e3.png" width=500>
	<br>
<br>
</h1>

<h3 align="center">Create Decentralized Kubernetes clusters </h3>
<p align="center">
  <a href="https://github.com/c3os-io/c3os/issues"><img src="https://img.shields.io/github/issues/c3os-io/c3os"></a>
  <a href="https://quay.io/repository/c3os/c3os"> <img src="https://quay.io/repository/mudler/c3os/status"></a>
</p>

<p align="center">
	 <br>
    Kubernetes-focused Linux OS - Automatic Node discovery - Automatic VPN - K3s
</p>

<hr>

C3OS is a lightweight Kubernetes-focused GNU/Linux derivative built with [Elemental-toolkit](https://github.com/rancher/elemental-toolkit) that optionally supports automatic node discovery, automatic role assignment and optionally VPN out of the box with no kubernetes networking configuration required. 

C3OS can also create multi-nodes Kubernetes cluster with [k3s](https://k3s.io) that connects autonomously in a hybrid P2P mesh VPN which bridges nodes without any central server, also behind nat, or it can be just used standalone as a k3s server.

C3OS is entirely backed up by community, It's Free and Open Source, under the Apache 2.0 License. Feel free to open issues or contribute with PRs!

- No infrastructure is required. C3OS can be used to bootstrap a cluster entirely from the ground-up.
- LAN, remote networks, multi-region/zones, NAT - No network configuration or opening port outside is required. Nodes will connect each other via holepunching and using hops wherever necessary.
- Zero kubernetes configuration - Nodes autonomously discover and configure themselves to form a Kubernetes cluster. The same configuration/bootstrapping process applies wether creating new clusters or joining nodes to existing one.
- Secure P2P Remote recovery to restore failed nodes or lost credentials
- Hybrid P2P mesh between nodes (optional)

It comes in two variants, based on openSUSE and Alpine.

[Documentation available here](https://docs.c3os.io).

## Run c3os

Download the ISO or the image from the latest [releases](https://github.com/c3os-io/c3os/releases).

You can just use BalenaEtcher or just `dd` the ISO/image to the disk.

## Automated installation

Boot the ISO and follow the instructions on screen. c3os supports automatic peer discovery and [device pairing](https://docs.c3os.io/installation/device_pairing/).

## QR Code installation

By default the ISO will boot in device pairing mode, which will present a QR code that can be used to drive the installation.


<img src="https://user-images.githubusercontent.com/2420543/153488321-07e63e5f-d9e3-48ce-b551-8b457ece14a9.png" height="350">

By having the QR visible at screen, use the `c3os` CLI to register the node, for example:

```bash
$ c3os register --config config.yaml --device /dev/sda --reboot --log-level debug
# Or, with an image screenshot of the QR code:
$ c3os register --config config.yaml --device /dev/sda ~/path/to/image.png
```

Check out the [documentation](https://docs.c3os.io).

## Manual Installation

After booting, it is possible to install `c3os` manually with `c3os interactive-install`.

The default user pass is `c3os:c3os` on LiveCD mediums. See [Docs](https://docs.c3os.io/installation/manual/).

## Upgrades

Upgrades can be triggered with Kubernetes or manually with `c3os upgrade`. See [Docs](https://docs.c3os.io/after_install/upgrades/).

## Building c3os

Requirements: Needs only docker.

Run `./earthly.sh +all --FLAVOR=opensuse`, should produce a docker image along with a working ISO
