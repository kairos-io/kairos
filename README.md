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
    Automatic Node discovery - Automatic VPN - K3s
</p>

<hr>


C3OS is a lightweight Kubernetes GNU/Linux [cOS](https://github.com/rancher-sandbox/cOS-toolkit) derivative that supports automatic node discovery, automatic role assignment and VPN out of the box with no kubernetes networking configuration required. 

C3OS creates multi-nodes Kubernetes cluster with [k3s](https://k3s.io) that connects autonomously in a hybrid P2P VPN which bridges nodes without any central server also behind nat.

- No infrastructure is required. C3OS can be used to bootstrap a cluster entirely from the ground-up.
- LAN, remote networks, multi-region/zones, NAT - No network configuration or opening port outside is required. Nodes will connect each other via holepunching and using hops wherever necessary.
- Zero kubernetes configuration - Nodes autonomously discover and configure themselves to form a Kubernetes cluster. The same configuration/bootstrapping process applies wether creating new clusters or joining nodes to existing one.

[Documentation available here](https://c3os-io.github.io/c3os).

## Run 

Download the ISO from the latest [releases](https://github.com/c3os-io/c3os/releases).

## Installation

Boot the ISO and follow the instructions on screen. The openSUSE variant supports automatic peer discovery and [device pairing](https://c3os-io.github.io/c3os/installation/device_pairing/).

Use the `c3os` CLI to register and handle node installation remotely, check out the [documentation](https://c3os-io.github.io/c3os).

### Manual

Install `c3os` with `cos-install --config <config-file>` or either place it in `/oem` after install. The config file can be a cloud-init file, or a URL pointing to a cloud-init file.

## Build

Needs only docker.

Run `build.sh`, should produce a docker image along with an ISO

## Upgrades

[Docs](https://c3os-io.github.io/c3os/after_install/upgrades/)
