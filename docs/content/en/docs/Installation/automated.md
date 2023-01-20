---
title: "Automated"
linkTitle: "Automated"
weight: 3
date: 2022-11-13
description: >
  Install Kairos automatically, with zero touch provisioning
---

To automate Kairos installation, you can configure a specific portion of the installation configuration file. The configuration file can then be supplied in a few different ways, such as creating an additional ISO to mount, specifying a URL, or even creating an ISO from a container image with an embedded configuration file.

Here's an example of how you might customize the install block:

```yaml
install:
  # Device for automated installs
  device: "/dev/sda"
  # Reboot after installation
  reboot: true
  # Power off after installation
  poweroff: true
  # Set to true to enable automated installations
  auto: true
  # A list of bundles
  bundles:
    -  quay.io/kairos/packages:k9s-utils-0.26.7
```

This block allows you to specify the device on which to install Kairos, whether to reboot or power off after installation, and which bundles to include.

## Data source

To supply your Kairos configuration file, you can create an ISO that contains both a user-data file (which contains your configuration) and a meta-data file.

Here's an example `user-data` configuration that is set up to automatically install Kairos onto /dev/sda and reboot after installation:

```yaml
#cloud-config

install:
  device: "/dev/sda"
  reboot: true
  poweroff: false
  auto: true # Required, for automated installations

kairos:
  network_token: ....
# extra configuration
```

Save this file as `cloud_init.yaml`, then create an ISO with the following steps:

1. Create a new directory and navigate to it:
```bash
$ mkdir -p build
$ cd build
```
2. Create empty `meta-data` and copy your config as `user-data`:
```bash
$ touch meta-data
$ cp -rfv cloud_init.yaml user-data
```
3. Use `mkisofs` to create the ISO file:
```bash
$ mkisofs -output ci.iso -volid cidata -joliet -rock user-data meta-data
```

Once the ISO is created, you can attach it to your machine and boot up as usual, along with the Kairos ISO.

## Via config URL

Another way to supply your Kairos configuration file is to specify a URL as a boot argument during startup. To do this, add `config_url=<URL>` as a boot argument. This will allow the machine to download your configuration from the specified URL and perform the installation using the provided settings.

After installation, the configuration will be available on the system at `/oem/90_custom.yaml`.

If you're not sure where to host your configuration file, a common option is to upload it as a GitHub gist.

## ISO remastering

It is possible to create custom ISOs with an embedded cloud configuration. This allows the machine to automatically boot with a pre-specified configuration file, which will be installed on the system after provisioning is complete.


### Locally

To create a custom ISO, you will need Docker installed on your machine. 

Here's an example of how you might do this:

```bash
$ IMAGE=<source/image>
$ mkdir -p files-iso/boot/grub2
# You can replace this step with your own grub config. This GRUB configuration is the boot menu of the ISO
$ wget https://raw.githubusercontent.com/kairos-io/kairos/master/overlay/files-iso/boot/grub2/grub.cfg -O files-iso/boot/grub2/grub.cfg
# Copy the config file
$ cp -rfv cloud_init.yaml files-iso/cloud_config.yaml
# Pull the image locally
$ docker pull $IMAGE
# Optionally, modify the image here!
# docker run --entrypoint /bin/bash --name changes -ti $IMAGE
# docker commit changes $IMAGE
# Build an ISO with $IMAGE
$ docker run -v $PWD:/cOS -v /var/run/docker.sock:/var/run/docker.sock -i --rm quay.io/kairos/osbuilder-tools:latest --name "custom-iso" --debug build-iso --date=false --local --overlay-iso /cOS/files-iso $IMAGE --output /cOS/
```

This will create a new ISO with your specified cloud configuration embedded in it. You can then use this ISO to boot your machine and automatically install Kairos with your desired settings.

You can as well modify the image in this step and add additional packages before deployment. See [customizing the system image](/docs/advanced/customizing).

### Kubernetes

It is possible to create custom ISOs and derivatives using extended Kubernetes API resources with an embedded configuration file. This allows you to drive automated installations and customize the container image without breaking the concept of immutability.

To do this, you will need a Kubernetes cluster. Here's an example of how you might use Kubernetes to create a custom ISO with Kairos:


1. Add the Kairos Helm repository:
```bash
$ helm repo add kairos https://Kairos-io.github.io/helm-charts
"kairos" has been added to your repositories
```
2. Update your Helm repositories:
```bash
$ helm repo update
Hang tight while we grab the latest from your chart repositories...
...Successfully got an update from the "kairos" chart repository
Update Complete. ⎈Happy Helming!⎈
```
3. Install the Kairos CRD chart:
```bash
$ helm install kairos-crd kairos/kairos-crds
NAME: kairos-crd
LAST DEPLOYED: Tue Sep  6 20:35:34 2022
NAMESPACE: default
STATUS: deployed
REVISION: 1
TEST SUITE: None
```
4. Install the Kairos `osbuilder` chart:
```bash
$ helm install kairos-osbuilder kairos/osbuilder
NAME: kairos-osbuilder
LAST DEPLOYED: Tue Sep  6 20:35:53 2022
NAMESPACE: default
STATUS: deployed
REVISION: 1
TEST SUITE: None
```
5. Use `kubectl` to apply an `OSArtifact` spec:
```bash
cat <<'EOF' | kubectl apply -f -
apiVersion: build.kairos.io/v1alpha1
kind: OSArtifact
metadata:
  name: hello-kairos
spec:
  imageName: "quay.io/kairos/core-opensuse:latest"
  iso: true
  bundles:
  # Bundles available at: https://packages.kairos.io/Kairos/
  - quay.io/kairos/packages:helm-utils-3.10.1
  cloudConfig: |
            #cloud-config
            users:
            - name: "kairos"
              passwd: "kairos"
            install:
              device: "auto"
              reboot: true
              poweroff: false
              auto: true # Required, for automated installations
EOF
```

This will create a new ISO with Kairos and the specified bundles included. You can then use this ISO to boot your machine and automatically install Kairos with the specified configuration.

Note: If you're using kind, you'll need to use the IP address and port of the nginx service to access the ISO. You can get this with:

```bash
# Note on running with kind:
$ IP=$(docker inspect kind-control-plane | jq -r '.[0].NetworkSettings.Networks.kind.IPAddress')
$ PORT=$(kubectl get svc osartifactbuilder-operator-osbuilder-nginx -o json | jq '.spec.ports[0].nodePort')
$ curl http://$IP:$PORT/hello-kairos.iso -o test.iso
```

Check out the [dedicated section in the documentation](/docs/advanced/build) for further examples.
