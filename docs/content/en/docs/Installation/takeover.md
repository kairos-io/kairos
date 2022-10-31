---
title: "Takeover"
linkTitle: "Takeover"
weight: 6
date: 2022-11-13
description: >
---

# Takeover installations

Kairos supports takeover installations. Here are a few summarized steps:

- From the dedicated control panel (OVH, Hetzner, etc.), boot in *rescue* mode
- [Install docker](https://docs.docker.com/engine/install/debian/) and run for example:

```
export DEVICE=/dev/sda
export IMAGE=quay.io/kairos/core-opensuse:v1.1.4
cat <<'EOF' > config.yaml
#cloud-config
users:
- name: "kairos"
  passwd: "kairos"
  ssh_authorized_keys:
  - github:mudler
EOF
export CONFIG_FILE=config.yaml
docker run --privileged -v $PWD:/data -v /dev:/dev -ti $IMAGE elemental install --cloud-init /data/$CONFIG_FILE --system.uri $IMAGE $DEVICE
```

- Switch back to *booting* from HD and reboot.
