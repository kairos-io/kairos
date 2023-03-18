---
title: "P2P multi-node cluster with AuroraBoot"
linkTitle: "P2P multi-node cluster with AuroraBoot"
weight: 6
date: 2023-02-15
description: >
  Full end to end example to bootstrap a self-coordinated cluster with Kairos and AuroraBoot
---

{{% alert title="Note" %}}

The p2p feature of Kairos is crazy and experimental! Do not run in production servers. 
Feedback and bug reports are welcome, as we are improving the p2p aspects of Kairos.

{{% /alert %}}

Deploying Kubernetes at the Edge can be a complex and time-consuming process, especially when it comes to setting up and managing multiple clusters. To make this process easier, Kairos leverages peer-to-peer technology to automatically coordinate and create Kubernetes clusters without the need of a control management interface.

To leverage p2p self-coordination capabilities of Kairos, you will need to configure the `network_token` under the `p2p` configuration block in your cloud-config file. Once you have set this, Kairos will handle the configuration of each node.

{{% alert title="Note" %}}
You can see this example live in the [Kairos and libp2p video]({{< ref "docs/media/#how-kairos-uses-libp2p" >}} "Media") in the [Media Section]({{< ref "docs/media" >}} "Media")
{{% /alert %}}

## Description

In the following example we are going to bootstrap a new multi-node, single cluster with Kairos. We will use at least 2 nodes, one as a master and one as a worker. Note how we don't specify any role, or either pin any IP in the following configurations.

We will first create a cloud config file for our deployment, and then run [AuroraBoot](/docs/reference/auroraboot) locally. We then start 2 VMs configured for netbooting. 

## Prepare your `cloud-config` file

Consider the following example, which uses cloud-config to automatically configure the cluster:

We start by creating a cloud config file locally, that could look similar to this:
``` yaml
#cloud-config

hostname: kairoslab-{{ trunc 4 .MachineID }}
users:
- name: kairos
  passwd: kairos
  ssh_authorized_keys:
  # Replace with your github user and un-comment the line below:
  - github:mudler
  - github:mauromorales

p2p:
 disable_dht: true # Enable for LAN-only clusters
 network_token: ""
```

As we want the installation to be triggered automatically, we add also the `install block`:
``` yaml
install:
 auto: true
 device: "auto"
 reboot: true
```

In order to leverage p2p and automatic node co-ordination, we need to generate a unique pre-shared token that will be used by all the nodes that we want to be part of our cluster. 

We can generate a network token by using the `edgevpn` images, by running it locally:

```
$ docker run -ti --rm quay.io/mudler/edgevpn -b -g
b3RwOgogIGRodDoKICAgIGludGVydmFsOiA5MDAwCiAgICBrZXk6IGtkdGtoY21sMHVJM2hzVUFUMXpUY1B2aDhBblkzNDZUbHJ3NklVRmUxYUoKICAgIGxlbmd0aDogNDMKICBjcnlwdG86CiAgICBpbnRlcnZhbDogOTAwMAogICAga2V5OiBIcEJGaGxxdlFrcTZVd3BPSTBPVkJWQ1daRjNRYlE3WGdDa1R1bnI0cGV3CiAgICBsZW5ndGg6IDQzCnJvb206IGFBUE5oRTdlODgyZUZhM2NMTW56VkM0ZDZjWFdpTU5EYlhXMDE4Skl2Q3oKcmVuZGV6dm91czogOHVzaGhzNnFrTU92U2ZvQmZXMHZPaEY1ZFlodVZlN1Flc00zRWlMM2pNMwptZG5zOiBJZ0ljaGlvRlVYOFN6V1VKQjNXQ0NyT2UzZXZ3YzE4MWVIWm42SmlYZjloCm1heF9tZXNzYWdlX3NpemU6IDIwOTcxNTIwCg==
```

This command will generate a network token that we can use in the configuration, which now looks like the following:

