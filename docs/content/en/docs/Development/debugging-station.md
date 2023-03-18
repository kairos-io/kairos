---
title: "Debugging station"
linkTitle: "Debugging station"
weight: 4
date: 2023-03-15
description: >
  Debugging station
---

When developing or troubleshooting Kairos, it can be useful to share a local cluster with another peer. This section illustrates how to use [Entangle](/docs/reference/entangle) to achieve that. We call this setup _debugging-station_.

## Configuration


{{% alert title="Note" color="warning" %}}

This section describes the configuration step by step. If you are in a hurry, you can skip this section and directly go to **Deploy with AuroraBoot**.

{{% /alert %}}

When deploying a new cluster, we can use [Bundles](/docs/advanced/bundles) to install the `entangle` and `cert-manager` chart automatically. We specify the bundles in the cloud config file as shown below:

```yaml
bundles:
- targets:
  - run://quay.io/kairos/community-bundles:cert-manager_latest
  - run://quay.io/kairos/community-bundles:kairos_latest
```

We also need to enable entangle by setting `kairos.entangle.enable: true`. 

Next, we generate a new token that we will use to connect to the cluster later.

```bash
docker run -ti --rm quay.io/mudler/edgevpn -b -g
```

In order for `entangle` to use the token, we can define a `Entanglement` to expose ssh in the mesh network like the following:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: ssh-entanglement
  namespace: kube-system
type: Opaque
stringData:
  network_token: ___GENERATED TOKEN HERE___
---
apiVersion: entangle.kairos.io/v1alpha1
kind: Entanglement
metadata:
  name: ssh-entanglement
  namespace: kube-system
spec:
    serviceUUID: "ssh"
    secretRef: "ssh-entanglement"
    host: "127.0.0.1"
    port: "22"
    hostNetwork: true
```

{{% alert title="Note" color="warning" %}}

If you have already a kubernetes cluster, you can install the [Entangle](/docs/reference/entangle) chart and just apply the manifest.

{{% /alert %}}

This entanglement will expose the port `22` in the node over the mesh network with the `ssh` service UUID so we can later connect to it. Replace `___GENERATED TOKEN HERE___` with the token you previously generated with the `docker` command (check out the [documentation](/docs/reference/entangle) for advanced usage).

In order to deploy the `Entanglement` automatically, we can add it to the `k3s` manifests folder in the cloud config file:

```yaml
write_files:
- path: /var/lib/rancher/k3s/server/manifests/expose-ssh.yaml
  permissions: "0644"
  owner: "root"
  content: |
      apiVersion: v1
      kind: Secret
      metadata:
        name: ssh-entanglement
        namespace: kube-system
      type: Opaque
      stringData:
        network_token: ___GENERATED TOKEN HERE___
      ---
      apiVersion: entangle.kairos.io/v1alpha1
      kind: Entanglement
      metadata:
        name: ssh-entanglement
        namespace: kube-system
      spec:
         serviceUUID: "ssh"
         secretRef: "ssh-entanglement"
         host: "127.0.0.1"
         port: "22"
         hostNetwork: true
```

Here's an example of a complete cloud configuration file which automatically install a Kairos node in the bigger disk, and exposes ssh with `entangle`:

```yaml
#cloud-config

install:
 device: "auto"
 auto: true
 reboot: true

hostname: debugging-station-{{ trunc 4 .MachineID }}

users:
- name: kairos
  passwd: kairos
  ssh_authorized_keys:
  - github:mudler

k3s:
  enabled: true

# Specify the bundle to use
bundles:
- targets:
  - run://quay.io/kairos/community-bundles:system-upgrade-controller_latest
  - run://quay.io/kairos/community-bundles:cert-manager_latest
  - run://quay.io/kairos/community-bundles:kairos_latest

kairos:
  entangle:
    enable: true

write_files:
- path: /var/lib/rancher/k3s/server/manifests/expose-ssh.yaml
  permissions: "0644"
  owner: "root"
  content: |
      apiVersion: v1
      kind: Secret
      metadata:
        name: ssh-entanglement
        namespace: kube-system
      type: Opaque
      stringData:
        network_token: ___GENERATED TOKEN HERE___
      ---
      apiVersion: entangle.kairos.io/v1alpha1
      kind: Entanglement
      metadata:
        name: ssh-entanglement
        namespace: kube-system
      spec:
         serviceUUID: "ssh"
         secretRef: "ssh-entanglement"
         host: "127.0.0.1"
         port: "22"
         hostNetwork: true
```

In this file, you can specify various settings for your debugging station. For example, the `hostname` field sets the name of the machine, and the `users` field creates a new user with the name "kairos" and a pre-defined password and SSH key. The `k3s` field enables the installation of the k3s Kubernetes distribution.

## Deploy with AuroraBoot

To automatically boot and install the debugging station, we can use [Auroraboot](/docs/reference/auroraboot). The following example shows how to use the cloud config above with it:

```bash
cat <<EOF | docker run --rm -i --net host quay.io/kairos/auroraboot \
                    --cloud-config - \
                    --set "container_image=quay.io/kairos/kairos-opensuse-leap:v1.6.1-k3sv1.26.1-k3s1"
#cloud-config

install:
 device: "auto"
 auto: true
 reboot: true

hostname: debugging-station-{{ trunc 4 .MachineID }}

users:
- name: kairos
  passwd: kairos
  ssh_authorized_keys:
  - github:mudler

k3s:
  enabled: true

# Specify the bundle to use
bundles:
- targets:
  - run://quay.io/kairos/community-bundles:system-upgrade-controller_latest
  - run://quay.io/kairos/community-bundles:cert-manager_latest
  - run://quay.io/kairos/community-bundles:kairos_latest

kairos:
  entangle:
    enable: true

write_files:
- path: /var/lib/rancher/k3s/server/manifests/expose-ssh.yaml
  permissions: "0644"
  owner: "root"
  content: |
      apiVersion: v1
      kind: Secret
      metadata:
        name: ssh-entanglement
        namespace: kube-system
      type: Opaque
      stringData:
        network_token: ___GENERATED TOKEN HERE___
      ---
      apiVersion: entangle.kairos.io/v1alpha1
      kind: Entanglement
      metadata:
        name: ssh-entanglement
        namespace: kube-system
      spec:
         serviceUUID: "ssh"
         secretRef: "ssh-entanglement"
         host: "127.0.0.1"
         port: "22"
         hostNetwork: true
EOF
```

## Connecting to the cluster

To connect to the cluster, we first need to open the tunnel in one terminal and then ssh from another one.

In one terminal, run the following command (it will run in the foreground):

```bash
# Run in a terminal (it is foreground)
export EDGEVPNTOKEN="___GENERATED TOKEN HERE___"
docker run -e "EDGEVPNTOKEN=$EDGEVPNTOKEN" --net host quay.io/mudler/edgevpn service-connect ssh 127.0.0.1:2222
```

In another terminal, run the following command to ssh to the box:

```bash
# Run in another terminal
ssh kairos@127.0.0.1 -p 2222
```

Note: it might take few attempts to establish a connection