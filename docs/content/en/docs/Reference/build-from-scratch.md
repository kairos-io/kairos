---
title: "Build Kairos from scratch"
linkTitle: "Build Kairos from scratch"
weight: 5
description: >
    This article shows how to bring your own image with Kairos, and build a Kairos derivative from scratch using base container images from popular distributions such as Ubuntu, Fedora, openSUSE, etc.
---

{{% alert title="Note" %}}
By default, Core and Standard Kairos images are pre-configured, optimized and maintained by the Kairos team, meeting most use cases. However, if you're an advanced user interested in creating your own derivative or building new flavors for Kairos core images, this section is reserved just for you.

While the process of building these images is still a work in progress, it's already usable for general consumption. You can follow our development efforts in the [factory epic](https://github.com/kairos-io/kairos/issues/116). For instance, we are currently working on adding features like [conformance tests](https://github.com/kairos-io/kairos/issues/958) to enable users to test images built with this process, ensuring their correctness before attempting to boot the system.
{{% /alert %}}

Kairos enables the creation of a distribution based on any base OS image that satisfies the Kairos model and contract. Essentially, every OS is treated solely as a collection of packages, and upgrades and operations are managed by Kairos components, which abstract the management model.

In practical terms, upgrades are not carried out by the package manager of the OS. Instead, the `kairos-agent` handles upgrades through container images. All installation and upgrades are delivered exclusively through container images. These images are overlayed at boot time, which means there is no additional runtime overhead, as no container engine is required for booting the OS.

The Kairos framework is an abstract layer between the OS and the management interface. It follows an atomic A/B approach, which can be controlled through Kubernetes, the CLI, or a declarative model.

The Kairos contract is straightforward: the OS container image must include everything required for booting, from the kernel to the init system.

The contract has several advantages:

- Delegation of package maintenance, CVE, and security fixes to the OS layer
- Easy issuance of upgrades to container images by chaining Dockerfiles or manually committing changes to the image. See also [Customizing](/docs/advanzed/customizing).
- Clear separation of concerns: the OS provides the booting bits and packages necessary for the OS to function, while Kairos provides the operational framework for handling the node's lifecycle and immutability interface.
- Support for long-term maintenance: each framework image allows conversion of any OS to the given Kairos framework version, potentially enabling maintenance for as long as the base OS support model allows.

This document outlines the steps for making any base image fully bootable with the Kairos framework. The steps include:

- Building a container image
  - Selecting a base image from the supported OS family (although it should work with any distro)
  - Installing the required packages from the package manager of the chosen OS
  - Building the initramfs
- Building an offline bootable ISO or netbooting the container image.

## Prerequisites

To follow the steps below, you'll need to have Docker or a container engine installed on your local machine. Additionally, note that the steps have been tested on Linux but should also work in other environments. If you encounter any issues, please feel free to open up issues and help us improve the Documentation!

## Build a container image

To build the container image, follow these steps:

1. Create a new directory for the image and write a Dockerfile inside it. The Dockerfile will contain the instructions for building the image:

```Dockerfile
FROM fedora:36

# Install any package wanted here
# Note we need to install _at least_ the minimum required packages for Kairos to work:
# - An init system (systemd)
# - Grub
# - kernel/initramfs 
RUN echo "install_weak_deps=False" >> /etc/dnf/dnf.conf

RUN dnf install -y \
    audit \
    coreutils \
    curl \
    device-mapper \
    dosfstools \
    dracut \
    dracut-live \
    dracut-network \
    dracut-squash \
    e2fsprogs \
    efibootmgr \
    gawk \
    gdisk \
    grub2 \
    grub2-efi-x64 \
    grub2-efi-x64-modules \
    grub2-pc \
    haveged \
    kernel \
    kernel-modules \
    kernel-modules-extra \
    livecd-tools \
    nano \
    NetworkManager \
    openssh-server \
    parted \
    polkit \
    rsync \
    shim-x64 \
    squashfs-tools \ 
    sudo \
    systemd \
    systemd-networkd \
    systemd-resolved \
    tar \
    which \
    && dnf clean all

RUN mkdir -p /run/lock
RUN touch /usr/libexec/.keep

# Copy the Kairos framework files. We use master builds here for fedora. See https://quay.io/repository/kairos/framework?tab=tags for a list
COPY --from=quay.io/kairos/framework:master_fedora / /

# Activate Kairos services
RUN systemctl enable cos-setup-reconcile.timer && \
          systemctl enable cos-setup-fs.service && \
          systemctl enable cos-setup-boot.service && \
          systemctl enable cos-setup-network.service

## Generate initrd
RUN kernel=$(ls /boot/vmlinuz-* | head -n1) && \
            ln -sf "${kernel#/boot/}" /boot/vmlinuz
RUN kernel=$(ls /lib/modules | head -n1) && \
            dracut -v -N -f "/boot/initrd-${kernel}" "${kernel}" && \
            ln -sf "initrd-${kernel}" /boot/initrd && depmod -a "${kernel}"
RUN rm -rf /boot/initramfs-*
```

