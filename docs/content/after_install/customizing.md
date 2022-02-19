+++
title = "Customizing the system image"
date = 2022-02-09T17:56:26+01:00
weight = 4
chapter = false
pre = "<b>- </b>"
+++

`c3os` is a container-based OS, if you want to change `c3os` and add a package it is required to build only a docker image.

For example:

```Dockerfile
FROM quay.io/c3os/c3os:opensuse-latest

RUN zypper in -y ...

RUN export VERSION="my-version"
RUN envsubst '${VERSION}' </etc/os-release
```

The image can be then used with `c3os upgrade` or with system-upgrade-controller for upgrades within Kubernetes.