+++
title = "Customizing the system image"
date = 2022-02-09T17:56:26+01:00
weight = 5
chapter = false
pre = "<b>- </b>"
+++

Kairos is a container-based OS, if you want to change Kairos and add a package, it is required to build only a Docker image.

For example:

```Dockerfile
FROM quay.io/kairos/kairos:opensuse-latest

RUN zypper in -y ...

RUN export VERSION="my-version"
RUN envsubst '${VERSION}' </etc/os-release
```

The image can be then used with `kairos upgrade` or with system-upgrade-controller for upgrades within Kubernetes.