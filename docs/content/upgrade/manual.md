+++
title = "Manual"
date = 2022-02-09T17:56:26+01:00
weight = 1
pre = "<b>- </b>"
+++


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

To upgrade to the latest available version, run from a shell of a cluster node:

```bash
sudo kairos-agent upgrade
```

To specify a version, run 

```bash
sudo kairos-agent upgrade <version>
```

Use `--force` to force upgrading to avoid checking versions. 

It is possible although to use the same command set from `Elemental-toolkit`. So for example, the following works too:

```bash
sudo elemental upgrade --no-verify --docker-image quay.io/kairos/kairos:opensuse-v1.21.4-22
```

[See also the general Elemental-toolkit documentation](https://rancher.github.io/elemental-toolkit/docs/getting-started/upgrading/#upgrade-to-a-specific-container-image) which applies for Kairos as well.