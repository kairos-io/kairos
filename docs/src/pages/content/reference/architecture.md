---
layout: "../../../layouts/docs/Layout.astro"
title: "Architecture"
index: 2
---

This section contains refrences to how Kairos works internally.

## Setup process

`kairos` node at first boot will start the `kairos-agent` service, you can always check what's happening by running `journalctl -fu kairos-agent`.

This service will setup `k3s` and `edgevpn` dynamically on first-boot, once it configures the machine it does not run on boot anymore, unless `/usr/local/.kairos/deployed` is removed..

Those are the steps executed in sequence by the `kairos-agent` service:

- Will create a `edgevpn@kairos` service and enabled on start. The configuration for the connection is stored in `/etc/systemd/system.conf.d/edgevpn-kairos.env` and depends on the cloud-init configuration file provided during installation time
- Automatic role negotiation starts, nodes will co-ordinate for an IP and a role
- Once roles are defined a node will either set the `k3s` or `k3s-agent` service. Configuration for each service is stored in `/etc/sysconfig/k3s` and `/etc/sysconfig/k3s-agent` respectively

## Paths

The following paths are relevant for Kairos:

| Path                        | Description                                                                                    |
| :-------------------------- | :--------------------------------------------------------------------------------------------- |
| /usr/local/.kairos/deployed | Sentinel file written after bootstrapping is complete. Remove to retrigger automatic bootstrap |
| /usr/local/.kairos/lease    | IP Lease of the node in the network. Delete to change IP address of the node                   |
