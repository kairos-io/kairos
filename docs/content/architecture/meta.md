+++
title = "Meta-Distribution"
date = 2022-02-09T17:56:26+01:00
weight = 1
pre = "<b>- </b>"
+++

We like to define kairos as a meta Linux Distribution as its goal is to convert any other distro to an immutable layout with Kubernetes Native components.

## Kairos

The Kairos stack is composed of the following:

- A core OS image release for each flavor in ISO, qcow2, and similar format (currently can pick from openSUSE and Alpine based) - provided for user convenience
- A release with k3s embedded
- A set of Kubernetes Native API components (CRDs) to install into the control-plane node, to manage deployment, artifacts creation, and lifecycle (WIP)
- A set of Kubernetes Native API components (CRDs) to install into the target nodes to manage and control the node after deployment (WIP)
- An agent installed into the nodes to be compliant with Kubernetes Native API components mentioned above

Every component is extensible and modular such as it can be customized and replaced in the stack, and built off either locally or with Kubernetes

## Setup process

`kairos` node at first boot will start the `kairos-agent` service, you can always check what's happening by running `journalctl -fu kairos-agent`.

This service will setup `k3s` and `edgevpn` dynamically on first-boot, once it configures the machine it does not run on boot anymore, unless `/usr/local/.kairos/deployed` is removed..

Those are the steps executed in sequence by the `kairos-agent` service:

- Will create a `edgevpn@kairos` service and enabled on start. The configuration for the connection is stored in `/etc/systemd/system.conf.d/edgevpn-kairos.env` and depends on the cloud-init configuration file provided during installation time
- Automatic role negotiation starts, nodes will co-ordinate for an IP and a role
- Once roles are defined a node will either set the `k3s` or `k3s-agent` service. Configuration for each service is stored in `/etc/sysconfig/k3s` and `/etc/sysconfig/k3s-agent` respectively
  
### Internal components

Kairos encompassess several components, some externally, most notably:

- [k3s](https://k3s.io) as a Kubernetes distribution
- [edgevpn](https://mudler.github.io/edgevpn) (optional) as fabric for the distributed network, node coordination and bootstrap. Provides also embedded DNS capabilities for the cluster.
- [elemental-toolkit](https://rancher.github.io/elemental-toolkit/docs/) as a fundament to build the Linux derivative. Indeed, any `Elemental` docs applies to `kairos` as well.
- [nohang](https://github.com/hakavlad/nohang) A sophisticated low memory handler for Linux 