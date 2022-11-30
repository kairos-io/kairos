---
title: "Configuration"
linkTitle: "Configuration"
weight: 2
date: 2022-11-13
description: >
---

Here you can find a full reference of the fields available to configure a Kairos node.

```yaml
#cloud-config

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
    # additional Kernel option cmdline to apply
    extra_cmdline: "config_url=http://"
    # Same, just for active
    extra_active_cmdline: ""
    # Same, just for passive
    extra_passive_cmdline: ""
    # Change GRUB menu entry
    default_menu_entry: ""
  # Environmental variable to set to the installer calls
  env:
  - foo=bar

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
    - --node-label ""
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

# Various options.
# Those could apply to install, or other phases as well
options:
  # Specify an alternative image to use during installation
  system.uri: ""
  # Specify an alternative recovery image to use during installation
  recovery-system.uri: ""
  # Just set it to eject the cd after install
  eject-cd: ""

# Standard cloud-init syntax, see: https://github.com/mudler/yip/tree/e688612df3b6f24dba8102f63a76e48db49606b2#compatibility-with-cloud-init-format
growpart:
 devices: ['/']

users:
- name: "kairos"
  passwd: "kairos"
  lock_passwd: true
  groups: "admin"
  ssh_authorized_keys:
  - github:mudler

runcmd:
- foo
hostname: "bar"
write_files:
- encoding: b64
  content: CiMgVGhpcyBmaWxlIGNvbnRyb2xzIHRoZSBzdGF0ZSBvZiBTRUxpbnV4
  path: /foo/bar
  permissions: "0644"
  owner: "bar"
```

## Syntax

