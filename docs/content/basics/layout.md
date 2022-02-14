+++
title = "Layout"
date = 2022-02-09T17:56:26+01:00
weight = 2
pre = "<b>- </b>"
+++

c3OS is an immutable Distribution, built with the [cOS-toolkit](https://rancher-sandbox.github.io/cos-toolkit-docs/docs/).

By default, `c3OS` uses an immutable setup.

A running system will look like as follows:

```
/usr/local - persistent (COS_PERSISTENT)
/oem - persistent (COS_OEM)
/etc - ephemeral
/usr - read only
/ immutable
```

[See also cOS docs](https://rancher-sandbox.github.io/cos-toolkit-docs/docs/reference/immutable_rootfs/).