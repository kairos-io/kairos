---
title: "Customizing the system image"
linkTitle: "Customization"
weight: 2
description: >
---

Kairos is an open source, container-based operating system. To modify Kairos and add a package, you'll need to build a container image from the [Kairos images](/docs/reference/image_matrix/). Here's an example with Docker which adds `figlet`:

```docker
# Use images from docs/reference/image_matrix/
FROM quay.io/kairos/kairos:opensuse-latest

RUN zypper in -y figlet

RUN export VERSION="my-version"
RUN envsubst '${VERSION}' </etc/os-release
```

After creating your Dockerfile, you can build your own image by running the following command:

```bash
$ docker build -t docker.io/<yourorg>/myos:0.1 .
Sending build context to Docker daemon  2.048kB
Step 1/3 : FROM quay.io/kairos/kairos-opensuse:latest
 ---> 897dc0cddf91
Step 2/3 : RUN zypper install -y figlet
 ---> Using cache
 ---> d57ff48546e7
Step 3/3 : RUN MY_VERSION="my-version" >> /etc/os-release
 ---> Running in b7bcb24969f5
Removing intermediate container b7bcb24969f5
 ---> ca21930a4585
Successfully built ca21930a4585
Successfully tagged <your-org>/myos:0.1
```

Once you have built your image, you can publish it to Docker Hub or another registry with the following command:

```bash
$ docker push <your-org>/myos:0.1
The push refers to repository [docker.io/<your-org>/myos]
c58930881bc4: Pushed
7111ee985500: Pushed
...
```

You can use your custom image when [upgrade nodes manually](/docs/upgrade/manual), [with Kubernetes](/docs/upgrade/kubernetes) or [specifying it in the cloud-config during installation](/docs/examples/core). Here's how to do it manually with the `kairos-agent` command:

```
node:/home/kairos # kairos-agent  upgrade --image docker.io/<your-org>/myos:0.1
INFO[2022-12-01T13:49:41Z] Starting elemental version v0.0.1
INFO[2022-12-01T13:49:42Z] Upgrade called
INFO[2022-12-01T13:49:42Z] Applying 'before-upgrade' hook
INFO[2022-12-01T13:49:42Z] Running before-upgrade hook
INFO[2022-12-01T13:49:42Z] deploying image docker.io/oz123/myos:0.1 to /run/initramfs/cos-state/cOS/transition.img
INFO[2022-12-01T13:49:42Z] Creating file system image /run/initramfs/cos-state/cOS/transition.img
INFO[2022-12-01T13:49:42Z] Copying docker.io/oz123/myos:0.1 source...
INFO[0000] Unpacking a container image: docker.io/oz123/myos:0.1
INFO[0000] Pulling an image from remote repository
...
INFO[2022-12-01T13:52:33Z] Finished moving /run/initramfs/cos-state/cOS/transition.img to /run/initramfs/cos-state/cOS/active.img 
INFO[2022-12-01T13:52:33Z] Upgrade completed
INFO[2022-12-01T13:52:33Z] Upgrade completed

node:/home/kairos # which figlet
which: no figlet in (/sbin:/usr/sbin:/usr/local/sbin:/root/bin:/usr/local/bin:/usr/bin:/bin)
node:/home/kairos # reboot

```

Now, reboot your OS and ssh again to it to use figlet:

```
$ ssh -l kairos node:
Welcome to Kairos!

Refer to https://kairos.io for documentation.
kairos@node2:~> figlet kairos rocks!
 _         _                                _        _
| | ____ _(_)_ __ ___  ___   _ __ ___   ___| | _____| |
| |/ / _` | | '__/ _ \/ __| | '__/ _ \ / __| |/ / __| |
|   < (_| | | | | (_) \__ \ | | | (_) | (__|   <\__ \_|
|_|\_\__,_|_|_|  \___/|___/ |_|  \___/ \___|_|\_\___(_)
```

## Customizing the Kernel

Kairos allows you to customize the kernel and initrd as part of your container-based operating system. If you are using a glibc-based distribution, such as OpenSUSE or Ubuntu, you can use the distribution's package manager to replace the kernel with the one you want, and then rebuild the initramfs with `dracut`.

Here's an example of how to do this:

```bash
# Replace the existing kernel with a new one, depending on the base image it can differ
apt-get install -y ...

# Create the kernel symlink
kernel=$(ls /boot/vmlinuz-* | head -n1)
ln -sf "${kernel#/boot/}" /boot/vmlinuz

# Regenerate the initrd, in openSUSE we could just use "mkinitrd"
kernel=$(ls /lib/modules | head -n1)
dracut -v -f "/boot/initrd-${kernel}" "${kernel}"
ln -sf "initrd-${kernel}" /boot/initrd

# Update the module dependencies
kernel=$(ls /lib/modules | head -n1)
depmod -a "${kernel}"
```

{{% alert title="Note" %}}

If you are using an Alpine-based distribution, modifying the kernel is only possible by rebuilding the kernel and initrd outside of the Dockerfile and then embedding it into the image. This is because dracut and systemd are not supported in musl-based distributions. We are currently exploring ways to provide initramfs that can be generated from musl systems as well.

{{% /alert %}}

After you have modified the kernel and initrd, you can use the kairos-agent upgrade command to update your nodes, or [within Kubernetes](/docs/upgrade/kubernetes).
