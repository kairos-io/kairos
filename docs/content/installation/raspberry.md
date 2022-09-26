+++
title = "Raspberry"
date = 2022-02-09T17:56:26+01:00
weight = 5
chapter = false
pre = "<b>- </b>"
+++

`kairos` supports Rasperry Pi model 3 and 4 with 64bit architecture.

If you are not familiar with the process, it is suggested to follow the [quickstart](/quickstart/installation) first to see how Kairos works.

## Prerequisites

- an SD card with 16gb
- Etcher or `dd`
- A Linux host where to flash the device

## Download

Download the Kairos images from the [Releases](https://github.com/kairos-io/provider-kairos/releases) you are interested into. For example, for RPI and `k3sv1.21.14+k3s1`:

```bash
wget https://github.com/kairos-io/provider-kairos/releases/download/v1.0.0-rc2/kairos-opensuse-arm-rpi-v1.0.0-rc2-k3sv1.21.14+k3s1.img
```

## Flash the image

Plug the SD card to your system - to flash the image, you can either use Etcher or `dd`:

```bash
dd if=kairos-opensuse-arm-rpi-v1.0.0-rc2-k3sv1.21.14+k3s1.img of=<device> oflag=sync status=progress
```

## Configure your node

To configure the device, be sure to have the SD plugged in your host. We need to copy a configuration file into `cloud-config` in the `COS_PERSISTENT` partition:

```
$ PERSISTENT=$(blkid -L COS_PERSISTENT)
$ mkdir /tmp/persistent
$ mount $PERSISTENT /tmp/persistent
$ mkdir /tmp/persistent/cloud-config
$ cp cloud-config.yaml /tmp/persistent/cloud-config
$ umount /tmp/persistent
```

You can, additionally push more cloud config files into such folder following the [yip](https://github.com/mudler/yip) syntax.
