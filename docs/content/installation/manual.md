+++
title = "Manual installation"
date = 2022-02-09T17:56:26+01:00
weight = 3
chapter = false
pre = "<b>- </b>"
+++

Manual installation is available as well aside of pairing and automated installation. 

## Default credentials

If needed to connect over ssh, the system have an hardcoded username/password when booting from the LiveCD:

```
user: c3os
pass: c3os
```

{{% notice note %}}

Note, after the installation the password login is disabled, so users and ssh keys to login must be configured via cloud-init.

{{% /notice %}}


Login over SSH as the `c3os` user or via console with `c3os:c3os` and run:

```bash
sudo elemental install --cloud-init $CONFIG
```

Where the config can be a cloud-init file or a URL to it:

```yaml
#cloud-init

c3os:
  network_token: ....

# extra configuration
```

## Manual K3s configuration

Automatic nodes configuration can be disabled by disabling the `c3os` block in the configuration file.

In that case, VPN is not configured, but you can still configure k3s automatically with the `k3s` and `k3s-agent` block:

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

for agent:


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

See also the [examples](https://github.com/c3os-io/c3os/tree/master/examples) folder in the repository to configure k3s manually.