In the Dockerfile, note the following:

- The base image we're using is fedora. However, you could also base your image on other distributions. See [the Kairos official images](https://github.com/kairos-io/kairos/tree/master/images) for an example.
- We're installing a set of packages, including `rsync`, `grub`, `systemd`, `kernel`, and we're generating the initramfs inside the image.
- We're copying the Kairos framework image file to the root of the container. Choose the framework image that closely matches your setup. You can find the framework images published here: https://quay.io/repository/kairos/framework?tab=tags

3. Now build the image with:

```bash
docker build -t test-byoi .
```

## Build bootable assets

Once the container image is built, we can proceed directly to creating an ISO or netboot it using [AuroraBoot](/docs/reference/auroraboot). We can use AuroraBoot to handle the ISO build process and even attach a default cloud config if desired. Here's an example for both scenarios:

{{< tabpane text=true  >}}
{{% tab header="ISO" %}}

We can use [AuroraBoot](/docs/reference/auroraboot) to handle the the ISO build process and optionally attach it a default cloud config, for example:

```bash
docker run -v "$PWD"/build:/tmp/auroraboot \
             -v /var/run/docker.sock:/var/run/docker.sock \
             --rm -ti quay.io/kairos/auroraboot:v0.2.2 \
             --set container_image=docker://test-byoi \
             --set "disable_http_server=true" \
             --set "disable_netboot=true" \
             --set "state_dir=/tmp/auroraboot"
# 2:45PM INF Pulling container image 'test-byoi' to '/tmp/auroraboot/temp-rootfs' (local: true)
# 2:45PM INF Generating iso 'kairos' from '/tmp/auroraboot/temp-rootfs' to '/tmp/auroraboot/iso'
# $ sudo ls -liah build/iso 
# total 449M
# 35142520 drwx------ 2 root root 4.0K Mar  7 15:46 .
# 35142517 drwxr-xr-x 5 root root 4.0K Mar  7 15:42 ..
# 35142521 -rw-r--r-- 1 root root    0 Mar  7 15:45 config.yaml
# 35138094 -rw-r--r-- 1 root root 449M Mar  7 15:46 kairos.iso
```

This will generate an ISO named kairos.iso which will be located at `build/iso/`. You can use either `BalenaEtcher` or `dd` to flash this ISO to a USB stick. Additionally, QEMU can be used to test the ISO:

```bash
qemu-system-x86_64 -m 2048 -drive if=virtio,media=disk,file=build/iso/kairos.iso
```

{{% /tab %}}

{{% tab header="Netboot" %}}

To netboot, we can also use [AuroraBoot](/docs/reference/auroraboot) to handle the process, or refer to [Netboot](/docs/installation/netboot). Here's an example:

```bash
docker run -v --net host \
             -v /var/run/docker.sock:/var/run/docker.sock \
             --rm -ti quay.io/kairos/auroraboot:v0.2.2 \
             --set container_image=docker://test-byoi \
             --set "disable_http_server=true" \
             --set "netboot.cmdline=rd.neednet=1 ip=dhcp rd.cos.disable netboot nodepair.enable console=tty0 selinux=0"
```

{{% /tab %}}
{{< /tabpane >}}

This example is available in the `examples/byoi/fedora` directory of the [Kairos repository](https://github.com/kairos-io/kairos/tree/master/examples/byoi/fedora), where you can run `build.sh` to reproduce it.