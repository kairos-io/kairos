---
title: "Kairos release v1.5"
date: 2023-01-27
linkTitle: "Announcing v1.5 Kairos release"
description: "Introducing Kairos 1.5: A Smarter, More Secure Way to Manage Your Infrastructure"
author: Ettore Di Giacinto ([Twitter](https://twitter.com/mudler_it)) ([GitHub](https://github.com/mudler))
---
<h1 align="center">
  <br>
     <img width="184" alt="kairos-white-column 5bc2fe34" src="https://user-images.githubusercontent.com/2420543/215073247-96988fd1-7fcf-4877-a28d-7c5802db43ab.png">
    <br>
<br>
</h1>

We are thrilled to announce the release of Kairos version 1.5, a major update that brings significant improvements to user experience and security. With this release, we have made it even easier for you to install and set up Kairos, as well as better protect your user data. Our community has been an invaluable source of feedback, bug reports, and contributions, and we are grateful for their support.

You can find Kairos core images at https://github.com/kairos-io/kairos/releases/tag/v1.5.0 and images with k3s pre-bundled here: https://github.com/kairos-io/provider-kairos/releases/tag/v1.5.1. 

## Effortless Installation with the WebUI Installer

![WebUI](https://user-images.githubusercontent.com/2420543/214573939-31f887b8-890c-4cce-a02a-0100198ea7d9.png)

Gone are the days of complicated command-line instructions. With the new [WebUI installer](/docs/installation/webui/), installation and setup are a breeze. Simply follow the steps on the web page, and you'll be up and running in no time. You can also use [our core images as an installer](/docs/examples/core/). Take a look at this gif to see the WebUI installer in action:

![Peek 2023-01-04 01-04](https://user-images.githubusercontent.com/2420543/210461794-fb80ad90-5d11-479d-945d-2e3ba3890435.gif)

## Protect Your Data with User Data Encryption at the Edge

Kairos 1.5 now allows you to encrypt your user data with ease, keeping it secure from prying eyes. Encryption is done via TPM and optionally with the Kairos KMS (Key Management Server) for external authentication and management of encrypted secrets. Check out our [documentation](/docs/advanced/partition_encryption) for more information on partition encryption.

## OS updates

We've added RockyLinux and Debian to our list of supported releases, giving you more options to run Kairos on both stable and feature-rich operating systems. We've also updated our Alpine support, so you can now run Kairos on the latest version of Alpine Linux.

## Extend Kairos with Custom Deployment Models (`bundles`)

Kairos 1.5 allows you to extend the configuration of your node with custom, container-based deployment models defined as `bundles`. Check out our [documentation](/docs/advanced/bundles) and [examples](/docs/examples/bundles) to see how to deploy `MetaLB`. `Kubevirt` and `MetalLB` bundles are also availble in the [community-bundles](https://github.com/kairos-io/community-bundles) repository.

---

For a full list of changes, see the  [Changelog](https://github.com/kairos-io/kairos/releases/tag/v1.5.0). We hope you find these updates useful and as always, let us know if you have any questions or feedback. Thanks for using Kairos!
