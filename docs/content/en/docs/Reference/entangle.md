---
title: "Entangle CRDs"
linkTitle: "Entangle"
weight: 8
date: 2022-11-13
description: >
 Inter-connecting Kubernetes clusters without the need of exposing any service to the public via E2E P2P encrypted networks.
 
---

{{% alert title="Note" %}}

This feature is crazy and experimental! Do not run in production servers. 
Feedback and bug reports are welcome, as we are improving the p2p aspects of Kairos.

{{% /alert %}}

Kairos has two Kubernetes Native extensions ( [entangle](https://github.com/kairos-io/entangle) and [entangle-proxy](https://github.com/kairos-io/entangle-proxy) ) that allows to interconnect services between different clusters via P2P with a shared secret.

The clusters won't need to do any specific setting in order to establish a connection, as it uses [libp2p](https://github.com/libp2p/go-libp2p) to establish a connection between the nodes.

Entangle can be used to connect services running on different clusters or can be used with `entangle-proxy` to control another cluster remotely via P2P.

## Prerequisites

To `entangle` two or more clusters you need one or more Kubernetes cluster; `entangle` depends on `cert-manager`:

```bash
kubectl apply -f https://github.com/jetstack/cert-manager/releases/latest/download/cert-manager.yaml
kubectl wait --for=condition=Available deployment --timeout=2m -n cert-manager --all
```

- `entangle` needs to run on all the clusters that you wish to interconnect. It provides capabilities to interconnect services between clusters
- `entangle-proxy` only on the cluster that you wish to use as control cluster

### Install the CRD and `entangle`

First, add the kairos helm repository:

```bash
helm repo add kairos https://kairos-io.github.io/helm-charts
helm repo update
```

Install the CRDs with:

```bash
helm install kairos-crd kairos/kairos-crds
```

Install `entangle`:

```bash
helm install kairos-entangle kairos/entangle
## To use a different image:
## helm install kairos-entangle kairos/entangle --set image.serviceTag=v0.18.0 --set image.tag=latest
```

### Install `entangle-proxy`

Now install `entangle-proxy` only on the cluster which is used to control, and which dispatches manifests to downstream clusters. 


```bash
helm install kairos-entangle-proxy kairos/entangle-proxy
```

## Controlling a remote cluster

![control](https://user-images.githubusercontent.com/2420543/205872002-894f24aa-ac1c-4f70-bb46-aaad89392a25.png)

To control a remote cluster, you need a cluster where to issue and apply manifest from (the control cluster, where `entangle-proxy` is installed) and a cluster running `entangle` which proxies `kubectl` with a `ServiceAccount`/`Role` associated with it.

They both need to agree on a secret, which is the `network_token` to be able to communicate, otherwise it won't work. There is no other configuration needed in order for the two cluster to talk to each other.

### Generating a network token

Generating a network token is described in [the p2p section](/docs/installation/p2p)

### Managed cluster

The cluster which is the target of our manifests, as specified needs to run a deployment which _entangles_ `kubectl`:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: mysecret
  namespace: default
type: Opaque
stringData:
  network_token: YOUR_NETWORK_TOKEN_GOES_HERE
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: entangle
  namespace: default
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: entangle
rules:
- apiGroups:
  - ""
  resources:
  - pods
  verbs:
  - create
  - delete
  - get
  - list
  - update
  - watch

- apiGroups:
  - ""
  resources:
  - events
  verbs:
  - create
---
apiVersion: v1
kind: List
items:
  - apiVersion: rbac.authorization.k8s.io/v1
    kind: ClusterRoleBinding
    metadata:
      name: entangle
    subjects:
    - kind: ServiceAccount
      name: entangle
      namespace: default
    roleRef:
      kind: ClusterRole
      name: entangle
      apiGroup: rbac.authorization.k8s.io
---
apiVersion: apps/v1
kind: Deployment
metadata:
  labels:
    app: agent-proxy
  name: agent-proxy
  namespace: default
spec:
  selector:
    matchLabels:
      app: agent-proxy
  replicas: 1
  template:
    metadata:
      labels:
        app: agent-proxy
        entanglement.kairos.io/name: "mysecret"
        entanglement.kairos.io/service: "foo"
        entanglement.kairos.io/target_port: "8001"
        entanglement.kairos.io/direction: "entangle"
    spec:
      serviceAccountName: entangle
      containers:
        - name: proxy
          image: "quay.io/kairos/kubectl"
          imagePullPolicy: Always
          command: ["/usr/bin/kubectl"]
          args:
            - "proxy"
```

Note: replace *YOUR_NETWORK_TOKEN_GOES_HERE* with the token generated with the `kairos-cli`.

### Control

To control, from the cluster that has `entangle-proxy` installed we can apply:
```yaml
apiVersion: v1
kind: Secret
metadata:
  name: mysecret
  namespace: default
type: Opaque
stringData:
  network_token: YOUR_NETWORK_TOKEN_GOES_HERE
---
apiVersion: entangle-proxy.kairos.io/v1alpha1
kind: Manifests
metadata:
  name: hello
  namespace: default
  labels:
   entanglement.kairos.io/name: "mysecret"
   entanglement.kairos.io/service: "foo"
   entanglement.kairos.io/target_port: "9090"
spec:
   serviceUUID: "foo"
   secretRef: "mysecret"
   manifests:
   - |
      apiVersion: v1
      kind: Pod
      metadata:
        name: test
        namespace: default
      spec:
            containers:
            - name: hello
              image: busybox:1.28
              command: ['sh', '-c', 'echo "Hello, ssaa!" && sleep 3600']
            restartPolicy: OnFailure
```

Note: replace *YOUR_NETWORK_TOKEN_GOES_HERE* with the token generated with the `kairos-cli` and used in the step above.

## Expose services

The `entangle` CRD can be used to interconnect services of clusters, or create tunnels to cluster services.

- Can inject a sidecar container to access a remote services exposed
- Can create a deployment which exposes a remote service from another cluster

### Deployment


`entangle` can be used to tunnel a connection or a service available from one cluster to another.

![entangle-A](https://user-images.githubusercontent.com/2420543/205871973-d913680d-355f-4322-8cbb-6a94f8505ccb.png)
In the image above, we can see how entangle can create a tunnel for a service running on Cluster A and mirror it to to Cluster B.


It can also expose services that are reachable from the host Network:
![entangle-B](https://user-images.githubusercontent.com/2420543/205871999-17abcde8-1b78-4a71-bc3e-ed77664c5551.png)


Consider the following example that tunnels a cluster `192.168.1.1:80` to another one using an `Entanglement`:

{{< tabpane text=true right=true  >}}
{{% tab header="Cluster A (where `192.168.1.1:80` is accessible)" %}}
```yaml
apiVersion: v1
kind: Secret
metadata:
  name: mysecret
  namespace: default
type: Opaque
stringData:
  network_token: _YOUR_SECRET_GOES_HERE_
---
apiVersion: entangle.kairos.io/v1alpha1
kind: Entanglement
metadata:
  name: test2
  namespace: default
spec:
   serviceUUID: "foo2"
   secretRef: "mysecret"
   host: "192.168.1.1"
   port: "80"
   hostNetwork: true
```
{{% /tab %}}
{{% tab header="Cluster B (which will have a `ClusterIP` available on the Kubernetes service network)" %}}
```yaml
---
apiVersion: v1
kind: Secret
metadata:
  name: mysecret
  namespace: default
type: Opaque
stringData:
  network_token: _YOUR_SECRET_GOES_HERE_
---
apiVersion: entangle.kairos.io/v1alpha1
kind: Entanglement
metadata:
  name: test3
  namespace: default
spec:
   serviceUUID: "foo2"
   secretRef: "mysecret"
   host: "127.0.0.1"
   port: "8080"
   inbound: true
   serviceSpec:
    ports:
    - port: 8080
      protocol: TCP
    type: ClusterIP
```
{{% /tab %}}
{{< /tabpane >}}

### Sidecar injection

The controller can inject a container which exposes a connection (in both directions):


```yaml
apiVersion: v1
kind: Secret
metadata:
  name: mysecret
  namespace: default
type: Opaque
stringData:
  network_token: _YOUR_SECRET_GOES_HERE_
---
apiVersion: v1
kind: Pod
metadata:
  name: hello
  namespace: default
  labels:
   # Here we use the labels to refer to the service on the network, and the secret which contains our network_token
   entanglement.kairos.io/name: "mysecret"
   entanglement.kairos.io/service: "foo"
   entanglement.kairos.io/target_port: "9090"
spec:
      containers:
      - name: hello
        image: busybox:1.28
        command: ['sh', '-c', 'echo "Hello, Kubernetes!" && sleep 3600']
      restartPolicy: OnFailure
```


Or we can combine them together:

{{< tabpane text=true right=true  >}}
{{% tab header="Cluster A" %}}
```yaml
apiVersion: v1
kind: Secret
metadata:
  name: mysecret
  namespace: default
type: Opaque
stringData:
  network_token: _YOUR_SECRET_GOES_HERE_
---
apiVersion: apps/v1
kind: Deployment
metadata:
  labels:
    app: entangle-proxy
  name: entangle-proxy
  namespace: default
spec:
  selector:
    matchLabels:
      app: entangle-proxy
  replicas: 1
  template:
    metadata:
      labels:
        app: entangle-proxy
        entanglement.kairos.io/name: "mysecret"
        entanglement.kairos.io/service: "foo"
        entanglement.kairos.io/target_port: "8001"
        entanglement.kairos.io/direction: "entangle"
      name: entangle-proxy
    spec:
      containers:
        - name: proxy
          image: "quay.io/mudler/k8s-resource-scheduler:latest"
          imagePullPolicy: Always
          command: ["/usr/bin/kubectl"]
          args:
            - "proxy"
```
{{% /tab %}}
{{% tab header="Cluster B" %}}
```yaml
apiVersion: v1
kind: Secret
metadata:
  name: mysecret
  namespace: default
type: Opaque
stringData:
  network_token: _YOUR_SECRET_GOES_HERE_
---
apiVersion: entangle.kairos.io/v1alpha1
kind: Entanglement
metadata:
  name: test
  namespace: default
spec:
   serviceUUID: "foo"
   secretRef: "mysecret"
   host: "127.0.0.1"
   port: "8080"
   inbound: true
   serviceSpec:
    ports:
    - port: 8080
      protocol: TCP
    type: ClusterIP
```
{{% /tab %}}
{{< /tabpane >}}
