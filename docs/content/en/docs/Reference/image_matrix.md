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
Support for other flavors not using systemd (e.g. Alpine), get less priority in our plannings due to the team bandwidth. We encourage you to contribute, as Kairos is a community-driven, Open Source Project, and we welcome any contribution, bugreporting, bugfixing, check out our [Contribution guidelines](https://github.com/kairos-io/kairos/contribute)!

Besides, there is tighter integration with systemd for several features that are available only with it (e.g. [live layering](/docs/advanced/livelayering/) ).
{{% /alert %}}

The images are pushed over quay.io, and are available for installation and upgrading.

The installable mediums part of the releases are generated with the methods described in the [automated installation reference](/docs/installation/automated/#iso-remastering) from the images sources listed below, and the images can be used to point over for upgrades, as well.

## Image flavors

Kairos release process generates images based on official container images from the major popular Linux Distributions, If you can't find your preferred distribution first check [if we are already planning](https://github.com/kairos-io/kairos/issues?q=is%3Aopen+is%3Aissue+label%3Aarea%2Fflavor) support for it, or create a new issue.

Below you can find a list of the images and their respective location on the `quay.io` registry.

- The **Core** images does not contain any Kubernetes engine. Those images can be used as base for manual customization.
- The **Standard** images contains `k3s` and the `kairos` provider which enables Kubernetes deployments, optionally with [p2p](/docs/installation/p2p).

Base images are tagged against specific upstream versions (e.g _Ubuntu 20 LTS_ pins to _Ubuntu 20:04_, _opensuse_ to _opensuse leap 15.4_, ...).

| **Flavor/Variant**                                  	| **Core (no k3s)**                                       	| **Standard(k3s)**                           	|
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


## Versioning policy

Kairos follows [Semantic Versioning](https://semver.org/) and our releases versioning signal changes regarding the Kairos components, not changes to the OS, and underlaying packages versions. Flavors are pinned to specific upstream OS branches (e.g. `opensuse` to `leap 15.4`) and major version bumps would be reflected by either having new flavors in our build matrix or having specific releases to follow upstream with regard to minor version bumps (e.g. `leap 15.3` and `leap 15.4`).

Consider:
- We support only the latest release branch with patch releases.
- Patch releases ( e.g. _1.1.x_ ) follows a weekly release cadence, if needed, exceptions made for highly impacting bugs in Kairos itself or at the OS layer (e.g. High scored CVEs).
- Minor releases follows a monthly cadence and are expected to bring enhancements, with planned releases.
- Major releases signal a new a set of advanced features, or significant changes to the codebase. In-place upgrade from old to new major release branch is not always guaranteed, however we steer to keep compatibility among versions.

{{% alert title="Note" color="info" %}}
In order to give more control over the chosen base image (e.g. `openSUSE`, `Ubuntu`, ... ), and rely less on our CI infrastructure, we are actively working on streamling the creation of Kairos-based distributions directly from upstream base images. You can track the development progress over [here](https://github.com/kairos-io/kairos/issues/116).

If you need to further customize images, including base image changes, package updates, and CVE hotfixes, follow the [customization docs](/docs/advanced/customizing).
{{% /alert %}}


## Release changelog

Our changelog is published as part of the release process: it contains all the changeset, highlights and release notes that are pertinent to the release.

We strongly reccomend to check every release changelog before running any upgrade, or while building your customized version of Kairos.

Release changelogs are available for Kairos core and for each component. Here you can find a list of the components that are part of a Kairos release and their respective release page with changelogs.



| **Project**                                  	| **Release page**                                       	|
|-----------------------------------------------------	|---------------------------------------------------------	|
| **Kairos core**                                  	|    https://github.com/kairos-io/kairos/releases      	|
| **Kairos provider (k3s support)**                 |    https://github.com/kairos-io/provider-kairos/releases |