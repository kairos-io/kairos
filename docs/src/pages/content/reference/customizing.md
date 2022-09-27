---
layout: "../../../layouts/docs/Layout.astro"
title: "Customizing the system image"
index: 7
---

`kairos` is a container-based OS, if you want to change `kairos` and add a package it is required to build only a docker image.

For example:

```Dockerfile
FROM quay.io/kairos/kairos:opensuse-latest

RUN zypper in -y ...

RUN export VERSION="my-version"
RUN envsubst '${VERSION}' </etc/os-release
```

The image can be then used with `kairos upgrade` or with system-upgrade-controller for upgrades within Kubernetes.
