---
title: "Network booting"
linkTitle: "Network booting"
weight: 5
date: 2022-12-1
description: >
  Install Kairos from network
---

Most hardware these days, supports booting an operating system from the network.
The technology behind this is called [Preboot Execution Environment](https://en.wikipedia.org/wiki/Preboot_Execution_Environment).
Kairos releases include artifacts to allow booting from the network. In general, the following files are needed:

- The initrd image: It's the system that loads first. It's responsible to load the kernel.
- The kernel: This is the kernel of the operating system that will boot.
- The squashfs: The filesystem of the operating system that will boot.

Booting using these files can happen in two ways:

- Either with direct support from the machine BIOS plus network configuration (DHCP server etc).
- Software based network booting. This works with a special ISO, built with
  [ipxe](https://ipxe.org/) project. Kairos releases include pre-built ISOs for
  netbooting (named like `*.ipxe.iso.ipxe`).


Generic hardware based netbooting is out of scope for this document.
Below we give instructions on how to use the Kairos release artifacts to netboot.

## Boot with pre-built ISOs

The ipxe ISOs from the Kairos release artifacts, were built with a ipxe script that points directly to the
`kernel`, `initrd` and `squashfs` artifacts of the same release on GitHub.

E.g.:

```
#!ipxe
set url https://github.com/kairos-io/kairos/releases/download/v1.3.0
set kernel kairos-alpine-opensuse-leap-v1.3.0-kernel
set initrd kairos-alpine-opensuse-leap-v1.3.0-initrd
set rootfs kairos-alpine-opensuse-leap-v1.3.0.squashfs

# Configure interface
ifconf

# set config https://example.com/machine-config
# set cmdline extra.values=1
kernel ${url}/${kernel} initrd=${initrd} rd.neednet=1 ip=dhcp rd.cos.disable root=live:${url}/${rootfs} netboot nodepair.enable config_url=${config} console=tty1 console=ttyS0 ${cmdline}
initrd ${url}/${initrd}
boot
```

Booting the ISO will automatically download and boot those artifacts. E.g. using qemu:

```
#!/bin/bash

qemu-img create -f qcow2 disk.img 40g
qemu-system-x86_64 \
    -m 4096 \
    -smp cores=2 \
    -nographic \
    -drive if=virtio,media=disk,file=disk.img \
    -drive if=ide,media=cdrom,file=${1:-kairos.iso}

```

## Notes on booting from network

Another way to boot with the release artifacts is using [pixiecore](https://github.com/danderson/netboot/tree/master/pixiecore).
Using a ipxe script [like the one in that project](https://github.com/danderson/netboot/blob/master/pixiecore/boot.ipxe), it is possible to use DHCP to boot to any version (unlike the hardcoded ISOs in the previous section).
For example:

- Start pixiecore server:

```sh
#!/bin/sh

sudo docker run  \
  --net=host \
  -v $PWD/artifacts:/files \
  quay.io/pixiecore/pixiecore \
  boot /files/kairos-core-alpine-ubuntu-kernel \
  /files/kairos-core-alpine-ubuntu-initrd \
  --cmdline='rd.neednet=1 ip=dhcp rd.cos.disable root=live:{{ ID "/files/kairos-core-alpine-ubuntu.squashfs" }} netboot nodepair.enable config_url={{ ID "/files/config.yaml" }} console=tty1 console=ttyS0 console=tty0'
```

This will start the pixiecore server. Any machine that depends on DHCP to netboot will be send the specified files and the cmd boot line.

Kairos project has and experimental ISO that boots from DHCP that can be used to try this out:

https://github.com/kairos-io/ipxe-dhcp/releases

(A DHCP server should be running on the same network where this boots and pixiecore server is running)
