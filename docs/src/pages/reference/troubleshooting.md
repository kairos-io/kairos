---
layout: "../../layouts/docs/Layout.astro"
title: "Troubleshooting"
index: 4
---

# Troubleshooting

Things can go wrong, this section tries to give guidelines in helping out identify potential issues.

It is important first to check out if your issue was already submitted [in the issue tracker](https://github.com/kairos-io/kairos/issues)

## Gathering logs

To gather useful logs and help developers spot right away issues, it's suggested to boot with `console=tty0 rd.debug` enabled for example:

![debug](https://user-images.githubusercontent.com/2420543/191934926-7d4ac908-9a4c-4ef4-9891-75820e6b8fe6.gif)

To edit the boot commands, type 'e' in the boot menu. To boot with the changes press 'CTRL+X'.

In case logs can't be acquired, taking screenshot or videos while opening up issues it's strongly reccomended!

## Initramfs breakpoints

Initramfs can be instructed to drop a shell in various phases of the boot process, for instance:

- `rd.break=pre-mount rd.shell`: Drop a shell before setting up mount points
- `rd.break=pre-pivot rd.shell`: Drop a shell before switch-root

## Disable immutability

It is possible to disable immutability by adding `rd.cos.debugrw` to the kernel boot commands

## Root permission

By default, there is no root user set. A default user (`kairos`) is created and can use `sudo` without password authentication during LiveCD bootup.

## Get back the kubeconfig

On all nodes which are deployed with the p2p full-mesh feature of the cluster it's possible to invoke `kairos get-kubeconfig` to recover the kubeconfig file.

## See also

- [Dracut debug docs](https://fedoraproject.org/wiki/How_to_debug_Dracut_problems)
- [Elemental troubleshooting docs](https://rancher.github.io/elemental-toolkit/docs/reference/troubleshooting/)
