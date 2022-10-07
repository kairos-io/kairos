---
layout: "../../layouts/docs/Layout.astro"
title: "Networking"
index: 8
---

# Networking settings

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

## Bonding

Bonding setup with Ubuntu can be configured via systemd-networkd (Ubuntu based images) and wicked (openSUSE based images), consider the following examples:

{{< tabs groupId="bonding">}}
{{% tab name="systemd-networkd" %}}

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
{{% tab name="wicked" %}}

```yaml
name: "My Deployment"
stages:
  boot:
    - name: "Setup network"
      commands:
        - wicked ifup bond0
  initramfs:
    - name: "Setup user and password"
      users:
        kairos:
          password: "kairos"
    - name: "Setup hostname"
      hostname: "hostname.domain.tld"
    - name: "Setup network files"
      files:
        - path: /etc/sysconfig/network/ifcfg-bond0
          content: |
            BONDING_MASTER='yes'
            BONDING_MODULE_OPTS='mode=802.3ad miimon=100 lacp_rate=fast'
            BONDING_SLAVE0='eth0'
            BONDING_SLAVE1='eth1'
            STARTMODE='onboot'
            BOOTPROTO='dhcp'
            HWADDR='11:22:33:44:55:66'
            ZONE='public'
            IPADDR=''
            NETMASK=''
          permissions: 0600
          owner: 0
          group: 0
        - path: /etc/sysconfig/network/ifcfg-eth0
          content: |
            BOOTPROTO='none'
            STARTMODE='hotplug'
          permissions: 0600
          owner: 0
          group: 0
        - path: /etc/sysconfig/network/ifcfg-eth1
          content: |
            BOOTPROTO='none'
            STARTMODE='hotplug'
          permissions: 0600
          owner: 0
          group: 0
  network:
    - name: "Setup user ssh-keys"
      authorized_keys:
        kairos:
          - "ssh-rsa AAA..."
          - "ssh-rsa AAA..."

k3s-agent:
  enabled: true
  env:
    K3S_TOKEN: "MySecretToken"
    K3S_URL: https://server.domain.tld:6443
```

{{% /tab %}}
{{< /tabs >}}

### References

- https://kerlilow.me/blog/setting-up-systemd-networkd-with-bonding/
