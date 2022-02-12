+++
title = "Device Pairing"
date = 2022-02-09T17:56:26+01:00
weight = 1
chapter = false
pre = "<b>- </b>"
+++

{{% notice note %}}
 Only the openSUSE variant supports automatic peer discovery and device pairing.
{{% /notice %}}

For pairing a c3os node, you will use the `c3os` CLI which is downloadable as part of the releases from another machine, which will be used to pair a new node.

## Start the c3os ISO

Download and mount the ISO in either baremetal or a VM that you wish to use as a node for your cluster.
It doesn't matter if you are joining a node to an existing cluster or creating a new one, the procedure is still the same.

A GRUB menu will be displayed:

![VirtualBox_test22_10_02_2022_20_56_55](https://user-images.githubusercontent.com/2420543/153488323-1ab451c3-d6ef-4109-b535-be8a823ba356.png?classes=border,shadow)

The first menu entry starts `c3os` in **Decentralized Device Pairing** pairing mode, while the second is reserved for manual installations.

Once booted the first entry, a boot splash screen will appear, and right after a QR code will be printed out of the screen
![VirtualBox_test22_10_02_2022_20_56_29](https://user-images.githubusercontent.com/2420543/153488315-a4290028-b856-436d-a43a-ea0404003fdf.png?classes=border,shadow)

## Prepare a configuration config file

In the machine you are using for bootstrapping (your workstation, a jumpbox, or ..)

Create a config file like the following, for example `config.yaml`:

```yaml
stages:
   network:
     - name: "Setup users"
       authorized_keys:
        c3os: 
        - github:mudler
c3os:
  network_token: "...."

vpn:
  # EdgeVPN environment options
  DHCP: "true"
```

{{% notice note %}}
If you are creating a new cluster, you need to create a new network token with the `c3os` CLI: `c3os generate-token`
{{% /notice %}}


The configuration config file is in [cloud-init](https://rancher-sandbox.github.io/cos-toolkit-docs/docs/reference/cloud_init/) syntax and you can customize it further to setup the machine behavior.

## Pair the machine

The VM once booted will print-out a QR code like the following:

![VirtualBox_test22_10_02_2022_20_56_36](https://user-images.githubusercontent.com/2420543/153488321-07e63e5f-d9e3-48ce-b551-8b457ece14a9.png?classes=border,shadow)


You can use it to pair the machine, by either providing a photo or by just calling `c3os register` which will by default take a screenshot:

```
c3os register --reboot --device /dev/sda --config config.yaml
```

Optionally we can specify an image where to extract the QR code from with:

```
c3os register --device /dev/sda --config config.yaml <file.png>
```

At this point, wait until the pairing is complete and the installation will start automatically in the new node.