Kairos supports the standard `cloud-init` syntax, and the extended syntax which is based on [yip](https://github.com/mudler/yip).

Examples using the extended notation for running K3s as agent or server are in [examples](https://github.com/kairos-io/kairos/tree/master/examples).

For instance, to set up the DNS at the boot stage:

```yaml
stages:
  boot:
    - name: "DNS settings"
      dns:
        path: /etc/resolv.conf
        nameservers:
          - 8.8.8.8
```

The extended syntax can be also used to pass-by commands via Kernel boot parameters, see examples below. 

## Test your cloud configs

Writing YAML files can be a tedious process, where syntax or intendation errors might occour.

To test your configuration, you can leverage the cloud-init commands, to test locally in a container, for instance, consider:

```bash

$ ls -liah
total 32K
38548066 drwxr-xr-x 2 mudler mudler 4.0K Nov 12 19:21 .
38548063 drwxr-xr-x 3 mudler mudler 4.0K Nov 12 19:21 ..
38548158 -rw-r--r-- 1 mudler mudler 1.4K Nov 12 19:21 00_rootfs.yaml
38548159 -rw-r--r-- 1 mudler mudler 1.1K Nov 12 19:21 06_recovery.yaml
38552350 -rw-r--r-- 1 mudler mudler  608 Nov 12 19:21 07_live.yaml
38552420 -rw-r--r-- 1 mudler mudler 5.3K Nov 12 19:21 08_boot_assessment.yaml
38553235 -rw-r--r-- 1 mudler mudler  380 Nov 12 19:21 09_services.yaml

$ docker run -ti -v $PWD:/test --entrypoint /usr/bin/elemental --rm quay.io/kairos/core-alpine cloud-init /test
INFO[2022-11-18T08:51:33Z] Starting elemental version v0.0.1
INFO[2022-11-18T08:51:33Z] Running stage: default
INFO[2022-11-18T08:51:33Z] Executing /test/00_rootfs.yaml
INFO[2022-11-18T08:51:33Z] Executing /test/06_recovery.yaml
INFO[2022-11-18T08:51:33Z] Executing /test/07_live.yaml
INFO[2022-11-18T08:51:33Z] Executing /test/08_boot_assessment.yaml
INFO[2022-11-18T08:51:33Z] Executing /test/09_services.yaml
INFO[2022-11-18T08:51:33Z] Done executing stage 'default'
```

Note that by default the "default" stage is executed - which doesn't actually map to any stage, to test, for instance other stage, we can use the `--stage (-s)` option, for example for `initramfs`:

```bash
$ docker run -ti -v $PWD:/test --entrypoint /usr/bin/elemental --rm quay.io/kairos/core-alpine cloud-init -s initramfs /test
```

It is possible also to test individual file by piping them to cloud-init, consider:

```bash
cat <<EOF | docker run -i --rm --entrypoint /usr/bin/elemental quay.io/kairos/core-alpine cloud-init -s test -
stages:
 test:
 - commands:
   - echo "test"
EOF

# INFO[2022-11-18T08:53:45Z] Starting elemental version v0.0.1
# INFO[2022-11-18T08:53:45Z] Running stage: test
# INFO[2022-11-18T08:53:45Z] Executing stages:
# test:
# - commands:
#   - echo "test"
# INFO[2022-11-18T08:53:45Z] Applying '' for stage 'test'. Total stages: 1
# INFO[2022-11-18T08:53:45Z] Processing stage step ''. ( commands: 1, files: 0, ... )
# INFO[2022-11-18T08:53:45Z] Command output: test
# INFO[2022-11-18T08:53:45Z] Stage 'test'. Defined stages: 1. Errors: false
# INFO[2022-11-18T08:53:45Z] Done executing stage 'test'
```

## Automatic Hostname at scale

Sometimes you may want to create a single `cloud-init` file for a set of machines and also make sure each node has a different hostname.

The cloud-config syntax supports templating, so you can automate hostname generation based on the `machine ID` which is generated for each host:

```yaml
#node-config
stages:
  initramfs:
    - name: "Setup hostname"
      hostname: "node-{{ trunc 4 .MachineID }}"
```

### `k3s`

The `k3s` and the `k3s-agent` block are used to customize the environment and argument settings of K3s, consider:

{{< tabpane text=true right=true  >}}
{{% tab header="server" %}}
```yaml
k3s:
  enabled: true
  # Additional env/args for k3s server instances
  env:
    K3S_RESOLV_CONF: ""
    K3S_DATASTORE_ENDPOINT: ""
  args:
    - ...
```
{{% /tab %}}
{{% tab header="agent" %}}
```yaml
k3s-agent:
  enabled: true
  # Additional env/args for k3s server instances
  env:
    K3S_RESOLV_CONF: ""
    K3S_DATASTORE_ENDPOINT: ""
  args:
    -
```
{{% /tab %}}
{{< /tabpane >}}

See also the [examples](https://github.com/kairos-io/kairos/tree/master/examples) folder in the repository to configure K3s manually.

## `install.grub_options`

This is a map of key/value GRUB options to be set in the GRUB environment after installation.

It can be used to set additional boot arguments on boot, consider to set `panic=0` as bootarg:

```yaml
#node-config

install:
  # See also: https://rancher.github.io/elemental-toolkit/docs/customizing/configure_grub/#grub-environment-variables
  grub_options:
    extra_cmdline: "panic=0"
```

Below a full list of all the available options:

| Variable               | Description                                             |
| ---------------------- | ------------------------------------------------------- |
| next_entry             | Set the next reboot entry                               |
| saved_entry            | Set the default boot entry                              |
| default_menu_entry     | Set the name entries on the GRUB menu                   |
| extra_active_cmdline   | Set additional boot commands when booting into active   |
| extra_passive_cmdline  | Set additional boot commands when booting into passive  |
| extra_recovery_cmdline | Set additional boot commands when booting into recovery |
| extra_cmdline          | Set additional boot commands for all entries            |
| default_fallback       | Sets default fallback logic                             |

## Kubernetes manifests

When using the `k3s` as Kubernetes distribution, it's possible to automatically deploy Helm charts or Kubernetes resources automatically after deployment, for instance to deploy fleet automatically:

```yaml
name: "Deploy fleet out of the box"
stages:
  boot:
    - name: "Copy fleet deployment files"
      files:
        - path: /var/lib/rancher/k3s/server/manifests/fleet-config.yaml
          content: |
            apiVersion: v1
            kind: Namespace
            metadata:
              name: cattle-system
            ---
            apiVersion: helm.cattle.io/v1
            kind: HelmChart
            metadata:
              name: fleet-crd
              namespace: cattle-system
            spec:
              chart: https://github.com/rancher/fleet/releases/download/v0.3.8/fleet-crd-0.3.8.tgz
            ---
            apiVersion: helm.cattle.io/v1
            kind: HelmChart
            metadata:
              name: fleet
              namespace: cattle-system
            spec:
              chart: https://github.com/rancher/fleet/releases/download/v0.3.8/fleet-0.3.8.tgz
```

## Kernel boot parameters

All the configurations can be issued via Kernel boot parameters, for instance, consider to add an user from the boot menu:

`stages.boot[0].authorized_keys.root[0]=github:mudler`

Or to either load a config url from network:

`config_url=http://...`

Usually secret gists are used to share such config files.

## Additional users

Kairos comes with the `kairos` user pre-configured, however, it is possible to configure additional users to the system via the cloud-init config mechanism

### Add a user during first-install

Consider the following example cloud-config, which adds the `testuser` user to the system with admin access:

```yaml
#cloud-config
install:
  device: /dev/sda
k3s:
  enabled: true

users:
- name: "kairos"
  passwd: "kairos"
  ssh_authorized_keys:
  - github:mudler
- name: "testuser"
  passwd: "testuser"
  ssh_authorized_keys:
  - github:mudler
  groups:
  - "admin"
```

### Add a user to an existing install

To add an user to an existing installation you can simply add a `/oem` file for the new user. For instance, consider the following:
```yaml
stages:
   initramfs:
     - name: "Set user and password"
       users:
        testuser:
          groups:
          - "admin"
          passwd: "mypassword"
          shell: /bin/bash
          homedir: "/home/testuser"
```

This configuration can be either manually copied over, or can be propagated also via Kubernetes using the system upgrade controller. See [the after-install](/docs/advanced/after-install) section for an example.

```bash
❯ ssh testuser@192.168.1.238
testuser@192.168.1.238's password:
Welcome to kairos!

Refer to https://kairos.io for documentation.
localhost:~$ sudo su -
localhost:~# whoami
root
localhost:~# exit
localhost:~$ whoami
testuser
localhost:~$
```

## P2P configuration

P2P functionalities are experimental Kairos features and disabled by default. In order to enable them, just use the `kairos` configuration block.

### `kairos.network_token`

This defines the network token used by peers to join the p2p virtual private network. You can generate it with the Kairos CLI with `kairos generate-token`. Check out [the P2P section](/docs/installation/p2p) for more instructions.

### `kairos.role`

Define a role for the node. Accepted: `worker`, `master`. Currently only one master is supported.

### `kairos.id`

Define a custom ID for the Kubernetes cluster. This can be used to create multiple clusters in the same network segment by specifying the same id across nodes with the same network token. Accepted: any string.

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
