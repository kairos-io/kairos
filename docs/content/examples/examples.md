+++
title = "Common setup"
date = 2022-02-09T17:56:26+01:00
weight = 6
chapter = false
pre = "<b>- </b>"
+++

In the following section you can find example configuration files to achieve specific `c3os` setups.

# Single node cluster

By default `c3os` requires multiple nodes. As for the `c3os` decentralized nature, it requires co-ordination between at least 2 nodes to achieve consensus on IPs, network setting, etc.

In order to create single-node cluster, we need to force both the `role` and the `ip` by disabling `DHCP`:

```yaml
c3os:
  network_token: "...."
  role: "master"
vpn:
  # EdgeVPN environment options
  DHCP: "false"
  ADDRESS: "10.1.0.2/24"
```

Note, the same setup can be used to specify master nodes in a set, as to join nodes it is still possible without specifying any extra setting:

```yaml
c3os:
  network_token: "...."
```

As always, IPs here are arbitrary as they are virtual ips in the VPN which is created between the cluster nodes.

# Run only k3s without VPNs

`c3os` can be also used without any VPN and P2P network. Infact, `k3s` is already pre-installed, and it is sufficient to not specify any `c3os` block in the cloud init configuration.

For example, to start `k3s` as a server with `c3os` it's sufficient to specify the `k3s` service in the config file:

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

This is will setup k3s single-node + VPN with a static ip (`10.1.0.2`).

```yaml
c3os:
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
        c3os:
          passwd: "c3os"
```

## Hostname

Sometimes you might want to create a single cloud-init file for a set of machines, and also make sure each node has a different hostname.

The cloud-config syntax supports templating, so one could automate hostname generation based on the machine id which is generated for each host:

```yaml
#node-config

stages:
   initramfs:
     - name: "Setup hostname"
       hostname: "node-{{ trunc 4 .MachineID }}"
```
