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
  # Manually set node role. Available: master, worker. Defaults auto (none)
  role: "master"
  # User defined network-id. Can be used to have multiple clusters in the same network
  network_id: "dev"  
  # Enable embedded DNS See also: https://mudler.github.io/edgevpn/docs/concepts/overview/dns/
  dns: true

vpn:
  # EdgeVPN environment options
  DHCP: "true"
  # Disable DHT (for airgap)
  EDGEVPNDHT: "false"
  EDGEVPNMAXCONNS: "200"
  # If DHCP is false, it's required to be given a specific node IP. Can be arbitrary
  ADDRESS: "10.2.0.30/24" 
  # See all EDGEVPN options:
  # - https://github.com/mudler/edgevpn/blob/master/cmd/util.go#L33
  # - https://github.com/mudler/edgevpn/blob/master/cmd/main.go#L48

k3s:
  # Additional env/args for k3s server instances
  env:
    K3S_RESOLV_CONF: ""
    K3S_DATASTORE_ENDPOINT: "mysql://username:password@tcp(hostname:3306)/database-name"
  args:
  - --label ""
  - --data-dir ""
  # Enabling below it replaces args/env entirely
  # replace_env: true
  # replace_args: true

k3s-agent:
  # Additional env/args for k3s agent instances
  env:
    K3S_NODE_NAME: "foo"
  args:
  - --private-registry "..."
  # Enabling below it replaces args/env entirely
  # replace_env: true
  # replace_args: true

# Cloud init syntax to setup users. 
# See https://rancher-sandbox.github.io/cos-toolkit-docs/docs/reference/cloud_init/
stages:
   network:
     - name: "Setup users"
       authorized_keys:
        c3os: 
        - github:mudler
```


## Syntax

`c3os` supports the standard cloud-init syntax and the extended one from the [cOS toolkit](https://rancher-sandbox.github.io/cos-toolkit-docs/docs/reference/cloud_init/).

Examples using the extended notation for running k3s as agent or server are in [examples](https://github.com/c3os-io/c3os/tree/master/examples). 

## Datasource

The configuration file can also be used to drive automated installation and deployments by mounting an ISO in the node with the `cidata` label. The ISO must contain a `user-data` (which contain your configuration) and `meta-data` file.

## Embedded DNS

When `c3os.dns` is set to `true` embedded DNS is configured on the node. This allows to propagate custom records to the nodes by using the blockchain DNS server, for example, assuming `c3os bridge` is running in a separate terminal:

```bash
curl -X POST http://localhost:8080/api/dns --header "Content-Type: application/json" -d '{ "Regex": "foo.bar", "Records": { "A": "2.2.2.2" } }'
```

Will add the `foo.bar` domain with `2.2.2.2` as `A` response. 

Every node with `dns` enabled will be able to resolve the domain after the domain is correctly announced.

You can check out the dns in the [DNS page in the API](http://localhost:8080/dns.html), see also [the EdgeVPN docs](https://mudler.github.io/edgevpn/docs/concepts/overview/dns/).

Furthermore, is possible to tweak DNS server which are used to forward requests for domain listed outside, and as well it's possible to lock down resolving only to nodes in the blockchain, by customizing the configuration file:

```yaml
c3os:
  network_token: "...."
  # Enable embedded DNS See also: https://mudler.github.io/edgevpn/docs/concepts/overview/dns/
  dns: true

vpn:
  # Disable DNS forwarding
  DNSFORWARD: "false"
  # Set cache size
  DNSCACHESIZE: "200"
  # Set DNS forward server
  DNSFORWARDSERVER: "8.8.8.8:53"
```
