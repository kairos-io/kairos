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


Login over SSH as the `c3os` user or via console with `c3os:c3os` and run:

```bash
sudo cos-installer --cloud-init $CONFIG
```

Where the config can be a cloud-init file or a URL to it:

```yaml
#cloud-init

c3os:
  network_token: ....

# extra configuration
```

## Manual K3s configuration

Automatic nodes configuration can be disabled by not specifying a `network_token` in the configuration file.

In that case no VPN and either k3s is configured automatically, see also the [examples](https://github.com/c3os-io/c3os/tree/master/examples) folder in the repository to configure k3s manually.
