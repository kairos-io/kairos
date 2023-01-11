---
title: "Bundles"
linkTitle: "Bundles"
weight: 5
description: >
    Bundles are a powerful feature of Kairos that allow you to customize and configure your operating system. This section explains how to use and build custom bundles.

---

Bundles are a powerful feature of Kairos that allow you to customize and configure your operating system, as well as your Kubernetes cluster. Whether you want to add custom logic, install additional packages, or make any other changes to your system, bundles make it easy to apply these changes after installation or before bootstrapping a node.

Bundles are container images containing only files (and not full OS) that can be used to install new software or extend the cloud-init syntax. You can find community-supported bundles in the [community-bundles](https://github.com/kairos-io/community-bundles) repository.

## Consuming Bundles

To use a bundle in your Kairos configuration, you will need to specify the type of bundle and the target image in your cloud-config file.

To apply a bundle before Kubernetes starts, you can include it in your config like this:

```yaml
#cloud-config

bundles:
- targets:
  - run://<image>
```

Replace `<image>` with the URL or path to the bundle image. The prefix (e.g. `run://`) indicates the type of bundle being used.

To install a bundle after installation instead (for those bundles that explicitly supports that), use the following:

```yaml
#cloud-config
install:
 bundles:
 - targets:
   - run://<image>
```

One of the benefits of using bundles is that they can also extend the cloud-config keywords available during installation. This means that by adding bundles to your configuration file, you can add new blocks of configuration options and customize your system even further.

A full config using a bundle from [community-bundles](https://github.com/kairos-io/community-bundles) that configures `metalLB` might look like this:

```yaml
#cloud-config

hostname: kairoslab-{{ trunc 4 .MachineID }}
users:
- name: kairos
  ssh_authorized_keys:
  # Add your github user here!
  - github:mudler

k3s:
  enable: true
  args:
  - --disable=servicelb

# Specify the bundle to use
bundles:
- targets:
  - run://quay.io/kairos/community-bundles:metallb_latest

# Specify metallb settings
metallb:
  version: 0.13.7
  address_pool: 192.168.1.10-192.168.1.20
```

## Bundle types

Bundles can carry also binaries that can be overlayed in the rootfs, either while [building images](/docs/advanced/build) or with [Live layering](https://kairos.io/docs/advanced/livelayering/).


Kairos supports three types of bundles:

- *Container*: This type is a bare container that simply contains files that need to be copied to the system. It is useful for copying over configuration files, scripts, or any other static content that you want to include on your system.

- *Run*: This type is also a bare container, but it comes with a script that can be run during the installation phase to add custom logic. This is useful for performing any necessary setup or configuration tasks that need to be done before the cluster is fully deployed.

- *Package*: This type is a [luet](https://luet.io) package that will be installed in the system. It requires the luet repository definition in order to work. Luet packages are a powerful way to manage dependencies and install software on your system.

Note: In the future, Kairos will also support a local type for use in airgap situations, where you can pre-add bundles to the image before deployment.

It's important to note that bundles do not have any special meaning in terms of immutability. They install files over paths that are mutable in the system, as they are simply overlaid during the boot process. This means that you can use bundles to make changes to your system at any time, even after it has been deployed.


## Create bundles

To build your own bundle, you will need to create a Dockerfile and any necessary files and scripts. A bundle is simply a container image that includes all the necessary assets to perform a specific task.

To create a bundle, you will need to define a base image and copy over any necessary files and scripts to the image. For example, you might use the following Dockerfile to create a bundle image that deploys everything inside `assets` in the Kubernetes cluster:

```Dockerfile
FROM alpine
COPY ./run.sh /
COPY ./assets /assets
```

And the associated `run.sh` that installs the assets depending on a cloud-config keyword can be:

```bash
#!/bin/bash

K3S_MANIFEST_DIR="/var/lib/rancher/k3s/server/manifests/"

mkdir -p $K3S_MANIFEST_DIR

# IF the user sets `example.enable` in the input cloud config, we install our assets
if [ "$(kairos-agent config get example.enable | tr -d '\n')" == "true" ]; then
  cp -rf assets/* $K3S_MANIFEST_DIR
fi
```

This Dockerfile creates an image based on the Alpine base image, and copies over a script file and some assets to the image. 
You can then add any additional instructions to the Dockerfile to install necessary packages, set environment variables, or perform any other tasks required by your bundle.

Once you have created your Dockerfile and any necessary script files, you can build your bundle image by running docker build and specifying the path to your Dockerfile. 

For example:

```bash
docker build -t <image> .
```

This command will build an image with the name "my-bundle" based on the instructions in your Dockerfile.

After building your bundle image, you will need to push it to a registry so that it can be accessed by Kairos. You can use a public registry like Docker Hub. To push your image to a registry, use the docker push command. For example:

```bash
docker push <image>
```

This will push the "my-bundle" image to your specified registry.

And use it with Kairos:

```yaml
#cloud-config

bundles:
- targets:
  - run://<image>

example:
  enable: true
```

See the [community-bundles repository](https://github.com/kairos-io/community-bundles) for further examples.