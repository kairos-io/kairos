---
layout: "../../layouts/docs/Layout.astro"
title: "Automated installation"
index: 4
---

It is possible to drive the installation automatically by configuring a specific portion of the configuration file (`install`).
The configuration file can be supplied then in various way, by either creating an additional ISO to mount ( if a VM, or burn to USB stick if baremetal), specifying a config via URL or even create a ISO from a container image with an embedded config file, which we are going to explore here.

The `install` block can be used to customize the installation drive, reboot or shutdown, and additional bundles, for example:

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
    - quay.io/kairos/packages/...
```

## Datasource

The configuration file can be provided to kairos by mounting an ISO in the node with the `cidata` label. The ISO must contain a `user-data` (which contain your configuration) and `meta-data` file.

Consider a cloud-init of the following content, which is configured to automatically install onto `/dev/sda` and reboot:

```yaml
#node-config

install:
  device: "/dev/sda"
  reboot: true
  poweroff: false
  auto: true # Required, for automated installations

kairos:
  network_token: ....
# extra configuration
```

Save it as `cloud_init.yaml`, and we will now create an ISO with it.

To create an ISO as datasource, run the following:

```bash
$ mkdir -p build
$ cd build
$ touch meta-data
$ cp -rfv cloud_init.yaml user-data
$ mkisofs -output ci.iso -volid cidata -joliet -rock user-data meta-data
```

Now the iso is ready to be attached as the CDROM to the machine, boot it up as usual along with the kairos iso.

## Via config URL

It is possible to specify `config_url=<URL>` as boot argument during boot. This will let the machine pull down the configuration specified via URL and perform the installation with the configuration specified. The config will be available in the system after installation as usual at `/oem/99_custom.yaml`.

If you don't know where to upload such config, it is common habit upload those as Github gists.

## ISO remastering

It is possible to create custom ISOs with an embedded cloud-config. This will let the machine automatically boot with a configuration file, which later will be installed in the system after provisioning is completed.

### Locally

To remaster an ISO locally you just need docker.

As kairos is based on elemental, the elemental-cli can be used to create a new ISO, with an additional config, consider the following steps:

```bash
$ IMAGE=<source/image>
$ mkdir -p files-iso/boot/grub2
# You can replace this step with your own grub config. This GRUB configuration is the boot menu of the ISO
$ wget https://raw.githubusercontent.com/kairos-io/kairos/master/overlay/files-iso/boot/grub2/grub.cfg -O files-iso/boot/grub2/grub.cfg
# Copy the config file
$ cp -rfv cloud_init.yaml files-iso/config.yaml
# Pull the image locally
$ docker pull $IMAGE
# Optionally, modify the image here!
# docker run --entrypoint /bin/bash --name changes -ti $IMAGE
# docker commit changes $IMAGE
# Build an ISO with $IMAGE
$ docker run -v $PWD:/cOS -v /var/run/docker.sock:/var/run/docker.sock -i --rm quay.io/kairos/osbuilder-tools:v0.1.1 --name "custom-iso" --debug build-iso --date=false --local --overlay-iso /cOS/files-iso $IMAGE --output /cOS/
```

### Kubernetes

It is possible to create ISOs and derivatives using extended Kubernetes API resources with an embedded config file to drive automated installations.

This method also allows to tweak the container image by overlaying others on top - without breaking the concept of immutability and single image OS.

Consider the following example, which requires a Kubernetes cluster to run the components, but works also on `kind`:

```bash

# Adds the kairos repo to helm
$ helm repo add kairos https://kairos-io.github.io/helm-charts
"kairos" has been added to your repositories
$ helm repo update
Hang tight while we grab the latest from your chart repositories...
...Successfully got an update from the "kairos" chart repository
Update Complete. ⎈Happy Helming!⎈

# Install the CRD chart
$ helm install kairos-crd kairos/kairos-crds
NAME: kairos-crd
LAST DEPLOYED: Tue Sep  6 20:35:34 2022
NAMESPACE: default
STATUS: deployed
REVISION: 1
TEST SUITE: None

# Installs osbuilder
$ helm install kairos-osbuilder kairos/osbuilder
NAME: kairos-osbuilder
LAST DEPLOYED: Tue Sep  6 20:35:53 2022
NAMESPACE: default
STATUS: deployed
REVISION: 1
TEST SUITE: None

# Applies an OSArtifact spec
cat <<'EOF' | kubectl apply -f -
apiVersion: build.kairos.io/v1alpha1
kind: OSArtifact
metadata:
  name: hello-kairos
spec:
  imageName: "quay.io/kairos/core-opensuse:latest"
  iso: true
  bundles:
  - quay.io/kairos/packages:goreleaser-utils-1.11.2
  grubConfig: |
          search --file --set=root /boot/kernel.xz
          set default=0
          set timeout=10
          set timeout_style=menu
          set linux=linux
          set initrd=initrd
          if [ "${grub_cpu}" = "x86_64" -o "${grub_cpu}" = "i386" -o "${grub_cpu}" = "arm64" ];then
              if [ "${grub_platform}" = "efi" ]; then
                  if [ "${grub_cpu}" != "arm64" ]; then
                      set linux=linuxefi
                      set initrd=initrdefi
                  fi
              fi
          fi
          if [ "${grub_platform}" = "efi" ]; then
              echo "Please press 't' to show the boot menu on this console"
          fi
          set font=($root)/boot/${grub_cpu}/loader/grub2/fonts/unicode.pf2
          if [ -f ${font} ];then
              loadfont ${font}
          fi
          menuentry "install" --class os --unrestricted {
              echo Loading kernel...
              $linux ($root)/boot/kernel.xz cdroot root=live:CDLABEL=COS_LIVE rd.live.dir=/ rd.live.squashimg=rootfs.squashfs console=tty1 console=ttyS0 rd.cos.disable vga=795 nomodeset nodepair.enable
              echo Loading initrd...
              $initrd ($root)/boot/rootfs.xz
          }

          if [ "${grub_platform}" = "efi" ]; then
              hiddenentry "Text mode" --hotkey "t" {
                  set textmode=true
                  terminal_output console
              }
          fi

  cloudConfig: |
            #node-config
            install:
              device: "/dev/sda"
              reboot: true
              poweroff: false
              auto: true # Required, for automated installations
EOF

# Note on running with kind:
$ IP=$(docker inspect kind-control-plane | jq -r '.[0].NetworkSettings.Networks.kind.IPAddress')
$ PORT=$(kubectl get svc hello-kairos -o json | jq '.spec.ports[0].nodePort')
$ curl http://$IP:$PORT/hello-kairos.iso -o test.iso


```
