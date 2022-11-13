---
title: "Entangle CRDs"
linkTitle: "Entangle"
weight: 8
date: 2022-11-13
description: >
---

{{% alert title="Note" %}}

This section is a work in progress!

{{% /alert %}}

Kairos has two Kubernetes Native extensions that allows to interconnect services between different clusters via P2P with a shared secret.

The clusters won't need to do any specific setting in order to establish a connection, as it uses internally [libp2p](https://github.com/libp2p/go-libp2p) to connect between the nodes.

Entangle can be used to connect services running on different clusters or can be used with `entangle-proxy` to control another cluster remotely via P2P.

## Prerequisites

To `entangle` two or more clusters you need one or more Kubernetes cluster; there is no extra requirement.

## Install the CRD

First, install the Kubernetes CRD: 
```bash
# Adds the Kairos repo to Helm
$ helm repo add kairos https://Kairos-io.github.io/helm-charts
"kairos" has been added to your repositories
$ helm repo update
Hang tight while we grab the latest from your chart repositories...
...Successfully got an update from the "kairos" chart repository
Update Complete. ⎈Happy Helming!⎈

# Install the CRD chart
$ helm install kairos-crd kairos/kairos-crds
NAME: kairos-crd
LAST DEPLOYED: Tue Sep  6 20:35:34 2022
NAMESPACE: default
STATUS: deployed
REVISION: 1
TEST SUITE: None
```

Then, install entangle and entangle-proxy:

```bash
# Installs kairos-entangle
$ helm install kairos-entangle kairos/entangle
NAME: kairos-entangle
LAST DEPLOYED: Tue Sep  6 20:35:53 2022
NAMESPACE: default
STATUS: deployed
REVISION: 1
TEST SUITE: None

# Installs kairos-proxy
$ helm install kairos-proxy kairos/entangle-proxy
NAME: kairos-proxy
LAST DEPLOYED: Tue Sep  6 20:35:53 2022
NAMESPACE: default
STATUS: deployed
REVISION: 1
TEST SUITE: None
```


