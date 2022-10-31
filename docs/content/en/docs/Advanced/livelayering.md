---
title: "Live layering"
linkTitle: "Live layering"
weight: 4
description: >
---

# Live layering

Live layering allows to activates/deactivates system extension images. System extension images may – dynamically at runtime — extend the `/usr/` and `/opt/` directory hierarchies with additional files.

Kairos supports live layering with `systemd-sysext`. Currently it is supported only on the `openSUSE`, `Fedora` and `Ubuntu` flavors with `systemd-sysext`.

## Description

For general reference on how `systemd-sysext` works, please read the [official](https://www.freedesktop.org/software/systemd/man/systemd-sysext.html) documentation.

Systemd system extensions can be located in the directories `/etc/extensions/`, `/run/extensions/`, `/var/lib/extensions/`, `/usr/lib/extensions/` and `/usr/local/lib/extensions/`.

In order to install extensions in runtime, they need to be placed into `/usr/local/lib/extensions` which is mounted over the `COS_PERSISTENT` partition. The other paths are reserved for the system image, which could ship extension directly from the container image used for upgrade or deployment.

## Installing extensions

In order to install extensions, you can just place them into `/usr/local/lib/extensions`.

For example, on a running Kairos node to install an extension from a container image:

```bash
luet util unpack <image> /usr/local/lib/extensions/<extension_name>
```

To load an extension during installation of a Kairos node, it can be supplied as a bundle in the `install` block in the node configuration:

```yaml
#node-config

# Set username and password
stages:
   initramfs:
     - name: "Set user and password"
       users:
        kairos:
          passwd: "kairos"
       hostname: kairos-{{ trunc 4 .Random }}
# Install configuration block
install:
  auto: true
  reboot: true
  device: auto
  # Bundles to install
  bundles:
  - rootfs_path: /usr/local/lib/extensions/<name>
    targets:
    - container://<image>
```

## Building extensions

Systemd extensions can be images, directory or files, quoting the systemd-sysext documentation:

- Plain directories or btrfs subvolumes containing the OS tree

- Disk images with a GPT disk label, following the Discoverable Partitions Specification

- Disk images lacking a partition table, with a naked Linux file system (e.g. squashfs or ext4)

All of those can be shipped as a container image and loaded as a bundle.

For example, a bundle can be defined as a naked container image containing only the files that we want to overlay in the system.

Consider the following Dockerfile to create an extension which adds `/usr/bin/ipfs` to the system:

```docker
FROM alpine as build
# Install a binary
ARG VERSION
ENV VERSION=$VERSION

RUN wget https://github.com/ipfs/kubo/releases/download/v0.15.0/kubo_v0.15.0_linux-amd64.tar.gz -O kubo.tar.gz
RUN tar xvf kubo.tar.gz
RUN mv kubo/ipfs /usr/bin/ipfs
RUN mkdir -p /usr/lib/extension-release.d/
RUN echo ID=kairos > /usr/lib/extension-release.d/extension-release.kubo
RUN echo VERSION_ID=$VERSION >> /usr/lib/extension-release.d/extension-release.kubo


FROM scratch

COPY --from=build /usr/bin/ipfs /usr/bin/ipfs
COPY --from=build /usr/lib/extension-release.d /usr/lib/extension-release.d
```

We can build that image with:

```bash
docker build --build-arg VERSION=v1.0.0 -t image .
```

Note that systemd extensions requires an extension-release file which matches the `ID` and the `VERSION_ID` of the OS iqn the `/etc/os-release` file.
This has the consequence that bundles can be created for specific OS versions and are loaded only if `ID` and `VERSION_ID` are matching. In the example above this can be controlled by the `VERSION` build arg while building the container image.
