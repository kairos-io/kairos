+++
title = "Upgrades"
date = 2022-02-09T17:56:26+01:00
weight = 1
pre = "<b>- </b>"
+++

## Kubernetes

Upgrades can be triggered from Kubernetes with `system-upgrade-controller` installed in your cluster. [See the cOS documentation](https://rancher-sandbox.github.io/cos-toolkit-docs/docs/getting-started/upgrading/#integration-with-system-upgrade-controller)


System upgrade controller needs to be installed in the cluster which is targeted for the upgrades, for example:

```bash
kubectl apply -f https://raw.githubusercontent.com/rancher/system-upgrade-controller/master/manifests/system-upgrade-controller.yaml
```

Then in order to trigger an upgrade, we need to create a new upgrade plan for the cluster. create a `Plan` resource like the following as `upgrade.yaml`:

```yaml
---
apiVersion: upgrade.cattle.io/v1
kind: Plan
metadata:
  name: os-upgrade
  namespace: system-upgrade
  labels:
    k3s-upgrade: server
spec:
  concurrency: 1
  #version:  latest
  version: "opensuse-v1.21.4-31"
  nodeSelector:
    matchExpressions:
      - {key: kubernetes.io/hostname, operator: Exists}
  serviceAccountName: system-upgrade
  cordon: true
  drain:
    force: true
    disableEviction: true
  upgrade:
    image: quay.io/c3os/c3os
    command:
    - "/usr/sbin/suc-upgrade"
```

And apply it:
```bash
kubectl apply -f upgrade.yaml
```

You can use the `version` field in the resource to tweak the `c3os version` depending on the chosen flavor. Refer to [system-upgrade-controller](https://github.com/rancher/system-upgrade-controller) for documentation.

## Manual

Upgrades can be triggered manually as well from the nodes.

To upgrade to latest available version, run from a shell of a cluster node:

```bash
c3os upgrade
```

To specify a version, just run 

```bash
c3os upgrade <version>
```

Use `--force` to force upgrading to avoid checking versions. All the available versions can be list with: `c3os upgrade list-releases`.

It is possible altough to use the same commandset from `cOS`. So for example, the following works too:

```bash
elemental upgrade --no-verify --docker-image quay.io/c3os/c3os:opensuse-v1.21.4-22
```

c3os images are released on [quay.io](https://quay.io/repository/c3os/c3os).

[See also the general cOS documentation](https://rancher-sandbox.github.io/cos-toolkit-docs/docs/getting-started/upgrading/#upgrade-to-a-specific-container-image) which applies for `c3os` as well.