---
title: "Using core images"
linkTitle: "Using core images"
weight: 4
description: > 
    This section provides examples on how to use Kairos core images
---

Welcome to the Kairos core image documentation! Our core images are released as part of the [kairos-io/kairos](https://github.com/kairos-io/kairos) repository and can be found in the releases section. These images don't include k3s, but can be used either as a base for customizing and creating downstream images or as an installer to pull and deploy other images during installation. In this guide, we'll be focusing on using the core images as an installer to deploy other images. If you're interested in using the core images as a base for customization, check out our [customizing](/docs/advanced/customizing) documentation.


## Installation

To get started with installing a Kairos core image, you'll want to use the artifacts from the [Kairos core](https://github.com/kairos-io/kairos/releases) repository, which don't contain any Kubernetes engine. The only configuration you'll need to apply is to specify the container image you want to deploy in the install.image field of your cloud config file. You can find a list of available images in [our support matrix](/docs/reference/image_matrix). For example, to use an image from the [provider-kairos](https://github.com/kairos-io/provider-kairos) repository, your cloud config might look like this:

```yaml
#cloud-config
install:
 # Here we specify the image that we want to deploy
 image: "docker:quay.io/kairos/kairos-opensuse:v1.4.0-k3sv1.26.0-k3s1"
```

Once you've chosen your image, follow the steps in our [Installation](/docs/installation) documentation to complete the process. After the installation is complete, the configuration in the k3s block will take effect, and you'll be able to use it just as you would with an image from the provider-kairos repository. For example:

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

There are a few key points to note in the configuration file:

- we set `install.image` to the container image that we want to deploy. That can be a [custom image](/docs/advanced/customizing) or an [image from scratch](/docs/reference/build)
- we use the `k3s` block as we normally would do on a provider-kairos image. This is because after installation we would boot in the image specified in the `install.image` field, and thus the configuration would take effect

That's it! With these steps, you should now be able to use Kairos core images as an installer to deploy other container images