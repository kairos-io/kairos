---
layout: "../../layouts/docs/Layout.astro"
title: "Take over installation"
index: 7
---

Kairos supports takeover installations. See [the Elemental-toolkit docs](https://rancher.github.io/elemental-toolkit/docs/getting-started/install/#installation-from-3rd-party-livecd-or-rescue-mediums). Here are a few summarized steps:

- From the dedicated control panel (OVH, Hetzner, etc.), boot in *rescue* mode
- [Install docker](https://docs.docker.com/engine/install/debian/) and run for example:

```
export DEVICE=/dev/sda
export IMAGE=quay.io/mudler/c3os:v1.21.4-19
# A url pointing to a valid cloud-init config file. E.g. as a gist at gists.github.com
export CONFIG_FILE=...
docker run --privileged -v $DEVICE:$DEVICE -ti $IMAGE cos-installer --config $CONFIG_FILE --no-cosign --no-verify --docker-image $IMAGE $DEVICE
```

- Switch back to *booting* from HD and reboot.
