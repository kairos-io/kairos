---
title: "Encrypting User Data with Kairos"
linkTitle: "Encrypting User Data with Kairos"
weight: 5
description: >
    This section describes how to encrypt partition with LUKS in Kairos.
---

Kairos offers the ability to encrypt user data partitions with `LUKS`. User-data partitions are dedicated to persist data for a running system, stored separately from the OS images. This encryption mechanism can also be used to encrypt additional partitions created during the installation process.  

Kairos supports the following encryption scenarios:  

1. **Offline mode** - Encryption key for partitions is stored on the machine inside the TPM chip.  
1. **Online mode (Automated)** - Keypair used to encrypt the partition passphrase is stored on the TPM chip.
1. **Online mode (Manually configured)** - Plaintext passphrase is stored in the KMS server and returned to the node after TPM challenging.

![encryption1_1674470732563_0](https://user-images.githubusercontent.com/2420543/214405291-97a30f2d-d70a-45ba-b842-5282c722c79e.png)

Kairos uses the TPM chip to encrypt partition passphrases, and for offline encryption, it stores the passphrase in the non-volatile registries of the chip.  
To enable encryption, you will need to specify the labels of the partitions you want to encrypt, a minimum configuration for offline encryption can be seen below:  

```yaml
#cloud-config

install:
  # Label of partitions to encrypt
  # COS_PERSISTENT is the OS partition 
  # dedicated to user-persistent data.
  encrypted_partitions:
  - COS_PERSISTENT
```

Please note that for online mode, you will also need to specify the key management server address that will be used to store the keys, a complete configuration reference is the following:

```yaml
#cloud-config

# Install block
install:
  # Label of partitions to encrypt
  # COS_PERSISTENT is the OS partition 
  # dedicated to user-persistent data.
  encrypted_partitions:
  - COS_PERSISTENT

# Kcrypt configuration block
kcrypt:
  challenger:
    # External KMS Server address. This must be reachable by the node
    challenger_server: "http://192.168.68.109:30000"
    # (optional) Custom Non-Volatile index to use to store encoded blobs
    nv_index: ""
    # (optional) Custom Index for the RSA Key pair
    c_index: ""
    # (optional) Custom TPM device
    tpm_device: ""
```

| Option | Description |
| --- | --- |
| `install.encrypted_partitions` | Label of partitions to encrypt |
| `kcrypt.challenger.challenger_server` | External KMS Server address |
| `kcrypt.challenger.nv_index` | Custom Non-Volatile index to use to store encoded blobs |
| `kcrypt.challenger.c_index` | Custom Index for the RSA Key pair |
| `kcrypt.challenger.tpm_device` | Custom TPM device |

## Requirements

The host machine must have a TPM chip version 2.0 or higher to use encryption with Kairos. A list of TPM chips/HW can be gound out here : https://trustedcomputinggroup.org/membership/certification/tpm-certified-products/

## Components  

The Kairos encryption design involves three components to manage partitions encryption and decryption lifecycle:  

- [kcrypt](https://github.com/kairos-io/kcrypt) runs on the machine and attempts to unlock partitions by using plugins to delegate encryption/decryption business logic.    
- [kcrypt-discovery-challenger](https://github.com/kairos-io/kcrypt-challenger) runs on the machine, it is called by `kcrypt` and uses the TPM chip to retrieve the passphrase as described below.  
- [kcrypt-challenger](https://github.com/kairos-io/kcrypt-challenger) is the KMS (Key Management Server) component, deployed in Kubernetes, which manages secrets and partitions of the nodes.

## Offline mode  

This scenario covers encryption of data at rest without any third party or KMS server. The keys used to encrypt the partitions are stored in the TPM chip.

### Deployment  

A high level overview of the components can be observed here:

![encryption1_1674472109993_0](https://user-images.githubusercontent.com/2420543/214405302-96bfef4b-fb5e-4442-8d50-c8d7ebe53dab.png)

A complete cloud config example for this scenario can be found in the code block below:

```yaml
#cloud-config

install:
  encrypted_partitions:
  - COS_PERSISTENT

hostname: metal-{{ trunc 4 .MachineID }}
users:
- name: kairos
  # Change to your pass here
  passwd: kairos
  ssh_authorized_keys:
  # Replace with your github user and un-comment the line below:
  # - github:mudler
```

## Online mode 

Online mode involves an external service (the Key Management Server, KMS) to boot the machines. The KMS role is to enable machine to boot by providing the encrypted secrets, or passphrases to unlock the encrypted drive. Authentication with the KMS is done via TPM challenging.

In this scenario, we need to first deploy the KMS server to an existing Kubernetes cluster, and associate the TPM hash of the nodes that we want to manage. During deployment, we specify the KMS server inside the cloud-config of the nodes to be provisioned.

### Requirements

- A Kubernetes cluster  
- Kcrypt-challenger reachable by the nodes attempting to boot  

### Install the KMS (`kcrypt-challenger`)

To install the KMS (`kcrypt-challenger`), you will first need to make sure that certificate manager is installed. You can do this by running the following command:  
 
```
kubectl apply -f https://github.com/jetstack/cert-manager/releases/latest/download/cert-manager.yaml
kubectl wait --for=condition=Available deployment --timeout=2m -n cert-manager --all
```

To install `kcrypt-challenger` on a Kubernetes cluster with `helm`, you can use the commands below:

```
# Install the helm repository
helm repo add kairos https://kairos-io.github.io/helm-charts
helm repo update

# Install the Kairos CRDs
helm install kairos-crd kairos/kairos-crds

# Deploy the KMS challenger
helm install kairos-challenger kairos/kairos-challenger --set service.challenger.type="NodePort"
  
# we can also set up a specific port and a version:
# helm install kairos-challenger kairos/kairos-challenger --set image.tag="v0.2.2" --set service.challenger.type="NodePort" --set service.challenger.nodePort=30000
```

A service must be used to expose the challenger. If using the node port, we can retrieve the address with:

```bash
export EXTERNAL_IP=$(kubectl get nodes -o jsonpath='{.items[].status.addresses[?(@.type == "ExternalIP")].address}')
export PORT=$(kubectl get svc kairos-challenger-escrow-service -o json | jq '.spec.ports[0].nodePort')
```

### Register a node

You can get a node TPM hash by running `/system/discovery/kcrypt-discovery-challenger` as root from the LiveCD and add via kubectl the node with its tpmhash (the hash returned by running the command):  
  
```
kairos@localhost:~> ID=$(sudo /system/discovery/kcrypt-discovery-challenger)
kairos@localhost:~> echo $ID
7441c78f1976fb23e6a5c68f0be35be8375b135dcb36fb03cecc60f39c7660bd
```

### Scenario: Automatically generated keys

![encryption3_1674472162848_0](https://user-images.githubusercontent.com/2420543/214405310-78f7deec-b43e-4581-a99b-a358492cc7ac.png)

The TPM chip generates unique RSA keys for each machine during installation, which are used to encrypt a generated secret. These keys can only be accessed by the TPM and not by the KMS, thus ensuring that both the KMS and the TPM chip are required to boot the machine. As a result, even if the machine or its disks are stolen, the drive remains sealed and encrypted.
Deployment using this method, will store the encrypted key used to boot into the KMS, and the keypair used to encrypt it in the TPM chip of the machine during installation. This means that, only the TPM chip can decode the passphrase, and the passphrase is stored in the KMS such as it can't be decrypted by it. As such, nodes can boot only with the KMS, and the disk can be decrypted only by the node.

To register a node to kubernetes, use the TPM hash retrieved before:  
  
```yaml
apiVersion: keyserver.kairos.io/v1alpha1
kind: SealedVolume
metadata:
    name: test2
    namespace: default
spec:
  TPMHash: "7441c78f1976fb23e6a5c68f0be35be8375b135dcb36fb03cecc60f39c7660bd"
  partitions:
    - label: COS_PERSISTENT
  quarantined: false
```

A node can use the following during deployment, specifying the address of the challenger server:
 
``` yaml
#cloud-config

install:
  encrypted_partitions:
  - COS_PERSISTENT
  grub_options:
    extra_cmdline: "rd.neednet=1"

kcrypt:
  challenger:
    challenger_server: "http://192.168.68.109:30000"
    nv_index: ""
    c_index: ""
    tpm_device: ""

hostname: metal-{{ trunc 4 .MachineID }}
users:
- name: kairos
  # Change to your pass here
  passwd: kairos
  ssh_authorized_keys:
  # Replace with your github user and un-comment the line below:
  - github:mudler
```

### Scenario: Static keys  

![encryption4_1674472306435_0](https://user-images.githubusercontent.com/2420543/214405316-63882311-ca27-4b6e-9465-70d702ab6dc1.png)

In this scenario the Kubernetes administrator knows the passphrase of the nodes, and sets explicitly during configuration the passphrase for each partitions of the nodes. This scenario is suitable for cases when the passphrase needs to be carried over, and not to be tied specifically to the TPM chip.  
The TPM chip is still used for authentication a machine. The discovery-challenger needs still to know the TPM hash of each of the nodes before installation.  
To register a node to kubernetes, use the TPM hash retrieved before, and specify a passphrase with a secret reference for the partition:  
 
```yaml
apiVersion: v1
kind: Secret
metadata:
  name: mysecret
  namespace: default
type: Opaque
stringData:
  pass: "awesome-plaintext-passphrase"
---  
apiVersion: keyserver.kairos.io/v1alpha1
kind: SealedVolume
metadata:
    name: test2
    namespace: default
spec:
  TPMHash: "7441c78f1976fb23e6a5c68f0be35be8375b135dcb36fb03cecc60f39c7660bd"
  partitions:
    - label: COS_PERSISTENT
      secret:
       name: mysecret
       path: pass
  quarantined: false
```

The node doesn't need any specific configuration beside the kcrypt challenger, so for instance:
  
```yaml
#cloud-config

install:
  encrypted_partitions:
  - COS_PERSISTENT
  grub_options:
    extra_cmdline: "rd.neednet=1"

kcrypt:
  challenger:
    challenger_server: "http://192.168.68.109:30000"
    nv_index: ""
    c_index: ""
    tpm_device: ""

hostname: metal-{{ trunc 4 .MachineID }}
users:
- name: kairos
  # Change to your pass here
  passwd: kairos
  ssh_authorized_keys:
  # Replace with your github user and un-comment the line below:
  - github:mudler
```

## Troubleshooting  
- Invoking `/system/discovery/kcrypt-discovery-challenger` without arguments returns the TPM pubhash.
- Invoking `kcrypt-discovery-challenger` with 'discovery.password' triggers the logic to retrieve the passphrase, for instance can be used as such:
```
    echo '{ "data": "{ \"label\": \"LABEL\" }"}' | sudo -E WSS_SERVER="http://localhost:30000" /system/discovery/kcrypt-discovery-challenger "discovery.password"
```