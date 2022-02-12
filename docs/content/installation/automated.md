+++
title = "Automated installation"
date = 2022-02-09T17:56:26+01:00
weight = 2
chapter = false
pre = "<b>- </b>"
+++

Automated installation is available as well aside of manual pairing. 

A cloud-init of the following form can be supplied as a datasource (CDROM, etc.):

```yaml
#cloud-init

c3os:
  device: "/dev/sda"
  reboot: true
  poweroff: true
  offline: true # Required, for automated installations
  network_token: ....

# extra configuration
```

which will drive the installation automatically