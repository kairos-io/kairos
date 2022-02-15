+++
title = "Manual installation"
date = 2022-02-09T17:56:26+01:00
weight = 3
chapter = false
pre = "<b>- </b>"
+++

Manual installation is available as well aside of pairing and automated installation. 

## Default credentials

If needed to connect over ssh, the system have an hardcoded username/password when booting from the LiveCD:

```
user: c3os
pass: c3os
```

{{% notice note %}}

Note, after the installation the password login is disabled, so users and ssh keys to login must be configured via cloud-init.

{{% /notice %}}


Login over SSH or via console and run:

```bash
cos-installer --config $CONFIG
```

Where the config can be a cloud-init file:

```yaml
#cloud-init

c3os:
  network_token: ....

# extra configuration
```

## Manual K3s configuration

Automatic nodes configuration can be disabled by not specifying a `network_token` in the configuration file.

In that case no VPN and k3s is configured automatically, see the [examples](https://github.com/mudler/c3os/tree/master/examples) to configure k3s manually.
