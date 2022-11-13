---
title: "Networking"
linkTitle: "Networking"
weight: 3
description: >
---

By default, Kairos ISOs are configured to automatically get an IP from the network interface. However, depending on the base system you have chosen, there are different way to configure networking. This section collects information on setting network configuration depending on the base that is being chosen (openSUSE, Alpine, Ubuntu).

There are different network managers depending on the distro:

- `connman` is available on Alpine-based distribution. By default is enabled on Kairos Alpine flavored variants.
- The openSUSE based flavor uses `wicked`
- The Ubuntu flavor uses `systemd-networkd`

## Static IP

To get a static IP, you can additionally define the following in your configuration file, depending on the network-manager being used:

{{< tabpane text=true right=true  >}}
{{% tab header="connman" %}}
```yaml
stages:
  initramfs:
    - files:
        - path: /var/lib/connman/default.config
          permission: 0644
          content: |
            [service_eth0]
            Type = ethernet
            IPv4 = 10.1.1.1/16/10.1.0.1
            Nameservers = 10.1.0.1
```
{{% /tab %}}
{{% tab header="systemd-networkd" %}}
```yaml
stages:
  initramfs:
    - files:
        - path: /etc/systemd/network/config.network
          permissions: 0644
          content: |
            [Match]
            Name=ens18

            [Network]
            Address=10.1.1.1/16
            Gateway=10.1.0.1
            DNS=10.1.0.1
```
{{% /tab %}}
{{< /tabpane >}}

## Bonding

Bonding setup with Ubuntu can be configured via systemd-networkd (Ubuntu based images) and wicked (openSUSE based images), consider the following examples:

{{< tabpane text=true right=true  >}}
{{% tab header="systemd-networkd" %}}
```yaml
#node-config
name: "My Deployment"
stages:
  boot:
    - name: "Setup network"
      commands:
        - systemctl restart systemd-networkd
  initramfs:
    # Drop network config file
    - name: "Setup hostname"
      hostname: "hostname"
    - name: "Setup network files"
      files:
        - path: /etc/systemd/network/10-bond0.network
          content: |
            [Match]
            Name=bond0
            [Network]
            DHCP=yes
          permissions: 0644
          owner: 0
          group: 0
        - path: /etc/systemd/network/10-bond0.netdev
          content: |
            [NetDev]
            Name=bond0
            Kind=bond
            [Bond]
            Mode=802.3ad
          permissions: 0644
          owner: 0
          group: 0
        - path: /etc/systemd/network/15-enp.network
          content: |
            [Match]
            Name=enp*
            [Network]
            Bond=bond0
          permissions: 0644
          owner: 0
          group: 0
        - path: /etc/systemd/network/05-bond0.link
          content: |
            [Match]
            Driver=bonding
            Name=bond0
            [Link]
            MACAddress=11:22:33:44:55:66
          permissions: 0644
          owner: 0
          group: 0
  network:
    - name: "Setup user ssh-keys"
      authorized_keys:
        kairos:
          - "ssh-rsa AAA..."
          - "ssh-rsa AAA..."
# k3s settings
k3s-agent:
  enabled: true
  env:
    K3S_TOKEN: "KubeSecret"
    K3S_URL: https://hostname:6443
```
{{% /tab %}}
{{% tab header="connman" %}}
```yaml
stages:
   boot:
     - name: "Setup network"
       commands:
       - modprobe bonding mode=4 miimon=100
       - ifenslave bond0 eno1
       - ifenslave bond0 eno2
       - ifenslave bond0 eno3
       - ifenslave bond0 eno4
       - ifconfig bond0 up hw ether 11:22:33:44:55:66
       - ifup bond0
       - sleep 5
       - rc-service connman restart 
   initramfs:
     - name: "Setup network files"
       files:
       - path: /var/lib/connman/default.config
         content: |
            [service_eth]
            Type = ethernet
            IPv4 = off
            IPv6 = off

            [service_bond0]
            Type = ethernet
            DeviceName = bond0
            IPv4 = dhcp
            MAC = 11:22:33:44:55:66
         permissions: 0644
         owner: 0
         group: 0
```
{{% /tab %}}
{{< /tabpane >}}

### References

- https://kerlilow.me/blog/setting-up-systemd-networkd-with-bonding/
