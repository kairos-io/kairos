---
title: "Customizing the system image"
linkTitle: "Customization"
weight: 2
description: >
---

Kairos is a container-based OS, if you want to change Kairos and add a package, it is required to build only a Docker image.

For example:

```docker
FROM quay.io/kairos/kairos:opensuse-latest

RUN zypper in -y figlet

RUN export VERSION="my-version"
RUN envsubst '${VERSION}' </etc/os-release
```

After that you build your own image  with:

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
Publish the image, for example to docker hub:
```bash
$ docker push <your-org>/myos:0.1
The push refers to repository [docker.io/<your-org>/myos]
c58930881bc4: Pushed
7111ee985500: Pushed
...
```

The image can be then used with `kairos-agent upgrade` or with system-upgrade-controller for upgrades within Kubernetes.
Here is how to do it with the `kairos-agent` command:

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
