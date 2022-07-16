+++
title = "Automated installation"
date = 2022-02-09T17:56:26+01:00
weight = 2
chapter = false
pre = "<b>- </b>"
+++

Automated installation is available as well aside of manual pairing. 

A cloud-init of the following form can be supplied as a datasource (CDROM, `cos.setup` bootarg):

```yaml
#cloud-config

install:
  device: "/dev/sda"
  reboot: true
  poweroff: true
  auto: true # Required, for automated installations
  
c3os:
  network_token: ....

# extra configuration
```

which will drive the installation automatically on first boot. 

The installer will kick in automatically and `reboot`/`poweroff` if specified. Note, to trigger the automatic installation the `offline` field must be enabled.