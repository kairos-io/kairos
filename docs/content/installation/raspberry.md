+++
title = "Raspberry"
date = 2022-02-09T17:56:26+01:00
weight = 5
chapter = false
pre = "<b>- </b>"
+++

Kairos supports Raspberry Pi model 3 and 4 with 64bit architecture.

You can find arm64 raspberry images in the releases page. For example https://github.com/kairos-io/provider-kairos/releases/download/v1.0.0-rc3/kairos-opensuse-v1.0.0-rc3-k3sv1.21.14+k3s1.iso.

Flash the image into a SD card with DD or Etcher and place your `cloud-init` configuration file inside the `cloud-config` directory (create it if not present) into the `COS_PERSISTENT` partition, for example `cloud-config/cloud-init.yaml`.

