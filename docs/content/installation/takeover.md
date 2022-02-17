+++
title = "Take over installation"
date = 2022-02-09T17:56:26+01:00
weight = 4
chapter = false
pre = "<b>- </b>"
+++

`c3os` supports takeover installations, see also [the cOS docs](https://rancher-sandbox.github.io/cos-toolkit-docs/docs/getting-started/install/#installation-from-3rd-party-livecd-or-rescue-mediums) here are few summarized steps:

- From the Dedicated control panel (OVH, Hetzner, etc.), boot in rescue mode
- [install docker](https://docs.docker.com/engine/install/debian/) and run:
  
```
export DEVICE=/dev/sda
```

```
export IMAGE=quay.io/c3os/c3os:v1.21.4-19
```

```
# A url pointing to a valid cloud-init config file. E.g. as a gist at gists.github.com
export CONFIG_FILE=...
```

```
docker run --privileged -v $DEVICE:$DEVICE -ti $IMAGE cos-installer --config $CONFIG_FILE --no-cosign --no-verify --docker-image $IMAGE $DEVICE
```

- Switch back to booting from HD and reboot
