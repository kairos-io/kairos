---
title: "Build Kairos from scratch"
linkTitle: "Build Kairos from scratch"
weight: 5
description: >
    This article shows how to bring your own image with Kairos, and build a Kairos derivative from scratch using base container images from popular distributions such as Ubuntu, Fedora, openSUSE, etc.
---

{{% alert title="Note" %}}

This section is reserved for experienced users and advanced use-cases, for instance when building new flavors for Kairos core images or creating your own derivative. 
For most use cases Core and Standard Kairos images are enough, pre-configured, optimized images built following the approach described below, which are pre-built and maintained by the Kairos team.

This process is still a work in progress. You can track our development efforts in the [factory epic](https://github.com/kairos-io/kairos/issues/116). Altough the state is already usable for general consumption, there are features like [conformance tests](https://github.com/kairos-io/kairos/issues/958) to allow users to run tests against images built with this process allowing to verify the correctness of the image built.
{{% /alert %}}

Kairos allows to "bring any distribution" meaning that any base OS image can be used as a source to create a Kairos based distribution, given it satisfies the Kairos model and contract. Every OS will be treated only as a collection of packages - upgrades - and operations are demanded to the Kairos components, which abstracts the management model.

Practically, that means that upgrades are not carried by the package manager of the OS, instead, the `kairos-agent` will take care of upgrading via container images. Upgrades, and installation are all delivered uniquely by container images. Container images are overlayed at boot, so there is no additional runtime overhead, as no container engine is required in order to boot the OS.

The Kairos framework is an abstract layer between the OS and the management interface, which follows an atomic A/B approach - this can be controlled directly by Kubernetes, manually via CLI or with a declarative model. 

The Kairos contract is simple: the OS container image need to have everything needed in order to boot, that goes from the kernel up to the init system.

The contract has few advantages:

- Delegates package maintenance, CVE, and security fixes to the OS layer
- Upgrades to the container images can be issued easily by chaining Dockerfiles, or committing changes to the image manually
- Clearly separation of concerns: the OS provides the booting bits and packages in order for the OS to function. Kairos provides the operational framework to handle the lifecycle of the node, and the immutability interface.
- Allows long term support and maintenance: each framework image allows to convert any OS to the given Kairos framework version, allowing to potentially maintain for how long the base OS support model adheres to.

In this document we will outline the steps in order to use any base image and make it fully bootable with the Kairos framework.

The steps involves roughly:

- Building a container image
  - Selecting a base image from the supported OS family (however, it should work with any distro)
  - Install the required packages from the package manager of the chosen OS
  - Build the initramfs
- Build an offline bootable ISO or Netboot the container image

## Prerequisites

- Docker or a container engine installed locally
- Note, the steps below are tested on Linux, but should work as well in other environments. Please open up issues and help us improve the Documentation!

## Build a container image

Create a directory, and write a simple `Dockerfile` containing what we want in the derivative:

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


Few things we can notice in the Dockerfile:
- We base our image on `fedora`. Similarly we could have based our image on other distributions. See [the Kairos official images](https://github.com/kairos-io/kairos/tree/master/images) for an example
- We install a set of packages. Few of them are actually required, such as: `rsync`, `grub`, `systemd`, `kernel`, and we generate finally the initramfs inside the image
- We copy the Kairos framework images file to the root of the container. Choose the framework image that match closely with your setup. The framework images are published here: https://quay.io/repository/kairos/framework?tab=tags

Now build the image with:

```bash
docker build -t test-byoi .
```

## Build bootable assets

After building the container image, we can directly proceed to create either an ISO or netboot it with [AuroraBoot](/docs/reference/auroraboot):

{{< tabpane text=true  >}}
{{% tab header="ISO" %}}

We can use [AuroraBoot](/docs/reference/auroraboot) to handle the the ISO build process and optionally attach it a default cloud config, for example:

```bash
docker run -v $PWD/build:/tmp/auroraboot \
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
The ISO is available at `build/iso/kairos.iso`, ready to be flashed to an USB stick with `BalenaEtcher` or `dd`.

You can use QEMU to test the ISO:

```bash
qemu-system-x86_64 -m 2048 -drive if=virtio,media=disk,file=build/iso/kairos.iso
```

{{% /tab %}}

{{% tab header="Netboot" %}}

We can use [AuroraBoot](/docs/reference/auroraboot) to handle the the netboot process, or see [Netboot](/docs/installation/netboot):

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


This example is also available in the [Kairos repository](https://github.com/kairos-io/kairos/tree/master/examples/byoi/fedora) in the `examples/byoi/fedora` directory, where you can run `build.sh` to reproduce the example.