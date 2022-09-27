---
layout: "../../../layouts/docs/Layout.astro"
title: "Common setup"
index: 2
---

In the following section you can find example configuration files to achieve specific `kairos` setups.

# Single node cluster

By default `kairos` requires multiple nodes. As for the `kairos` decentralized nature, it requires co-ordination between at least 2 nodes to achieve consensus on IPs, network setting, etc.

In order to create single-node cluster, we need to force both the `role` and the `ip` by disabling `DHCP`:

```yaml
kairos:
  network_token: "...."
  role: "master"
vpn:
  # EdgeVPN environment options
  DHCP: "false"
  ADDRESS: "10.1.0.2/24"
```

Note, the same setup can be used to specify master nodes in a set, as to join nodes it is still possible without specifying any extra setting:

```yaml
kairos:
  network_token: "...."
```

As always, IPs here are arbitrary as they are virtual ips in the VPN which is created between the cluster nodes.

# Run only k3s without VPNs

`kairos` can be also used without any VPN and P2P network. Infact, `k3s` is already pre-installed, and it is sufficient to not specify any `kairos` block in the cloud init configuration.

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

This is will setup k3s single-node + VPN with a static ip (`10.1.0.2`).

```yaml
kairos:
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
        kairos:
          passwd: "kairos"
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
