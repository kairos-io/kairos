---
title: "Image support matrix"
linkTitle: "Image support matrix"
weight: 5
date: 2022-11-13
description: >
---

Kairos builds several artifacts for user convenience which are based on openSUSE, Alpine Linux, and Ubuntu.

{{% alert title="Note" color="info" %}}
The Kairos core team does its best to test all distributions but *systemd* based ones(e.g. _openSUSE_, _ubuntu_, ) are more throughly tested as are uniform and have homogeneus settings. 
Support for other flavors not using systemd (e.g. Alpine), although supported get less priority in our plannings due to the team bandwidth.

Besides, there is tighter integration with systemd for several features that are available only with it (e.g. [live layering](./docs/advanced/livelayering/) ).
{{% /alert %}}

The images are pushed over quay.io, and are available for installation and upgrading.

The installable mediums part of the releases are generated with the methods described in the [automated installation reference](/docs/installation/automated/#iso-remastering) from the images sources listed below, and the images can be used to point over for upgrades, as well.


| **Flavor/Variant**                                  	| **Core (no k3s)**                                       	| **Standard(k3s+opt.full-mesh)**                           	|
|-----------------------------------------------------	|---------------------------------------------------------	|-----------------------------------------------------------	|
| **openSUSE based**                                  	| https://quay.io/repository/kairos/core-opensuse         	| https://quay.io/repository/kairos/kairos-opensuse         	|
| **Ubuntu based (rolling)** **                                   	| https://quay.io/repository/kairos/core-ubuntu           	| https://quay.io/repository/kairos/kairos-ubuntu           	|
| **Ubuntu based (22 LTS)** **                                    	| https://quay.io/repository/kairos/core-ubuntu-22-lts           	| https://quay.io/repository/kairos/kairos-ubuntu-22-lts           	|
| **Ubuntu based (20 LTS)** **                                   	| https://quay.io/repository/kairos/core-ubuntu-20-lts           	| https://quay.io/repository/kairos/kairos-ubuntu-20-lts           	|
| **Alpine Linux based (openSUSE kernel)**                              	| https://quay.io/repository/kairos/core-alpine-opensuse-leap           	| https://quay.io/repository/kairos/kairos-alpine-opensuse-leap           	|
| **Alpine Linux based (Ubuntu kernel)**                              	| https://quay.io/repository/kairos/core-alpine-ubuntu           	| https://quay.io/repository/kairos/kairos-alpine-ubuntu           	|
| **openSUSE based (RaspberryPi 3 and 4, arm64)**     	| https://quay.io/repository/kairos/core-opensuse-arm-rpi 	| https://quay.io/repository/kairos/kairos-opensuse-arm-rpi 	|
| **Alpine Linux based (RaspberryPi 3 and 4, arm64)** 	| https://quay.io/repository/kairos/core-alpine-arm-rpi   	| https://quay.io/repository/kairos/kairos-alpine-arm-rpi   	|

{{% alert title="Note" color="info" %}}
** the `ubuntu` flavor tracks the latest available Ubuntu release (at the time of writing 22.10). the LTS flavors instead are tracking the latest LTS available in dockerhub. i.e. ubuntu-22-lts uses 22.04 as base image
{{% /alert %}}

{{% alert title="Note" color="info" %}}
  The pipelines don't publish `img` artifacts for arm architecture because the files are too big for Github Actions (they are above the artifact size limit).
  They can be extracted from the published docker images with the following command:

  ```bash
  export IMAGE=quay.io/kairos/core-alpine-arm-rpi:v1.1.7.img
  docker run -ti --rm -v $PWD:/image quay.io/luet/base util unpack "$IMAGE" /image
  ```

  (replace with the proper image)

  The artifacts are in the `build/` directory.
{{% /alert %}}
