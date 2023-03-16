---
title: "Upgrading from Kubernetes"
linkTitle: "Upgrading from Kubernetes"
weight: 1
date: 2022-11-13
description: >
---

Kairos upgrades can be performed either manually or via Kubernetes if the cluster is composed of Kairos nodes. In order to trigger upgrades, it is required to apply a `Plan` spec to the target cluster for the upgrade.

## Prerequisites

- It is necessary [system-upgrade-controller](https://github.com/rancher/system-upgrade-controller) to be deployed in the target cluster.

To install it, use kubectl:

```bash
kubectl apply -f https://github.com/rancher/system-upgrade-controller/releases/download/v0.9.1/system-upgrade-controller.yaml
```

### Upgrading from version X to version Y with Kubernetes

To trigger an upgrade, create a plan for `system-upgrade-controller` which refers to the image version that we want to upgrade.

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
  version: "v1.0.0-k3sv1.24.3-k3s1"
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
    image: quay.io/kairos/kairos-opensuse-leap
    command:
    - "/usr/sbin/suc-upgrade"
EOF
```

To check all the available versions, see the [images](https://quay.io/repository/kairos/kairos-opensuse-leap?tab=tags) available on the container registry, corresponding to the flavor/version selected.

{{% alert title="Note" %}}

Several upgrade strategies can be used with `system-upgrade-controller` which are not illustrated here in this example. For instance, it can be specified in the number of hosts which are running the upgrades, filtering by labels, and more. [Refer to the project documentation](https://github.com/rancher/system-upgrade-controller) on how to create efficient strategies to roll upgrades on the nodes. In the example above, the upgrades are applied to every host of the cluster, one-by-one in sequence.

{{% /alert %}}

A pod should appear right after which carries on the upgrade and automatically reboots the node:

```
$ kubectl get pods -A
...
system-upgrade   apply-os-upgrade-on-kairos-with-1a1a24bcf897bd275730bdd8548-h7ffd   0/1     Creating   0          40s
```

Done! We should have all the basics to get our first cluster rolling, but there is much more we can do.

## Verify images attestation during upgrades

Container images can be signed during the build phase of a CI/CD pipeline using [Cosign](https://github.com/sigstore/cosign), Kairos signs every artifact as part of the release process.

To ensure that the images used during upgrades match the expected signatures, [Kyverno](https://kyverno.io/) can be used to set up policies. This is done by checking if the signature is present in the OCI registry and if the image was signed using the specified key. The policy rule check fails if either of these conditions is not met.

To learn more about this specific Kyverno feature, you can refer to the [documentation](https://kyverno.io/docs/writing-policies/verify-images/). This allows for the verification of image authenticity directly at the node level prior to upgrading.

A Kyverno policy for `provider-kairos` images might look like the following:

```yaml
apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: check-image
spec:
  validationFailureAction: Enforce
  background: false
  webhookTimeoutSeconds: 30
  failurePolicy: Fail
  rules:
    - name: check-image
      match:
        any:
        - resources:
            kinds:
              - Pod
      verifyImages:
      - imageReferences:
        - "quay.io/kairos/kairos-*"
        attestors:
        - entries:
          # See: https://kyverno.io/docs/writing-policies/verify-images/#keyless-signing-and-verification
          - keyless:
              subject: "https://github.com/kairos-io/provider-kairos/.github/workflows/release.yaml@refs/tags/*"
              issuer: "https://token.actions.githubusercontent.com"
              rekor:
                url: https://rekor.sigstore.dev
```

To install Kyverno in a Kairos cluster, you can simply use the community [bundles](/docs/advanced/bundles). For example, you can use the following installation cloud config file:

```yaml
#cloud-config

hostname: kyverno-{{ trunc 4 .MachineID }}

# Specify the bundle to use
bundles:
- targets:
  - run://quay.io/kairos/community-bundles:system-upgrade-controller_latest
  - run://quay.io/kairos/community-bundles:cert-manager_latest
  - run://quay.io/kairos/community-bundles:kyverno_latest

users:
- name: kairos
  passwd: kairos

k3s:
 enabled: true
```

This configuration file prepare the system with the `cert-manager`, `system-upgrade-controller` and the `kyverno` bundle, enabling `k3s`.

## Customize the upgrade plan

It is possible to run additional commands before the upgrade takes place into the node, consider the following example:

```yaml
---
apiVersion: v1
kind: Secret
metadata:
  name: custom-script
  namespace: system-upgrade
type: Opaque
stringData:
  upgrade.sh: |
    #!/bin/sh
    set -e

    # custom command, for example, that injects or modifies a configuration option
    sed -i 's/something/to/g' /host/oem/99_custom.yaml
    # run the upgrade script
    /usr/sbin/suc-upgrade
---
apiVersion: upgrade.cattle.io/v1
kind: Plan
metadata:
  name: custom-os-upgrade
  namespace: system-upgrade
spec:
  concurrency: 1
  # This is the version (tag) of the image.
  # The version is refered to the kairos version plus the k3s version.
  version: "v1.0.0-rc2-k3sv1.23.9-k3s1"
  nodeSelector:
    matchExpressions:
      - { key: kubernetes.io/hostname, operator: Exists }
  serviceAccountName: system-upgrade
  cordon: false
  drain:
    force: false
    disableEviction: true
  upgrade:
    # Here goes the image which is tied to the flavor being used.
    # Currently can pick between opensuse and alpine
    image: quay.io/kairos/kairos-opensuse-leap
    command:
      - "/bin/bash"
      - "-c"
    args:
      - bash /host/run/system-upgrade/secrets/custom-script/upgrade.sh
  secrets:
    - name: custom-script
      path: /host/run/system-upgrade/secrets/custom-script
```

## Upgrade from c3os to Kairos

If you already have a `c3os` deployment, upgrading to Kairos requires changing every instance of `c3os` to `kairos` in the configuration file. This can be either done manually or with Kubernetes before rolling the upgrade.  Consider customizing the upgrade plan, for instance:

```yaml
---
apiVersion: v1
kind: Secret
metadata:
  name: custom-script
  namespace: system-upgrade
type: Opaque
stringData:
  upgrade.sh: |
    #!/bin/sh
    set -e
    sed -i 's/c3os/kairos/g' /host/oem/99_custom.yaml
    /usr/sbin/suc-upgrade
---
apiVersion: upgrade.cattle.io/v1
kind: Plan
metadata:
  name: custom-os-upgrade
  namespace: system-upgrade
spec:
  concurrency: 1
  # This is the version (tag) of the image.
  # The version is refered to the kairos version plus the k3s version.
  version: "v1.0.0-rc2-k3sv1.23.9-k3s1"
  nodeSelector:
    matchExpressions:
      - { key: kubernetes.io/hostname, operator: Exists }
  serviceAccountName: system-upgrade
  cordon: false
  drain:
    force: false
    disableEviction: true
  upgrade:
    # Here goes the image which is tied to the flavor being used.
    # Currently can pick between opensuse and alpine
    image: quay.io/kairos/kairos-opensuse-leap
    command:
      - "/bin/bash"
      - "-c"
    args:
      - bash /host/run/system-upgrade/secrets/custom-script/upgrade.sh
  secrets:
    - name: custom-script
      path: /host/run/system-upgrade/secrets/custom-script
```

## What's next?

- [Upgrade nodes manually](/docs/upgrade/manual)
- [Immutable architecture](/docs/architecture/immutable)
- [Create decentralized clusters](/docs/installation/p2p)
