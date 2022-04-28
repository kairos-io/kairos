+++
title = "VPN Recovery mode"
date = 2022-02-09T17:56:26+01:00
weight = 4
chapter = false
pre = "<b>- </b>"
+++

The c3os vpn recovery mode can be used to recover a damaged system, or to regain access remotely (with assistance) to a machine which has been lost access to. The recovery mode is accessible only from the GRUB menu, from both the LiveCD and an installed system.

{{% notice note %}}
On installed system there are two recovery modes available during boot. Below it is described only how the VPN recovery works. The manual recovery entry has nothing special from the standard cOS recovery mode. It can be used to reset the A/B partitions (with the user/pass used during setup) and perform any other operation without remote VPN access.
{{% /notice %}}

## Boot into recovery mode

VPN recovery mode can be accessed either via ISO or from an installed system.

A GRUB menu will be displayed:
![Screenshot from 2022-04-28 17-48-06](https://user-images.githubusercontent.com/2420543/165800177-3e4cccd8-f67c-43a2-bd88-329478539400.png)

Select the last entry `c3os (vpn recovery mode)` and press enter.

At this point the boot process starts and you should be welcomed by the `c3os` screen: 

![Screenshot from 2022-04-28 17-48-32](https://user-images.githubusercontent.com/2420543/165800182-9aa29c90-09e9-4c53-b3c7-c8ced262e3ac.png)

After few second the recovery process starts, and right after a QR code will be printed out of the screen along with a password which can be used later on to SSH into the machine:

![Screenshot from 2022-04-28 17-48-43](https://user-images.githubusercontent.com/2420543/165800187-4d2fe04e-c501-4ad8-a29f-32a0110eaa72.png)

At this stage, take a screenshot or a photo, just save the image with the QR code.

## Connect to the machine

In another machine you are using to connect to your server (your workstation, a jumpbox, or ..) use the `c3os` CLI to connect over the remote machine:

```bash
sudo ./c3os bridge --qr-code-image /path/to/image.png
```

At this point the bridge should start, and you should be able to see connection messages in the terminal. The machine in recovery mode will be accessible at `10.1.0.20`. The bridge operates in the foreground, so you have to kill by hitting CTRL-C.

In another terminal now indeed you can try to ping the machine (note, it might take some delay to create the tunnel, so first attempts might fail):

```bash
$ ping 10.1.0.20                                               
PING 10.1.0.20 (10.1.0.20) 56(84) bytes of data.               
64 bytes from 10.1.0.20: icmp_seq=16 ttl=64 time=685 ms                                                                        
64 bytes from 10.1.0.20: icmp_seq=17 ttl=64 time=444 ms  
```

once you can ping the machine, you should be able to ssh with the password showed below the QR code:

```
ssh 10.1.0.20 -p 2222
```
