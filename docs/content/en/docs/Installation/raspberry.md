---
title: "RaspberryPi"
linkTitle: "RaspberryPi"
weight: 4
date: 2022-11-13
description: >
  Install Kairos on RaspberryPi 3 and 4
---

Kairos supports Raspberry Pi model 3 and 4 with 64bit architecture.

If you are not familiar with the process, it is suggested to follow the [quickstart](/docs/getting-started) first to see how Kairos works.

## Prerequisites

- An SD card which size is at least 16 GB
- Etcher or `dd`
- A Linux host where to flash the device

## Download

Extract the `img` file from a container image as described [in this page](/docs/reference/image_matrix)

## Flash the image

Plug the SD card to your system. To flash the image, you can either use Etcher or `dd`. Note it's compressed with "XZ", so we need to decompress it first:

```bash
xzcat kairos-opensuse-leap-arm-rpi-v1.0.0-rc2-k3sv1.21.14+k3s1.img.xz | sudo dd of=<device> oflag=sync status=progress bs=10MB
```

Once the image is flashed, there is no need to carry any other installation steps. We can boot the image, or apply our config.

## Boot

Use the SD Card to boot. The default username/password is `kairos`/`kairos`.
To configure your access or disable password change the `/usr/local/cloud-config/01_defaults.yaml` accordingly.

## Configure your node

To configure the device beforehand, be sure to have the SD plugged in your host. We need to copy a configuration file into `cloud-config` in the `COS_PERSISTENT` partition:

```
$ PERSISTENT=$(blkid -L COS_PERSISTENT)
$ mkdir /tmp/persistent
$ sudo mount $PERSISTENT /tmp/persistent
$ sudo mkdir /tmp/persistent/cloud-config
$ sudo cp cloud-config.yaml /tmp/persistent/cloud-config
$ sudo umount /tmp/persistent
```

You can push additional `cloud config` files. For a full reference check out the [docs](/docs/reference/configuration) and also [configuration after-installation](/docs/advanced/after-install)

## Customizing the disk image

The following shell script shows how to localy rebuild and customize the image with docker

```
IMAGE=quay.io/kairos/kairos-alpine-arm-rpi:v1.1.6-k3sv1.25.3-k3s1
# Pull the image locally
docker pull $IMAGE
mkdir -p build
docker run -v $PWD:/HERE -v /var/run/docker.sock:/var/run/docker.sock --privileged -i --rm --entrypoint=/build-arm-image.sh quay.io/kairos/osbuilder-tools:v0.4.0 \
 --model rpi64 \
 --state-partition-size 6200 \
 --recovery-partition-size 4200 \
 --size 15200 \
 --images-size 2000 \
 --local \
 --config /HERE/cloud-config.yaml \
 --docker-image $IMAGE /HERE/build/out.img

```
