+++
title = "Common setup"
date = 2022-02-09T17:56:26+01:00
weight = 6
chapter = false
pre = "<b>- </b>"
+++

In the following section, you can find example configuration files to achieve specific `Kairos` setups.

# Single node cluster

By default `kairos` requires multiple nodes. As for the `kairos` decentralized nature, it requires co-ordination between at least 2 nodes to achieve consensus on IPs, network setting, etc.

To create single-node cluster, we need to force both the `role` and the `ip` by disabling `DHCP`:

```yaml
kairos:
  network_token: "...."
  role: "master"
vpn:
  # EdgeVPN environment options
  DHCP: "false"
  ADDRESS: "10.1.0.2/24"
```

**Note**: The same setup can be used to specify master nodes in a set, as to join nodes it is still possible without specifying any extra setting:

```yaml
kairos:
  network_token: "...."
```

As always, IPs here are arbitrary as they are virtual IPs in the VPN which is created between the cluster nodes.

# Run Only K3s without VPNs

`Kairos` can be also used without any VPN and P2P network. In fact, `k3s` is already pre-installed, and it is sufficient to not specify any `Kairos` block in the cloud-init configuration.

For example, to start `k3s` as a server with `kairos` it's sufficient to specify the `k3s` service in the config file:

```yaml
#node-config

k3s:
  enabled: true
```

And similarly for an `agent`:

```yaml
#node-config
k3s-agent:
  enabled: true
  env:
    K3S_TOKEN: ...
    K3S_URL: ...
```

## Single node cluster with default user/password

This is will set up K3s single-node + VPN with a static IP (`10.1.0.2`).

```yaml
Kairos:
  network_token: "...."
  role: "master"

vpn:
  # EdgeVPN environment options
  DHCP: "false"
  ADDRESS: "10.1.0.2/24"
  
stages:
   initramfs:
     - name: "Set user and password"
       users:
        Kairos:
          passwd: "Kairos"
```

## Hostname

Sometimes you might want to create a single cloud-init file for a set of machines and also make sure each node has a different hostname.

The cloud-config syntax supports templating, so you could automate hostname generation based on the machine ID which is generated for each host:

```yaml
#node-config

stages:
   initramfs:
     - name: "Setup hostname"
       hostname: "node-{{ trunc 4 .MachineID }}"
```
