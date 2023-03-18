---
title: "QR Code"
linkTitle: "QR Code"
weight: 1
date: 2022-11-13
description: >
  Use the QR code displayed at boot to drive the installation
---

{{% alert title="Warning" %}}
You will need a Standard Kairos OS image in order to use QR Code feature.
{{% /alert %}}

By default Kairos will display a QR code after booting the ISO to install the machine:

![livecd](https://user-images.githubusercontent.com/2420543/189219806-29b4deed-b4a1-4704-b558-7a60ae31caf2.gif)


To trigger the installation process via QR code, you need to use the Kairos CLI and provide a Cloud Config, as described in the [Getting started guide](/docs/getting-started). You can also see some Cloud Config examples in our [Examples section](/docs/examples). The CLI is currently available only for Linux and Windows. It can be downloaded from the release artifact:

```bash
VERSION=$(wget -q -O- https://api.github.com/repos/kairos-io/provider-kairos/releases/latest | jq -r '.tag_name')
curl -L https://github.com/kairos-io/provider-kairos/releases/download/${VERSION}/kairos-cli-${VERSION}-Linux-x86_64.tar.gz -o - | tar -xvzf - -C .
```

The CLI allows to register a node with a screenshot, an image, or a token. During pairing, the configuration is sent over, and the node will continue the installation process.

In a terminal window from your desktop/workstation, run:

```
kairos register --reboot --device /dev/sda --config config.yaml
```

- The `--reboot` flag will make the node reboot automatically after the installation is completed.
- The `--device` flag determines the specific drive where Kairos will be installed. Replace `/dev/sda` with your drive. Any existing data will be overwritten, so please be cautious.
- The `--config` flag is used to specify the config file used by the installation process.

{{% alert title="Note" %}}
By default, the CLI will automatically take a screenshot to get the QR code. Make sure it fits into the screen. Alternatively, an image path or a token can be supplied via arguments (e.g. `kairos register /img/path` or `kairos register <token>`).
{{% /alert %}}

After a few minutes, the configuration is distributed to the node and the installation starts. At the end of the installation, the system is automatically rebooted.

