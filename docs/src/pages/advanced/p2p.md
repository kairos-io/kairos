---
layout: "../../layouts/docs/Layout.astro"
title: "P2P configuration"
index: 2
---


## Single node cluster

When the p2p featureset is enabled by specifying a `kairos` block, by default are expected multiple nodes to join: this is required in order to correctly handle co-ordination among the nodes of a cluster.

To create a single-node cluster, we need to force both the `role` and the `ip` by disabling `DHCP`:

```yaml
kairos:
  network_token: "...."
  role: "master"
vpn:
  # EdgeVPN environment options
  DHCP: "false"
  ADDRESS: "10.1.0.2/24"
```

**Note**: The same setup can be used to specify master node amont other nodes, it is still possible for workers to join without specifying any extra setting:

```yaml
kairos:
  network_token: "...."
```

As always, IPs here are arbitrary, as they are virtual IPs in the VPN which is created between the cluster nodes.

