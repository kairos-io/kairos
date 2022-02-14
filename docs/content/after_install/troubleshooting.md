+++
title = "Troubleshooting"
date = 2022-02-09T17:56:26+01:00
weight = 3
chapter = false
pre = "<b>- </b>"
+++

## Get kubeconfig

On all nodes of the cluster it's possible to invoke `c3os get-kubeconfig` to recover the kubeconfig file

## Connect to the cluster network

Network tokens can be used to connect to the VPN created by the cluster. They are infact tokens of [edgevpn](https://github.com/mudler/edgevpn) networks, and thus can be used to connect to. Refer to the [edgeVPN](https://mudler.github.io/edgevpn/docs/getting-started/cli/) documentation on how to connect to the VPN, but it boils down to run `edgevpn`:

```bash
EDGEVPNTOKEN=<network_token> edgevpn --dhcp
```

## Setup process

`c3os` node at first boot will start the `c3os-setup` service, you can always check what's happening by running `journalctl -fu c3os-setup`.

This service will setup `k3s` and `edgevpn` dynamically on first-boot, once it configures the machine it does not run on boot anymore, unless `/usr/local/.c3os/deployed` is removed..

Those are the steps executed in sequence by the `c3os-setup` service:

- Will create a `edgevpn@c3os` service and enabled on start. The configuration for the connection is stored in `/etc/systemd/system.conf.d/edgevpn-c3os.env` and depends on the cloud-init configuration file provided during installation time
- Automatic role negotiation starts, nodes will co-ordinate for an IP and a role
- Once roles are defined a node will either set the `k3s` or `k3s-agent` service. Configuration for each service is stored in `/etc/sysconfig/k3s` and `/etc/sysconfig/k3s-agent` respectively
  