``` yaml
#cloud-config

# https://github.com/kairos-io/kairos/issues/885
config_url: ""

install:
 auto: true
 device: "auto"
 reboot: true

hostname: kairoslab-{{ trunc 4 .MachineID }}
users:
- name: kairos
  passwd: kairos
  ssh_authorized_keys:
  - github:mudler
  - github:mauromorales

p2p:
 disable_dht: true #Enabled by default
 network_token: "b3RwOgogIGRodDoKICAgIGludGVydmFsOiA5MDAwCiAgICBrZXk6IGtkdGtoY21sMHVJM2hzVUFUMXpUY1B2aDhBblkzNDZUbHJ3NklVRmUxYUoKICAgIGxlbmd0aDogNDMKICBjcnlwdG86CiAgICBpbnRlcnZhbDogOTAwMAogICAga2V5OiBIcEJGaGxxdlFrcTZVd3BPSTBPVkJWQ1daRjNRYlE3WGdDa1R1bnI0cGV3CiAgICBsZW5ndGg6IDQzCnJvb206IGFBUE5oRTdlODgyZUZhM2NMTW56VkM0ZDZjWFdpTU5EYlhXMDE4Skl2Q3oKcmVuZGV6dm91czogOHVzaGhzNnFrTU92U2ZvQmZXMHZPaEY1ZFlodVZlN1Flc00zRWlMM2pNMwptZG5zOiBJZ0ljaGlvRlVYOFN6V1VKQjNXQ0NyT2UzZXZ3YzE4MWVIWm42SmlYZjloCm1heF9tZXNzYWdlX3NpemU6IDIwOTcxNTIwCg=="
```

Change also accordingly the users that can access to the machine:

``` yaml
ssh_authorized_keys:
- github:mudler <--- put your GitHub handle here
```

## Provisioning with AuroraBoot

We now can run [AuroraBoot](/docs/reference/auroraboot) with `quay.io/kairos/kairos-opensuse-leap:v1.5.1-k3sv1.21.14-k3s1` to provision `openSUSE Leap` machines with `k3s 1.21.14` and Kairos `1.5.1`. 

AuroraBoot takes `cloud-config` files also from _STDIN_, so we will pipe the configuration file to it, and specify the container image that we want to use for our nodes:

``` bash
cat <<EOF | docker run --rm -i --net host quay.io/kairos/auroraboot \
                    --cloud-config - \
                    --set "container_image=quay.io/kairos/kairos-opensuse-leap:v1.5.1-k3sv1.21.14-k3s1"
#cloud-config

# https://github.com/kairos-io/kairos/issues/885
config_url: ""

install:
 auto: true
 device: "auto"
 reboot: true

hostname: kairoslab-{{ trunc 4 .MachineID }}
users:
- name: kairos
  passwd: kairos
  ssh_authorized_keys:
  - github:mudler
  - github:mauromorales
p2p:
 disable_dht: true #Enabled by default
 network_token: "b3RwOgogIGRodDoKICAgIGludGVydmFsOiA5MDAwCiAgICBrZXk6IGtkdGtoY21sMHVJM2hzVUFUMXpUY1B2aDhBblkzNDZUbHJ3NklVRmUxYUoKICAgIGxlbmd0aDogNDMKICBjcnlwdG86CiAgICBpbnRlcnZhbDogOTAwMAogICAga2V5OiBIcEJGaGxxdlFrcTZVd3BPSTBPVkJWQ1daRjNRYlE3WGdDa1R1bnI0cGV3CiAgICBsZW5ndGg6IDQzCnJvb206IGFBUE5oRTdlODgyZUZhM2NMTW56VkM0ZDZjWFdpTU5EYlhXMDE4Skl2Q3oKcmVuZGV6dm91czogOHVzaGhzNnFrTU92U2ZvQmZXMHZPaEY1ZFlodVZlN1Flc00zRWlMM2pNMwptZG5zOiBJZ0ljaGlvRlVYOFN6V1VKQjNXQ0NyT2UzZXZ3YzE4MWVIWm42SmlYZjloCm1heF9tZXNzYWdlX3NpemU6IDIwOTcxNTIwCg=="
EOF
```

## Booting and access the cluster

Start the Machines (VM, or baremetal) with Netboot ( see also [here](/docs/reference/auroraboot/#3-start-nodes) ) and wait for the installation to finish.

Afterward, you should be able to ssh to one of the machines and be able to use your Kubernetes cluster:

``` bash
$ ssh kairos@IP
$ sudo su -
$ kairos get-kubeconfig > kubeconfig
$ KUBECONFIG=kubeconfig k9s
```

## Notes

By default, the Kubernetes API endpoint is not exposed outside the VPN. This is an opinionated configuration from Kairos. To check out configurations without VPN, see also [the KubeVIP example](/docs/examples/multi-node-p2p-ha-kubevip).

## Troubleshooing

During the first-boot, you can check the provisioning status by looking at the `kairos-agent` logs:

``` bash
$ systemctl status kairos-agent
$ journalctl -fu kairos-agent
```

## See also

- [Installation with p2p](/docs/installation/p2p)
- [P2P Architecture](/docs/architecture/network)