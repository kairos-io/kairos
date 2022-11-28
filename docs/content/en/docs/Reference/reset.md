---
title: "Reset a node"
linkTitle: "Reset"
weight: 4
date: 2022-11-13
description: >
---

Kairos has a recovery mechanism built-in which can be leveraged to restore the system to a known point. At installation time, the recovery partition is created from the installation medium and can be used to restore the system from scratch, leaving configuration intact and cleaning any persistent data accumulated by usage in the host (e.g. Kubernetes images, persistent volumes, etc. ).

The reset action will regenerate the bootloader configuration and the images in the state partition (labeled `COS_STATE`) by using the recovery image generated at install time, cleaning up the host. 

The configuration files in `/oem` are kept intact, the node on the next reboot after a reset will perform the same boot sequence (again) of a first-boot installation.

# How to

{{% alert title="Note" %}}

By following the steps below you will _reset_ entirely a node and the persistent data will be lost. This includes _every_ user-data stored on the machine.

{{% /alert %}}

The reset action can be accessed via the Boot menu, remotely, triggered via Kubernetes or manually. In each scenario the machine will reboot into reset mode, perform the cleanup, and reboot automatically afterwards.

## From the boot menu

It is possible to reset the state of a node by either booting into the "Reset" mode into the boot menu, which automatically will reset the node:

![reset](https://user-images.githubusercontent.com/2420543/191941281-573e2bed-f66c-48db-8c46-e8034417539e.gif?classes=border,shadow)

## Remotely, via command line

On a Kairos booted system, logged as root:

```bash
$ grub2-editenv /oem/grubenv set next_entry=statereset
$ reboot
```

## From Kubernetes

`system-upgrade-controller` can be used to apply a plan to the nodes to use Kubernetes to schedule the reset on the nodes itself, similarly on how upgrades are applied. 

Consider the following example which resets a machine by changing the config file used during installation:
```yaml
---
apiVersion: v1
kind: Secret
metadata:
  name: custom-script
  namespace: system-upgrade
type: Opaque
stringData:
  config.yaml: |
    #cloud-config
    hostname: testcluster-{{ trunc 4 .MachineID }}
    k3s:
      enabled: true
    users:
    - name: kairos
      passwd: kairos
      ssh_authorized_keys:
      - github:mudler
  add-config-file.sh: |
    #!/bin/sh
    set -e
    if diff /host/run/system-upgrade/secrets/custom-script/config.yaml /host/oem/90_custom.yaml >/dev/null; then
        echo config present
        exit 0
    fi
    # we can't cp, that's a symlink!
    cat /host/run/system-upgrade/secrets/custom-script/config.yaml > /host/oem/90_custom.yaml
    grub2-editenv /host/oem/grubenv set next_entry=statereset
    sync

    mount --rbind /host/dev /dev
    mount --rbind /host/run /run
    nsenter -i -m -t 1 -- reboot
    exit 1
---
apiVersion: upgrade.cattle.io/v1
kind: Plan
metadata:
  name: reset-and-reconfig
  namespace: system-upgrade
spec:
  concurrency: 2
  # This is the version (tag) of the image.
  # The version is refered to the kairos version plus the k3s version.
  version: "v1.0.0-rc2-k3sv1.23.9-k3s1"
  nodeSelector:
    matchExpressions:
      - { key: kubernetes.io/hostname, operator: Exists }
  serviceAccountName: system-upgrade
  cordon: false
  upgrade:
    # Here goes the image which is tied to the flavor being used.
    # Currently can pick between opensuse and alpine
    image: quay.io/kairos/kairos-opensuse
    command:
      - "/bin/bash"
      - "-c"
    args:
      - bash /host/run/system-upgrade/secrets/custom-script/add-config-file.sh
  secrets:
    - name: custom-script
      path: /host/run/system-upgrade/secrets/custom-script
```

## Manual reset

It is possible to trigger the reset manually by logging into the recovery from the boot menu and running `kairos reset` from the console.

To optionally change the behavior of the reset process (such as cleaning up also configurations), run `elemental reset` instead which supports options via arg:

| Option              | Description                 |
| ------------------- | --------------------------- |
| --reset-persistent  | Clear persistent partitions |
| --reset-oem         | Clear OEM partitions        |
| --system.uri string | Reset with the given image  |

- **Note**: `--reset-oem` resets the system pruning all the configurations.
- `system.uri` allows to reset using another image or a directory.
  `string` can be among the following: `dir:/path/to/dir`, `oci:<image>`, `docker:<image>`, `channel:<luet package>` or `file:/path/to/file`.
