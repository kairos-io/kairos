---
layout: "../../layouts/docs/Layout.astro"
title: "Immutable layout"
index: 3
---

# Immutable layout

Kairos adopts an immutable layout and derivatives created with its toolkit inherits the same immutability aspects.

An immutable OS is a carefully engineered system which boots in a restricted, permissionless mode, where certain paths of the system are not writable. For instance, after installation it's not possible to install additional packages in the system, and any configuration change is discarded after reboot.

A running Linux-based OS system will look like with the following paths:

```
/usr/local - persistent ( partition label COS_PERSISTENT)
/oem - persistent ( partition label COS_OEM)
/etc - ephemeral
/usr - read only
/ immutable
```

`/usr/local` will contain all the persistent data which will be carried over in-between upgrades, unlike the changes made to `/etc` which will be discarded.

## Benefits of using an Immutable System

There are many reasons why you would like to use an immutable system, and this is genuinely, a good question. There are various perspectives, one is from a security standpoint. It is far more secure than traditional systems—most of attack vectors relies on writing on the system or either installing persistent tools after a vector has been exploited.

From a maintenance perspective, configuration management tools like Chef, Puppet, or the likes are not needed as immutable systems and have only a configuration entry point, every other configuration is cleaned up automatically after a reboot.

The benefit of rolling out the same system over a set of machines are obvious:

- No snowflakes - All the machines ships the same image, configuration settings, and behavior. This allows to have a predictable infrastructure, predictable upgrades, and homogeneous configurations across your cluster.
- Configuration is driven via cloud-init. There is only one source of truth for the configuration, and that happens at bootstrap time. Anything else is handled afterwards—natively via Kubernetes, so no configuration management software is required.
- Reduced attack surface - Immutable systems cannot be modified or tampered at runtime. This enhances the security of a running OS, as changes on the system are not allowed.

Tools like Chef, Puppet, and Ansible share the same underlying issues when it comes to configuration management: nodes can have different version matrices of software and OS, which makes your set of nodes dishomogeneous and difficult to maintain and orchestrate from day 1 to day 2.

Kairos tackles the issue from another angle, as it can turn _any_ distribution to an "immutable" system, distributed as a standard container image, which gets provisioned to the devices as declared. This allows to treat OSes with the same repeatable portability as containers for apps, removing snowflakes in your cluster. Container registries can be used either internally or externally to the cluster to propagate upgrades with customized versions of the OS (kernel, packages, and so on).
