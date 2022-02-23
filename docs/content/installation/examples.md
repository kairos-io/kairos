+++
title = "Examples"
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