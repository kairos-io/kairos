+++
title = "Manual installation"
date = 2022-02-09T17:56:26+01:00
weight = 1
chapter = false
pre = "<b>- </b>"
+++

To install manually, follow the [quickstart](/quickstart/installation). When the QR code is prompted at the screen, you should be able to login via SSH to the box with the password `kairos` as `kairos` user.

{{% notice note %}}

Note, after the installation the password login is disabled, users and ssh keys to login must be configured via cloud-init.

{{% /notice %}}

## Installation

To start the installation, run from the console:

```bash
sudo elemental install --device /dev/sda --cloud-init $CONFIG
```

Where the config can be a cloud-init file or a URL to it:

```yaml
#cloud-init

kairosos:
  network_token: ....

# extra configuration
```
