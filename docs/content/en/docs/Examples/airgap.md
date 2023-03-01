---
title: "How to Create an Airgap K3s Installation with Kairos"
linkTitle: "Airgapped ISO with AuroraBoot"
weight: 4
description: > 
    This section describe examples on how to use AuroraBoot and Kairos bundles to create ISOs for airgapped installs
---

If you want to create an [airgap K3s installation](https://docs.k3s.io/installation/airgap), Kairos provides a convenient way to do so using AuroraBoot. In this guide, we will go through the process of creating a custom ISO of Kairos that contains a configuration file and a [bundle](https://kairos.io/docs/advanced/bundles/) that executes preparatory steps after installation. The bundle will overlay new files in the system and prepare the node for having an airgapped K3s installation.

{{% alert title="Note" %}}
If you already have a Kubernetes cluster, you can use the osbuilder controller to generate container images with your additional files already inside.
{{% /alert %}}

## Prerequisites

Docker running in the host

## Creating the Bundle

First, we need to create a bundle that contains the K3s images used for the airgap installation. The bundle will place the images in the `/var/lib/rancher/k3s/agent/images` directory. The `/var/lib/rancher` is already configured as persistent by Kairos defaults and every change to that directory persist reboots. You can add additional persistent paths in the system with [the cloud config](https://kairos.io/docs/advanced/customizing/#bind-mounts)

1. Create a new directory named `images-bundle`, and create a new file inside it called `Dockerfile`.
2. Paste the following code into the `Dockerfile`:

```Dockerfile
FROM alpine
WORKDIR /build
RUN wget https://github.com/k3s-io/k3s/releases/download/v1.23.16%2Bk3s1/k3s-airgap-images-amd64.tar.gz

FROM scratch
COPY ./run.sh /
COPY --from=alpine /build/k3s-airgap-images-amd64.tar.gz /assets
```
3. Create a new file called `run.sh` inside the `images-bundle` directory, and paste the following code:

```bash
#!/bin/bash

mkdir -p /usr/local/.state/var-lib-rancher.bind/k3s/agent/images/
cp -rfv ./k3s-airgap-images-amd64.tar.gz /usr/local/.state/var-lib-rancher.bind/k3s/agent/images/
```
4. Make the `run.sh` file executable by running the following command: 
```bash
chmod +x run.sh
```
5. Build the container image by running the following command inside the images-bundle directory. This will save the image as `data/bundle.tar`:
```bash
docker build -t images-bundle .
```
6. Save the bundle:

```
$ ls
images-bundle

# create a directory
$ mkdir data
$ docker save images-bundle -o data/bundle.tar
```

## Building the Offline ISO for Airgap

Now that we have created the bundle, we can use it to build an offline ISO for the airgap installation.

1. Create a cloud config for the ISO and save it as config.yaml. The config.yaml file should contain your cloud configuration for Kairos and is used to set up the system when it is installed. An example can be:
```yaml
#cloud-config

install:
 auto: true
 device: "auto"
 reboot: true
 bundles:
  # This bundle needs to run after-install as it consumes assets from the LiveCD
  # which is not accessible otherwise at the first boot (there is no live-cd with any bundle.tar)
 - targets:
   - run:///run/initramfs/live/bundle.tar
   local_file: true

# Define the user accounts on the node.
users:
- name: "kairos"                       # The username for the user.
  passwd: "kairos"                      # The password for the user.
  ssh_authorized_keys:                  # A list of SSH keys to add to the user's authorized keys.
  - github:mudler                       # A key from the user's GitHub account.

k3s:
  enabled: true
```

2. Build the ISO with [AuroraBoot](/docs/reference/auroraboot) by running the following command:


```bash
IMAGE=quay.io/kairos/kairos-opensuse-leap:v1.6.1-k3sv1.26.1-k3s1

docker pull $IMAGE

docker run -v $PWD/config.yaml:/config.yaml \
             -v $PWD/build:/tmp/auroraboot \
             -v /var/run/docker.sock:/var/run/docker.sock \
             -v $PWD/data:/tmp/data \
             --rm -ti quay.io/kairos/auroraboot:v0.2.0 \
             --set "disable_http_server=true" \
             --set "disable_netboot=true" \
             --set "container_image=docker://$IMAGE" \
             --set "iso.data=/tmp/data" \
             --cloud-config /config.yaml \
             --set "state_dir=/tmp/auroraboot"
```

The resulting ISO should be available at: `build/iso/kairos.iso`

This example is also available in the [Auroraboot repository](https://github.com/kairos-io/AuroraBoot/tree/master/examples/airgap) in the `examples/airgap` directory, where you can run `build_docker.sh` to reproduce the example.

## See also

- [Customize the OS image](/docs/advanced/customizing/)
- [Live layer bundles](/docs/advanced/livelayering/)
- [Create ISOs with Kubernetes](/docs/installation/automated/#kubernetes)
- [Bundles reference](https://kairos.io/docs/advanced/bundles/)