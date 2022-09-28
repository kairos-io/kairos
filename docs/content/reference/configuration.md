+++
title = "Configuration reference"
date = 2022-02-09T17:56:26+01:00
weight = 4
chapter = false
pre = "<b>- </b>"
+++

Here you can find a full reference of the fields available to configure a Kairos node.

```yaml
#node-config

# The kairos block enables the p2p full-mesh functionalities.
# To disable, don't specify one.
kairos:
  # This is a network token used to establish the p2p full meshed network.
  # Don't specify one to disable full-mesh functionalities.
  network_token: "...."
  # Manually set node role. Available: master, worker. Defaults auto (none). This is available 
  role: "master"
  # User defined network-id. Can be used to have multiple clusters in the same network
  network_id: "dev"  
  # Enable embedded DNS See also: https://mudler.github.io/edgevpn/docs/concepts/overview/dns/
  dns: true

# The install block is to drive automatic installations without user interaction.
install:
  # Device for automated installs
  device: "/dev/sda"
  # Reboot after installation
  reboot: true
  # Power off after installation
  poweroff: true
  # Set to true when installing without Pairing
  auto: true
  # Add bundles in runtime
  bundles:
  - ...
  # Set grub options
  grub_options:
    key: value

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

# Additional cloud init syntax can be used here.
# See https://rancher.github.io/elemental-toolkit/docs/reference/cloud_init/ for a complete reference
stages:
   network:
     - name: "Setup users"
       authorized_keys:
        kairos: 
        - github:mudler
```


## Syntax

Kairos supports the standard `cloud-init` syntax, and the extended one from the [Elemental-toolkit](https://rancher.github.io/elemental-toolkit/docs/reference/cloud_init/) which is based on [yip](https://github.com/mudler/yip).

Examples using the extended notation for running K3s as agent or server are in [examples](https://github.com/kairos-io/kairos/tree/master/examples).

### `k3s`

The `k3s` and the `k3s-agent` block are used to customize the environment and argument settings of K3s, consider:

```yaml
k3s:
  enabled: true
  # Additional env/args for k3s server instances
  env:
    K3S_RESOLV_CONF: ""
    K3S_DATASTORE_ENDPOINT: "mysql://username:password@tcp(hostname:3306)/database-name"
  args:
  - --cluster-init
```

For the Agent:


```yaml
k3s-agent:
  enabled: true
  # Additional env/args for k3s server instances
  env:
    K3S_RESOLV_CONF: ""
    K3S_DATASTORE_ENDPOINT: "mysql://username:password@tcp(hostname:3306)/database-name"
  args:
  - --cluster-init
```

See also the [examples'](https://github.com/kairos-io/kairos/tree/master/examples) folder in the repository to configure K3s manually.

## `install.grub_options`

Is a map of key/value grub options to be set in the grub environment after installation.

It can be used to set additional boot arguments on boot, consider to set `panic=0` as bootarg:

```yaml
#node-config

install:
  # See also: https://rancher.github.io/elemental-toolkit/docs/customizing/configure_grub/#grub-environment-variables
  grub_options:
    extra_cmdline: "panic=0"
```

Below a full list of all the available options:


| Variable               |  Description                                            |
|------------------------|---------------------------------------------------------|
| next_entry             | Set the next reboot entry                               |
| saved_entry            | Set the default boot entry                              |
| default_menu_entry     | Set the name entries on the GRUB menu                   |
| extra_active_cmdline   | Set additional boot commands when booting into active   |
| extra_passive_cmdline  | Set additional boot commands when booting into passive  |
| extra_recovery_cmdline | Set additional boot commands when booting into recovery |
| extra_cmdline          | Set additional boot commands for all entries            |
| default_fallback       | Sets default fallback logic                             |


### `kairos.dns`

When the `kairos.dns` is set to `true` the embedded DNS is configured on the node. This allows to propagate custom records to the nodes by using the blockchain DNS server. For example, this is assuming `kairos bridge` is running in a separate terminal:

```bash
curl -X POST http://localhost:8080/api/dns --header "Content-Type: application/json" -d '{ "Regex": "foo.bar", "Records": { "A": "2.2.2.2" } }'
```

It will add the `foo.bar` domain with `2.2.2.2` as `A` response. 

Every node with DNS enabled will be able to resolve the domain after the domain is correctly announced.

You can check out the DNS in the [DNS page in the API](http://localhost:8080/dns.html), see also the [EdgeVPN docs](https://mudler.github.io/edgevpn/docs/concepts/overview/dns/).

Furthermore, it is possible to tweak the DNS server which are used to forward requests for domain listed outside, and as well, it's possible to lock down resolving only to nodes in the blockchain, by customizing the configuration file:

```yaml
#cloud-config
kairos:
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