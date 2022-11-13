---
title: "Manual"
linkTitle: "Manual"
weight: 2
date: 2022-11-13
description: >
---

# Upgrading manually

Upgrades can be run manually from the terminal.

Kairos images are released on [quay.io](https://quay.io/repository/kairos/kairos).

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

To upgrade to the latest available version, run from a shell of a cluster node the following:

```bash
sudo kairos-agent upgrade
```

To specify a version, run:

```bash
sudo kairos-agent upgrade <version>
```

Use `--force` to force upgrading to avoid checking versions.

To specify a specific image, use the `--image` flag:

```bash
sudo kairos-agent upgrade --image <image>
```
