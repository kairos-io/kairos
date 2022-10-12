---
layout: "../../layouts/docs/Layout.astro"
title: "Immutable layout"
index: 3
---

# Immutable layout

Kairos adopts an immutable layout and derivatives created with its toolkit inherits the same immutability aspects.

An immutable OS is a carefully engineered system which boots in a restricted, permissionless mode, where certain paths of the system are not writable. For instance, after installation it's not possible to install additional packages in the system, and any configuration change is discarded after reboot.

A running Linux based OS system will look like with the following paths:

```
/usr/local - persistent ( partition label COS_PERSISTENT)
/oem - persistent ( partition label COS_OEM)
/etc - ephemeral
/usr - read only
/ immutable
```

`/usr/local` will contain all the persistent data which will be carried over in-between upgrades, instead, any change to `/etc` will be discarded.

## Benefits of using an Immutable system

There are many reasons why you would like to use an immutable system, and this is a genuine, good question. There are various perspective, one is from a security standpoint. It is far more secure than traditional systems—most of attack vectors relies on writing on the system or either installing persistent tools after a vector has been exploited.

From a maintenance perspective, configuration management tools like Chef, Puppet, or the likes are not needed as immutable systems have only a configuration entry point, every other configuration is cleaned up automatically after a reboot.

The benefit of rolling out the same system over a set of machines are obvious:

- No snowflakes - All the machines ships the same image, configuration settings, and behavior. This allows to have a predictable infrastructure, predictable upgrades, and homogeneous configurations across your cluster.
- Configuration is driven via cloud-init. There is only one source of truth for configuration, and that does happen at bootstrap time. Anything else it's handled afterwards natively via Kubernetes, so no configuration management software is required.
- Reduced attack surface - Immutable systems cannot be modified or tampered on runtime. This enhances the security of a running OS as changes on the system are not allowed.

Tools like Chef, Puppet, and Ansible share the same underlying issues when it comes to configuration management: nodes can have different version matrices of software and OS, which makes your set of nodes dishomogeneous and difficult to maintain and orchestrate from day 1 to day 2.

Kairos tackles the issue from another angle, as can turn _any_ distribution to an "immutable" system, distributed as a standard container image, which gets provisioned to the devices as declared. This allows to treat OSes with the same repeatable portability as containers for apps, removing snowflakes in your cluster. Container registries can be used either internally or externally to the cluster to propagate upgrades with customized versions of the OS (kernel, packages, and so on).

## Design

Kairos after installation will create the following partitions:

- A state partition that stores the container images which are going to be booted (active and passive, stored in `.img` format which are loopback mounted)
- A recovery partition that stores the container images used for recovery (in `.squashfs` format)
- A OEM partition (optional) that stores user configuration and cloud-config files
- A persistent partition to keep the data across reboot

![Kairos-installation-partitioning](https://user-images.githubusercontent.com/2420543/195111190-3bdfb917-312a-40f4-b0bc-4a65a701c06b.png)

The persistent partition is mounted on boot to `/usr/local`, but few mont points are mount-bind to it. The mountpoints are defined in a [cloud-config](https://github.com/kairos-io/kairos/blob/a1a9bef4dff30e0718fa4d2697f075ce37c7ed90/overlay/files/system/oem/11_persistency.yaml#L11) file. It is possible to override such configuration by providing it in the cloud-config provided during installation.

The Recovery system allows to perform emergency tasks in case of failure of the active and passive images, and a fallback mechanism will take place in case of failures such as boots the partitions in this sequence: “A -> B -> Recovery”.

The upgrade happens in a transition image and take place only after all the necessary steps are completed. An upgrade of the ‘A/B’ partitions can be done by [with Kubernetes](/upgrade/kubernetes) or [manually](/upgrade/manual). The upgrade will create a new pristine image that will be selected as active for the next reboot, the old one will be flagged as passive. If we are performing the same from the passive system, only the active is subject to changes.

### Kernel and Initrd

The Kernel and Initrd are loaded from the system images and expected to be present. A typical setup have kernels and initrd in a special boot partition.
In Kairos instead the Kernel and Initrd are being loaded from the images, which are chainloaded from the bootloader (GRUB). This is a design choice to keep the entire OS stack confined as a single layer which gets pulled and swapped atomically during upgrades.
