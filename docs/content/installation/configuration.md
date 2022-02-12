+++
title = "Configuration reference"
date = 2022-02-09T17:56:26+01:00
weight = 4
chapter = false
pre = "<b>- </b>"
+++

A c3os node during pairing or either automated install can be configured via a single configuration file.

```yaml
c3os:
  network_token: "...."
  # Device for offline installs
  device: "/dev/sda"
  # Reboot after installation
  reboot: true
  # Power off after installation
  poweroff: true
  # Set to true when installing without Pairing
  offline: true

vpn:
  # EdgeVPN environment options
  DHCP: "true"
  # Disable DHT (for airgap)
  EDGEVPNDHT: "false"
  EDGEVPNMAXCONNS: "200"
  # See all EDGEVPN options:
  # - https://github.com/mudler/edgevpn/blob/master/cmd/util.go#L33
  # - https://github.com/mudler/edgevpn/blob/master/cmd/main.go#L48

# Cloud init syntax to setup users. 
# See https://rancher-sandbox.github.io/cos-toolkit-docs/docs/reference/cloud_init/
stages:
   network:
     - name: "Setup users"
       authorized_keys:
        c3os: 
        - github:mudler
```

## Datasource

The configuration file can also be used to drive automated installation and deployments by mounting an ISO in the node with the `cidata` label. The ISO must contain a `user-data` (which contain your configuration) and `meta-data` file.