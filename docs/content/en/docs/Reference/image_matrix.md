---
title: "Image support matrix"
linkTitle: "Image support matrix"
weight: 5
date: 2022-11-13
description: >
---

Kairos offers several pre-built images for user convenience based on popular Linux distributions such as openSUSE, Alpine Linux, and Ubuntu. The Kairos core team does its best to test these images, but those that are based on systemd (e.g. openSUSE, Ubuntu) are more thoroughly tested due to their homogenous settings. Support for other non-systemd based flavors (e.g. Alpine) may be limited due to team bandwidth. However, as Kairos is an open source community-driven project, we welcome any contributions, bug reports, and bug fixes. Check out our [Contribution guidelines](https://github.com/kairos-io/kairos/contribute) for more information.

In addition, tighter integration with systemd allows for several features that are only available with it, such as live layering.

These images are pushed to quay.io and are available for installation and upgrading. The installable mediums included in the releases are generated using the methods described in the [automated installation reference](/docs/installation/automated/#iso-remastering), and the images can be used for upgrades as well.

## Image flavors

Kairos release processes generates images based on official container images from popular Linux distributions. If you don't see your preferred distribution, check if [we are already planning](https://github.com/kairos-io/kairos/issues?q=is%3Aopen+is%3Aissue+label%3Aarea%2Fflavor) support for it or create a new issue.

Below is a list of the available images and their locations on the quay.io registry:

- The **Core** images do not include any Kubernetes engine and can be used as a base for customizations.
- The **Standard** images include `k3s` and the [kairos provider](https://github.com/kairos-io/provider-kairos), which enables Kubernetes deployments and optionally enables [p2p](/docs/installation/p2p).

Base images are tagged with specific upstream versions (e.g. Ubuntu 20 LTS is pinned to Ubuntu 20:04, openSUSE to openSUSE leap 15.4, etc.).

| **Flavor/Variant**                                  	                   | **Core (no k3s)**                                       	               | **Standard(k3s)**                           	                             |
|-------------------------------------------------------------------------|-------------------------------------------------------------------------|---------------------------------------------------------------------------|
| **openSUSE Leap based**                                  	              | https://quay.io/repository/kairos/core-opensuse-leap         	          | https://quay.io/repository/kairos/kairos-opensuse-leap         	          |
| **openSUSE Tumbleweed based**                                  	        | https://quay.io/repository/kairos/core-tumbleweed-leap         	        | https://quay.io/repository/kairos/kairos-opensuse-tumbleweed         	    |
| **Fedora based**                                  	                     | https://quay.io/repository/kairos/core-fedora         	                 | https://quay.io/repository/kairos/kairos-fedora         	                 |
| **Debian based**                                  	                     | https://quay.io/repository/kairos/core-debian         	                 | https://quay.io/repository/kairos/kairos-debian         	                 |
| **Ubuntu based (rolling)** **                                   	       | https://quay.io/repository/kairos/core-ubuntu           	               | https://quay.io/repository/kairos/kairos-ubuntu           	               |
| **Ubuntu based (22 LTS)** **                                    	       | https://quay.io/repository/kairos/core-ubuntu-22-lts           	        | https://quay.io/repository/kairos/kairos-ubuntu-22-lts           	        |
| **Ubuntu based (20 LTS)** **                                   	        | https://quay.io/repository/kairos/core-ubuntu-20-lts           	        | https://quay.io/repository/kairos/kairos-ubuntu-20-lts           	        |
| **Alpine Linux based (openSUSE kernel)**                              	 | https://quay.io/repository/kairos/core-alpine-opensuse-leap           	 | https://quay.io/repository/kairos/kairos-alpine-opensuse-leap           	 |
| **Alpine Linux based (Ubuntu kernel)**                              	   | https://quay.io/repository/kairos/core-alpine-ubuntu           	        | https://quay.io/repository/kairos/kairos-alpine-ubuntu           	        |
| **openSUSE Leap based (RaspberryPi 3 and 4, arm64)**     	              | https://quay.io/repository/kairos/core-opensuse-leap-arm-rpi 	          | https://quay.io/repository/kairos/kairos-opensuse-leap-arm-rpi 	          |
| **openSUSE Tumbleweed based (RaspberryPi 3 and 4, arm64)**     	        | https://quay.io/repository/kairos/core-opensuse-tumbleweed-arm-rpi 	    | https://quay.io/repository/kairos/kairos-opensuse-tumbleweed-arm-rpi 	        |
| **Alpine Linux based (RaspberryPi 3 and 4, arm64)** 	                   | https://quay.io/repository/kairos/core-alpine-arm-rpi   	               | https://quay.io/repository/kairos/kairos-alpine-arm-rpi   	               |
| **Rocky Linux based** 	                   | https://quay.io/repository/kairos/core-rockylinux   	               |                |
| **Debian Linux based** 	                   | https://quay.io/repository/kairos/core-debian   	               | https://quay.io/repository/kairos/kairos-debian   	               |

{{% alert title="Note" color="info" %}}

** The `ubuntu` flavor tracks the latest available Ubuntu release (at the time of writing 22.10). The LTS flavors, on the other hand, track the latest LTS available on DockerHub. For example, ubuntu-22-lts uses 22.04 as the base image.
{{% /alert %}}

{{% alert title="Note" color="info" %}}
The pipelines do not publish `img` artifacts for the arm architecture because the files are too large for GitHub Actions (they exceed the artifact size limit). These artifacts can be extracted from the published docker images using the following command:

```bash
export IMAGE=quay.io/kairos/core-alpine-arm-rpi:v1.1.7.img
docker run -ti --rm -v $PWD:/image quay.io/luet/base util unpack "$IMAGE" /image
```

(replace `$IMAGE` with the proper image)

The artifacts can be found in the `build` directory.

{{% /alert %}}


## Versioning policy

Kairos follows [Semantic Versioning](https://semver.org/) and our releases signal changes to Kairos components, rather than changes to the underlying OS and package versions. Flavors are pinned to specific upstream OS branches (e.g. `opensuse` to `leap 15.4`) and major version bumps will be reflected through new flavors in our build matrix or through specific releases to follow upstream with regard to minor version bumps (e.g. `leap 15.3` and `leap 15.4`).

Here are some key points to note:
- We only support the latest release branch with patch releases.
- Patch releases (e.g. _1.1.x_) follow a weekly release cadence, unless there are exceptions for highly impactful bugs in Kairos itself or at the OS layer (e.g. high-severity CVEs).
- Minor releases follow a monthly cadence and are expected to bring enhancements through planned releases.
- Major releases signal new advanced features or significant changes to the codebase. In-place upgrades from old to new major release branches are not always guaranteed, but we strive for compatibility across versions.

{{% alert title="Note" color="info" %}}
In order to give users more control over the chosen base image (e.g. `openSUSE`, `Ubuntu`, etc.) and reduce reliance on our CI infrastructure, we are actively working on streamlining the creation of Kairos-based distributions directly from upstream base images. You can track the development progress [here](https://github.com/kairos-io/kairos/issues/116).

If you need to further customize images, including changes to the base image, package updates, and CVE hotfixes, check out the [customization docs](/docs/advanced/customizing).
{{% /alert %}}


## Release changelog

Our changelog is published as part of the release process and contains all the changes, highlights, and release notes that are relevant to the release. We strongly recommend checking the changelog for each release before upgrading or building a customized version of Kairos.

Release changelogs are available for Kairos core and for each component. Below is a list of the components that are part of a Kairos release and their respective release pages with changelogs.

| **Project**                                  	| **Release page**                                       	|
|-----------------------------------------------------	|---------------------------------------------------------	|
| **Kairos core**                                  	|    https://github.com/kairos-io/kairos/releases      	|
| **Kairos provider (k3s support)**                 |    https://github.com/kairos-io/provider-kairos/releases |
