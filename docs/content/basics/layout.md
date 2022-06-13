+++
title = "Layout"
date = 2022-02-09T17:56:26+01:00
weight = 2
pre = "<b>- </b>"
+++

c3OS is an immutable Distribution, built with the [Element-toolkit](https://rancher.github.io/elemental-toolkit/docs/).

By default, `c3OS` uses an immutable setup.

A running system will look like as follows:

```
/usr/local - persistent (COS_PERSISTENT)
/oem - persistent (COS_OEM)
/etc - ephemeral
/usr - read only
/ immutable
```

[See also Element docs](https://rancher.github.io/elemental-toolkit/docs/reference/immutable_rootfs/).