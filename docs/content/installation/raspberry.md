+++
title = "Raspberry"
date = 2022-02-09T17:56:26+01:00
weight = 5
chapter = false
pre = "<b>- </b>"
+++

`c3os` supports Rasperry Pi model 3 and 4 with 64bit architecture.

You can find arm64 raspberry images in the releases page. For example `https://github.com/c3os-io/c3os/releases/download/v1.21.4-35/c3os-opensuse-arm-rpi-v1.21.4-35.img.tar.xz`. 

Flash the image into a SD card with dd or Etcher and place your cloud-init configuration file inside `cloud-config` into the `COS_PERSISTENT` partition, for example `cloud-config/cloud-init.yaml`.

Also make sure to resize and enlarge the `COS_PERSISTENT` partition accordingly, or via cloud-init configuration.