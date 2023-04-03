---
title: "Confidential computing setup"
linkTitle: "Confidential Computing"
weight: 9
description: >
---

Confidential computing is a type of secure computing that allows users to encrypt and decrypt data on a secure, isolated computing environment.
It works by encrypting the data before it is sent to the cloud or other computing resources. This allows users to keep their data private and secure, even if it is accessed by unauthorized parties.
This makes it useful for sensitive data such as financial information, health records, and other confidential data.

One important aspect of Confidential Computing is the ability to encrypt data even in-memory. This document describes how to setup Kairos to use [`enclave-cc`](https://github.com/confidential-containers/enclave-cc)
in order to run confidential workloads.

## Create a Kairos cluster

The [`coco community bundle`](https://github.com/kairos-io/community-bundles/tree/main/coco) is supported since Kairos version `v2.0.0-alpha3` ("coco" stands for "**Co**nfidential **Co**mputing).

A configuration file like the following should be used (see the `bundles` section):

```
#cloud-config
bundles:
    - targets:
        - run://quay.io/kairos/community-bundles:system-upgrade-controller_latest
        - run://quay.io/kairos/community-bundles:cert-manager_latest
        - run://quay.io/kairos/community-bundles:kairos_latest
        - run://quay.io/kairos/community-bundles:coco_latest

install:
    auto: true
    device: auto
    reboot: true

k3s:
    enabled: true

users:
    - name: kairos
      passwd: kairos
```

The bundle is making some changes on the host's filesystem (installs a customized containerd binary among other things) and a restart of the node is needed in order for the changes to be applied fully.
When this file appears, reboot the node: `/etc/containerd/.sentinel`.

## Additional steps

- [Label our node](https://github.com/confidential-containers/documentation/blob/main/quickstart.md#prerequisites):

```
  kubectl label --overwrite node $(kubectl get nodes -o jsonpath='{.items[].metadata.name}') node-role.kubernetes.io/worker=""
```

- [Deploy the operator](https://github.com/confidential-containers/documentation/blob/main/quickstart.md#deploy-the-the-operator)

```
  kubectl apply -k github.com/confidential-containers/operator/config/release?ref=v0.4.0
```

- [Deploy the `ccruntime` resource]

```
  kubectl apply -k github.com/confidential-containers/operator/config/samples/ccruntime/ssh-demo?ref=v0.4.0
```

  (wait until they are all running: `kubectl get pods -n confidential-containers-system --watch`)

- [Deploy a workload](https://github.com/confidential-containers/documentation/blob/main/quickstart.md#test-creating-a-workload-from-the-sample-encrypted-image)

  The last part with the verification will only work from within a Pod because the IP address is internal:

  `ssh -i ccv0-ssh root@$(kubectl get service ccv0-ssh -o jsonpath="{.spec.clusterIP}")`

  You can create a Pod like this:

  ```
  apiVersion: v1
  kind: Pod
  metadata:
    name: kubectl
  spec:
    containers:
    - name: kubectl
      image: opensuse/leap
      command: ["/bin/sh", "-ec", "trap : TERM INT; sleep infinity & wait"]
  ```

  Get a shell to it and run the verification commands (You will need to install `ssh` in the Pod first).
