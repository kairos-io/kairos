+++
title = "Interactive installation"
date = 2022-02-09T17:56:26+01:00
weight = 2
chapter = false
pre = "<b>-</b>"
+++

The interactive installation can be accessed from the LiveCD ISO and guides the user into the installation process.

It generates a configuration file, which is later accessible after installation at `/oem/99_custom.yaml`.

## From the boot menu

When loading any Kairos ISOs, a GRUB menu like the following will be displayed. To access the interactive installation, select the third entry (`kairos (interactive install)`).

![interactive](https://user-images.githubusercontent.com/2420543/189219819-6b16d13d-c409-4b9b-889b-12792f800a08.gif)

## Manually

The interactive installer can be also started manually with `kairos-agent interactive-install` from the LiveCD.
