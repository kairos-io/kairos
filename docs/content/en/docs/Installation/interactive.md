---
title: "Interactive"
linkTitle: "Interactive"
weight: 2
date: 2022-11-13
description: >
  Install Kairos interactively
---

The interactive installation can be accessed from the LiveCD ISO and guides the user into the installation process.

It generates a configuration file, which is later accessible after installation in the `/oem/99_custom.yaml` file.

## From the boot menu

When loading any Kairos ISOs, a GRUB menu, like the following will be displayed. To access the interactive installation, select the third entry (`kairos (interactive install)`).

![interactive](https://user-images.githubusercontent.com/2420543/189219819-6b16d13d-c409-4b9b-889b-12792f800a08.gif)

## Manually

The interactive installer can be also started manually with `kairos-agent interactive-install` from the LiveCD.
