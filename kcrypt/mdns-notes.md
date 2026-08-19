# Prerequisites

Nodes and KMS should be on the same local network (mdns requirement)

# Steps

- Create a cluster with a port bound to the host:

```
k3d cluster create kcrypt -p '30000:30000@server:0' 
```

(we are going to assign this port to the kcrypt challenger server and advertise it over mdns)

- Follow [the instructions to setup the kcrypt challenger server](https://github.com/kairos-io/kcrypt-challenger#installation):

```
helm repo add kairos https://kairos-io.github.io/helm-charts
helm install kairos-crd kairos/kairos-crds
```

Create the following 'kcrypt-challenger-values.yaml` file:


```yaml
service:
  challenger:
    type: "NodePort"
    port: 8082
    nodePort: 30000
```

and deploy the challenger server with it:

```bash
helm install -f kcrypt-challenger-values.yaml kairos-challenger kairos/kairos-challenger
```

- Add the sealedvolume and secret for the tpm chip:

```
apiVersion: v1
kind: Secret
metadata:
  name: example-host-tpm-secret
  namespace: default
type: Opaque
stringData:
  pass: "awesome-passphrase"
---
apiVersion: keyserver.kairos.io/v1alpha1
kind: SealedVolume
metadata:
    name: example-host
    namespace: default
spec:
  TPMHash: "5640e37f4016da16b841a93880dcc44886904392fa3c86681087b77db5afedbe"
  partitions:
    - label: COS_PERSISTENT
      secret:
        name: example-host-tpm-secret
        path: pass
  quarantined: false
```

- Start the [simple-mdns-server](https://github.com/kairos-io/simple-mdns-server)

```
go run . --port 30000 --interfaceName enp121s0 --serviceType _kcrypt._tcp --hostName mychallenger.local
```


- Start a node in manual install mode

- Replace `/system/discovery/kcrypt-discovery-challenger` with a custom build (until we merge)

- Create the following config:

```
#cloud-config

users:
  - name: kairos
    passwd: kairos

install:
  grub_options:
    extra_cmdline: "rd.neednet=1"
  encrypted_partitions:
  - COS_PERSISTENT

# Kcrypt configuration block
kcrypt:
  challenger:
    mdns: true
    challenger_server: "http://mychallenger.local"
```

- Install:

```
kairos-agent manual-install --device auto config.yaml
```
