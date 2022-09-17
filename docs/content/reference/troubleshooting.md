+++
title = "Troubleshooting"
date = 2022-02-09T17:56:26+01:00
weight = 3
chapter = false
pre = "<b>- </b>"
+++

## Root permission

By default there is no root user set. A default user (`kairos`) is created and can use `sudo` without password authentication during LiveCD bootup.

## Get kubeconfig

On all nodes of the cluster it's possible to invoke `kairos get-kubeconfig` to recover the kubeconfig file

## Connect to the cluster network

Network tokens can be used to connect to the VPN created by the cluster. They are indeed tokens of [edgevpn](https://github.com/mudler/edgevpn) networks, and thus can be used to connect to with its CLI. 

The `kairos` CLI can be used to connect as well, with the `bridge` command:

```bash
sudo kairos bridge --network-token <TOKEN>
```

{{% notice note %}}
The command needs root permissions as it sets up a local tun interface to connect to the VPN.
{{% /notice %}}

Afterward you can connect to [localhost:8080](http://localhost:8080) to access the network API and verify machines are connected.

See [edgeVPN](https://mudler.github.io/edgevpn/docs/getting-started/cli/) documentation on how to connect to the VPN with the edgeVPN cli, which is similar:

```bash
EDGEVPNTOKEN=<network_token> edgevpn --dhcp
```

## Setup process

`kairos` node at first boot will start the `kairos-agent` service, you can always check what's happening by running `journalctl -fu kairos-agent`.

This service will setup `k3s` and `edgevpn` dynamically on first-boot, once it configures the machine it does not run on boot anymore, unless `/usr/local/.kairos/deployed` is removed..

Those are the steps executed in sequence by the `kairos-agent` service:

- Will create a `edgevpn@kairos` service and enabled on start. The configuration for the connection is stored in `/etc/systemd/system.conf.d/edgevpn-kairos.env` and depends on the cloud-init configuration file provided during installation time
- Automatic role negotiation starts, nodes will co-ordinate for an IP and a role
- Once roles are defined a node will either set the `k3s` or `k3s-agent` service. Configuration for each service is stored in `/etc/sysconfig/k3s` and `/etc/sysconfig/k3s-agent` respectively
  
