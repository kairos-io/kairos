---
title: "Cloud init based"
linkTitle: "Cloud init based"
weight: 3
date: 2022-11-13
description: >
---

Kairos supports the [standard cloud-init syntax](https://github.com/mudler/yip#compatibility-with-cloud-init-format) and [its own extended syntax](https://github.com/mudler/yip) to allow to configure a system declaratively with a cloud-config centric approach.

If you are not familiar with the concepts of cloud-init, [official cloud-init](https://cloud-init.io/) is a recommended read.

## Configuration persistency

Kairos is an Immutable OS and the only configuration that is persistent across reboots is the cloud-init configuration.
Multiple cloud-init files can be present in the system and Kairos will read them and process them in sequence (lexicographic order) allowing to extend the configuration with additional pieces also after deployment, or to manage logical configuration pieces separately.

In Kairos the `/oem` directory keeps track of all the configuration of the system and stores the configuration files. Multiple files are allowed and they are all executed during the various system stages. `/usr/local/cloud-config` can be optionally used as well to store cloud config files in the persistent partition instead. `/system/oem` is instead reserved to default cloud-init files that are shipped by the base OS image.

By using the standard cloud-config syntax, a subset of the functionalities are available and the settings will be executed in the boot stage.

## Boot stages

During boot the stages are emitted in an event-based pattern until a system completes its boot process 

![Kairos-boot-events](https://user-images.githubusercontent.com/2420543/195111193-3167eab8-8058-4676-a1a0-f64aea745646.png)

The events can be used in the cloud-config extended syntax to hook into the various stages, which can allow to hook inside the different stages of a node lifecycle.

For instance, to execute something before reset is sufficient to add the following to the config file used to bootstrap a node:

```yaml
name: "Run something before reset"
stages:
   before-reset:
     - name: "Setting"
       commands:
       - | 
          echo "Run a command before reset the node!"

```

Below there is a detailed list of the stages available that can be used in the cloud-init configuration files:

| **Stage**              | **Description**                                                                                                                                                                                                                                                                    |
|------------------------|------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| _rootfs_               | This is the earliest stage, running before switching root, just right after the root is mounted in /sysroot and before applying the immutable rootfs configuration. This stage is executed over initrd root, no chroot is applied.                                                 |
| _initramfs_            | This is still an early stage, running before switching root. Here you can apply radical changes to the booting setup of Elemental. Despite this is executed before switching root this exection runs chrooted into the target root after the immutable rootfs is set up and ready. |
| _boot_                 | This stage is executed after initramfs has switched root, during the systemd bootup process.                                                                                                                                                                                       |
| _fs_                   | This stage is executed when fs is mounted and is guaranteed to have access to the state and persistent partitions ( `COS_STATE`  and  `COS_PERSISTENT` respectively).                                                                                                              |
| _network_              | This stage is executed when network is available                                                                                                                                                                                                                                   |
| _reconcile_            | This stage is executed 5m after boot and periodically each 60m.                                                                                                                                                                                                                    |
| _after-install_        | This stage is executed after installation of the OS has ended                                                                                                                                                                                                                      |
| _after-install-chroot_ | This stage is executed after installation of the OS has ended.                                                                                                                                                                                                                     |
| _after-upgrade_        | This stage is executed after upgrade of the OS has ended.                                                                                                                                                                                                                          |
| _after-upgrade-chroot_ | This stage is executed after upgrade of the OS has ended (chroot call).                                                                                                                                                                                                            |
| _after-reset_          | This stage is executed after reset of the OS has ended.                                                                                                                                                                                                                            |
| _after-reset-chroot_   | This stage is executed after reset of the OS has ended (chroot call).                                                                                                                                                                                                              |
| _before-install_       | This stage is executed before installation                                                                                                                                                                                                                                         |
| _before-upgrade_       | This stage is executed before upgrade                                                                                                                                                                                                                                              |
| _before-reset_         | This stage is executed before reset                                                                                                                                                                                                                                                |

Note: Steps executed at the `chroot` stage are running inside the new OS as chroot, allowing to write persisting changes to the image, for example by downloading and installing additional software.


### Sentinels

When a Kairos boots it creates sentinel files in order to allow to execute cloud-init steps programmaticaly.

- /run/cos/recovery_mode is being created when booting from the recovery partition
- /run/cos/live_mode is created when booting from the LiveCD

To execute a block using the sentinel files you can specify: `if: '[ -f "/run/cos/..." ]'`, for instance: