---
title: "Reset a node"
linkTitle: "Reset"
weight: 4
date: 2022-11-13
description: >
---

Kairos has a recovery mechanism built-in which can be leveraged to restore the system to a known point. At installation time, the recovery partition is created from the installation medium and can be used to restore the system from scratch, leaving configuration and persistent data intact.

The reset will regenerate the bootloader and the images in the COS_STATE partition by using the recovery image.

# How to

It is possible to reset the state of a node by either booting into the "Reset" mode into the boot menu, which automatically will reset the node:

![reset](https://user-images.githubusercontent.com/2420543/191941281-573e2bed-f66c-48db-8c46-e8034417539e.gif?classes=border,shadow)

## Manual reset

It is possible to trigger the reset manually by logging into the recovery from the boot menu and running `kairos reset` from the console.

To optionally tweak the reset process, run `elemental reset` instead which supports options via arg:

| Option              | Description                 |
| ------------------- | --------------------------- |
| --reset-persistent  | Clear persistent partitions |
| --reset-oem         | Clear OEM partitions        |
| --system.uri string | Reset with the given image  |

- **Note**: `--reset-oem` resets the system pruning all the configurations.
- `system.uri` allows to reset using another image or a directory.
  `string` can be among the following: `dir:/path/to/dir`, `oci:<image>`, `docker:<image>`, `channel:<luet package>` or `file:/path/to/file`.
