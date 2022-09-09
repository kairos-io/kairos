+++
title = "Manual"
date = 2022-02-09T17:56:26+01:00
weight = 1
pre = "<b>- </b>"
+++


Upgrades can be run manually from the terminal. 

c3os images are released on [quay.io](https://quay.io/repository/c3os/c3os).

## List available versions

To see all the available versions:

```bash
$ sudo c3os-agent upgrade list-releases
v0.57.0
v0.57.0-rc2
v0.57.0-rc1
v0.57.0-alpha2
v0.57.0-alpha1
```

## Upgrade

To upgrade to latest available version, run from a shell of a cluster node:

```bash
sudo c3os-agent upgrade
```

To specify a version, just run 

```bash
sudo c3os-agent upgrade <version>
```

Use `--force` to force upgrading to avoid checking versions. 

It is possible altough to use the same commandset from `Elemental-toolkit`. So for example, the following works too:

```bash
sudo elemental upgrade --no-verify --docker-image quay.io/c3os/c3os:opensuse-v1.21.4-22
```

[See also the general Elemental-toolkit documentation](https://rancher.github.io/elemental-toolkit/docs/getting-started/upgrading/#upgrade-to-a-specific-container-image) which applies for `c3os` as well.