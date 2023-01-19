---
title: "Manual installation"
linkTitle: "Manual installation"
weight: 1
date: 2022-11-13
description: >
  Install Kairos manually
---

To install manually, follow the [quickstart](/docs/getting-started). When the QR code is prompted at the screen, you will be able to log in via SSH to the box with the password `kairos` as `kairos` user.

{{% alert title="Note" %}}

**Note**: After the installation, the password login is disabled, users, and SSH keys to log in must be configured via cloud-init.

{{% /alert %}}


## Installation

To start the installation, run from the console the following command:

```bash
sudo kairos-agent manual-install --device "auto" $CONFIG
```

Where the configuration can be a `cloud-init` file or a URL to it:

```yaml
#cloud-init

p2p:
  network_token: ....
# extra configuration
```

**Note**: 
- The command is disruptive and will erase any content on the drive.
- The parameter **"auto"** selects the biggest drive available in the machine.