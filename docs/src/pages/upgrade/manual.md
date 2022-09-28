---
layout: "../../layouts/docs/Layout.astro"
title: "Manual"
index: 2
---

Upgrades can be run manually from the terminal.

kairos images are released on [quay.io](https://quay.io/repository/kairos/kairos).

## List available versions

To see all the available versions:

```bash
$ sudo kairos-agent upgrade list-releases
v0.57.0
v0.57.0-rc2
v0.57.0-rc1
v0.57.0-alpha2
v0.57.0-alpha1
```

## Upgrade

To upgrade to latest available version, run from a shell of a cluster node:

```bash
sudo kairos-agent upgrade
```

To specify a version, just run

```bash
sudo kairos-agent upgrade <version>
```

Use `--force` to force upgrading to avoid checking versions.

It is possible altough to use the same commandset from `Elemental-toolkit`. So for example, the following works too:

```bash
sudo elemental upgrade --no-verify --docker-image quay.io/kairos/kairos:opensuse-v1.21.4-22
```

[See also the general Elemental-toolkit documentation](https://rancher.github.io/elemental-toolkit/docs/getting-started/upgrading/#upgrade-to-a-specific-container-image) which applies for `kairos` as well.
