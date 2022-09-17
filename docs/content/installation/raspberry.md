+++
title = "Raspberry"
date = 2022-02-09T17:56:26+01:00
weight = 5
chapter = false
pre = "<b>- </b>"
+++

`kairos` supports Rasperry Pi model 3 and 4 with 64bit architecture.

You can find arm64 raspberry images in the releases page. For example `https://github.com/kairos-io/kairos/releases/download/v1.21.4-35/kairos-opensuse-arm-rpi-v1.21.4-35.img.tar.xz`. 

Flash the image into a SD card with dd or Etcher and place your cloud-init configuration file inside the `cloud-config` directory ( create it if not present ) into the `COS_PERSISTENT` partition, for example `cloud-config/cloud-init.yaml`.

