+++
title = "Networking"
date = 2022-02-09T17:56:26+01:00
weight = 5
chapter = false
pre = "<b>- </b>"
+++

By default Kairos ISOs are configured to automatically get an ip from the network interface. However, depending on the base system you have chosen, there are different way to configure networking. This section collects information on setting network configuration depending on the base that is being chosen (openSUSE, Alpine, Ubuntu).

There are different network manager depending on the distro:
- `connman` is available on Alpine based distribution. By default is enabled on Kairos Alpine flavored variants.
- The openSUSE based flavor uses `wicked`
- The Ubuntu flavor uses `systemd-networkd`

## Static IP

To get a static IP, you can additionally define the following in your config file, depending on the network-manager being used:

{{< tabs groupId="staticIP">}}
{{% tab name="connman" %}}
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
{{% tab name="wicked" %}}
```yaml
name: "Default network configuration"
stages:
   boot:
     - commands:
       - wicked ifup eth0
   initramfs:
     - name: "Setup network"
       files:
       - path: /etc/sysconfig/network/ifcfg-eth0
         content: |
            BOOTPROTO='static'
            IPADDR='192.168.1.2/24'            
         permissions: 0600
         owner: 0
         group: 0
```
{{% /tab %}}
{{% tab name="systemd-networkd" %}}
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
{{< /tabs >}}

