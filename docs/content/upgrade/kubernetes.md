
+++
title = "Upgrading from Kubernetes"
date = 2022-02-09T17:56:26+01:00
weight = 1
pre = "<b>- </b>"
+++

Kairos upgrades can be driven either manually or via Kubernetes. In order to trigger upgrades it is required to apply a CRD to the target cluster for the upgrade.

### Upgrading from version X to version Y with Kubernetes


To upgrade a node it is necessary [system-upgrade-controller](https://github.com/rancher/system-upgrade-controller) to be deployed in the target cluster.

To install it, use kubectl:
```bash
kubectl apply -f https://raw.githubusercontent.com/rancher/system-upgrade-controller/master/manifests/system-upgrade-controller.yaml
```

To trigger an upgrade, create a plan for the `system-upgrade-controller` which refers to the image version that we want to upgrade.

```bash
cat <<'EOF' | kubectl apply -f -
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
  # This is the version (tag) of the image. 
  # The version is refered to the kairos version plus the k3s version.
  version: "v0.57.0-k3sv1.23.9-k3s1"
  nodeSelector:
    matchExpressions:
      - {key: kubernetes.io/hostname, operator: Exists}
  serviceAccountName: system-upgrade
  cordon: false
  drain:
    force: false
    disableEviction: true
  upgrade:
    # Here goes the image which is tied to the flavor being used.
    # Currently can pick between opensuse and alpine
    image: quay.io/kairos/kairos-opensuse
    command:
    - "/usr/sbin/suc-upgrade"
EOF
```

To check all the available versions, see the [images](https://quay.io/repository/kairos/kairos-opensuse?tab=tags) available on the container registry, corresponding to the flavor/version selected.

{{% notice note %}}

Several upgrade strategies can be used with `system-upgrade-controller` which are not illustrated here in this example. For instance it can be specified the number of hosts which are running the upgrades, filtering by labels, and more. [Refer to the project documentation](https://github.com/rancher/system-upgrade-controller) on how to create efficient strategies to roll upgrades on the nodes. In the example above the upgrades are applied to every host of the cluster, one-by-one in sequence.

{{% /notice %}}

A pod should appear right after which carries on the upgrade and automatically reboots the node:

```
$ kubectl get pods -A
...
system-upgrade   apply-os-upgrade-on-kairos-with-1a1a24bcf897bd275730bdd8548-h7ffd   0/1     Creating   0          40s
```

Done! we should have all the basic to get our first cluster rolling, but there is much more we can do. 

Don't miss out how to create multi-machine clusters, or clusters using the p2p fully-meshed network.
