# [![Docker Repository on Quay](https://quay.io/repository/mudler/c3os/status "Docker Repository on Quay")](https://quay.io/repository/mudler/c3os) cOS + k3s = c3OS

A dead simple [cOS](https://github.com/rancher-sandbox/cOS-toolkit) derivative with k3s based on openSUSE.

## Run 

Download the ISO from the latest [releases](https://github.com/mudler/c3os/releases).

## Installation

Boot the ISO and install `c3os` with `cos-install --config <config-file>` or either place it in `/oem` after install. The config file can be a cloud-init file, or a URL pointing to a cloud-init file.

## Build

Needs only docker.

Run `build.sh`, should produce a docker image along with an ISO

## Cloud-init examples

`c3os` supports the standard cloud-init syntax and the extended one from the [cOS toolkit](https://rancher-sandbox.github.io/cos-toolkit-docs/docs/reference/cloud_init/).

Examples using the extended notation for running k3s as agent or server are in `examples/`. 

## Upgrades

`c3os` supports both manual and upgrades within kubernetes with `system-upgrade-controller`.

For an example of how to trigger an upgrade, [see the cOS toolkit documentation](https://rancher-sandbox.github.io/cos-toolkit-docs/docs/getting-started/upgrading/#integration-with-system-upgrade-controller).


## Default user

The system have an hardcoded username/password when running from the LiveCD:

```
user: c3os
pass: c3os
```

Note, after the upgrade the password login is disabled, so users and ssh keys to login must be configured via cloud-init.