---
title: "Using core images"
linkTitle: "Using core images"
weight: 4
description: > 
    This section provides examples on how to use Kairos core images
---

Kairos core images are part of the [assets released](https://github.com/kairos-io/kairos/releases) by the [kairos-io/kairos](https://github.com/kairos-io/kairos) repository. The images don't include k3s - such images can be used either as base for customizing and creating downstream images, or either as installer to pull during installation and deploy other images. This examples show how to use core images standalone as installer to deploy other images. To use the image as base for customization, see [customizing](/docs/advanced/customizing).

## Installation

Use the [Kairos core](https://github.com/kairos-io/kairos/releases) artifacts which doesn't contain any Kubernetes engine, the only configuration section that needs to be applied is the portion about the container image:

```yaml
#cloud-config
install:
 # Here we specify the image that we want to deploy
 image: "docker:<image>"
```

You can pick an `<image>` from [our support matrix](/docs/reference/image_matrix), in the example below we will use an image from [provider-kairos](https://github.com/kairos-io/provider-kairos).

Follow the [Installation](/docs/installation) documentation, and use the following cloud config file:

```yaml
#cloud-config

install:
 device: "auto"
 auto: true
 reboot: true
 # Here we specify the image that we want to deploy
 image: "docker:quay.io/kairos/kairos-opensuse:v1.4.0-k3sv1.26.0-k3s1"

hostname: "test"
users:
- name: "kairos"
  passwd: "kairos"
  ssh_authorized_keys:
  - github:mudler

k3s:
  enable: true
```

Notably:

- we set `install.image` to the container image that we want to deploy. That can be a [custom image](/docs/advanced/customizing) or an [image from scratch](/docs/reference/build)
- we use the `k3s` block as we normally would do on a provider-kairos image. This is because after installation we would boot in the image specified in the `install.image` field, and thus the configuration would take effect