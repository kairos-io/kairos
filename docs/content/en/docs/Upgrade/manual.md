---
title: "Manual"
linkTitle: "Manual"
weight: 2
date: 2022-11-13
description: >
---

Upgrades can be run manually from the terminal.

Kairos images are released on [quay.io](https://quay.io/organization/kairos).

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


To upgrade with a container image behind a registry with authentication, the upgrade command provides the following flags:

| Flag                    | Description                                                                              |
|-------------------------|------------------------------------------------------------------------------------------|
| `--auth-username`       | User to authenticate with                                                                |
| `--auth-password`       | Password to authenticate with                                                            |
| `--auth-server-address` | Server address to authenticate to, defaults to docker                                    |
| `--auth-registry-token` | IdentityToken is used to authenticate the user and get an access token for the registry. |
| `--auth-identity-token` | RegistryToken is a bearer token to be sent to a registry                                 |

For instance:

```bash
sudo kairos-agent upgrade --image private/myimage:latest --auth-username MYNAME --auth-password MYPASSWORD
```