+++
title = "Upgrades"
date = 2022-02-09T17:56:26+01:00
weight = 1
pre = "<b>- </b>"
+++

## Kubernetes

Upgrades can be triggered from Kubernetes with `system-upgrade-controller` installed in your cluster. [See the cOS documentation](https://rancher-sandbox.github.io/cos-toolkit-docs/docs/getting-started/upgrading/#integration-with-system-upgrade-controller)

## Manual

Upgrades can be triggered manually as well from the nodes, for example, run the following as root:

```
cos-upgrade --no-verify --no-cosign --docker-image quay.io/c3os/c3os:opensuse-v1.21.4-22
```

c3os images are released on [quay.io](https://quay.io/repository/c3os/c3os).

[See also the cOS documentation](https://rancher-sandbox.github.io/cos-toolkit-docs/docs/getting-started/upgrading/#upgrade-to-a-specific-container-image)