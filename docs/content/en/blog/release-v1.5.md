---
title: "Kairos release v1.5"
date: 2023-01-27
linkTitle: "Announcing v1.5 Kairos release"
description: "Kairos v1.5 is available for general consumption, with many cool features!"
author: Ettore Di Giacinto ([Twitter](https://twitter.com/mudler_it)) ([Github](https://github.com/mudler))
---
<h1 align="center">
  <br>
     <img width="184" alt="kairos-white-column 5bc2fe34" src="https://user-images.githubusercontent.com/2420543/215073247-96988fd1-7fcf-4877-a28d-7c5802db43ab.png">
    <br>
<br>
</h1>

Hey everyone,

We're excited to announce the release of Kairos version 1.5! You can find it at core images at https://github.com/kairos-io/kairos/releases/tag/v1.5.0 and images with k3s here https://github.com/kairos-io/provider-kairos/releases/tag/v1.5.1. This new version brings some major improvements to the user experience and security. Here's a quick rundown of the updates.

We want to give a big shoutout to our community for their support in helping us improve Kairos. Your feedback, bug reports, and contributions have been invaluable.

We hope you find these updates useful. As always, let us know if you have any questions or feedback. Thanks for using Kairos!

## WebUI installer

![WebUI](https://user-images.githubusercontent.com/2420543/214573939-31f887b8-890c-4cce-a02a-0100198ea7d9.png)

We've added the [WebUI installer](/docs/installation/webui/) to make installation and setup a breeze. No more complicated command line instructions — just follow the steps on the web page and you'll be up and running in no time (see also [how to use our core images as an installer](/docs/examples/core/)).

You can see here a gif of it in action:

![Peek 2023-01-04 01-04](https://user-images.githubusercontent.com/2420543/210461794-fb80ad90-5d11-479d-945d-2e3ba3890435.gif)

## User data encryption at the edge

You can now encrypt your user data with ease and keep it secure from prying eyes. Encryption is done via TPM, and optionally with the Kairos KMS (Key Management Server) for external authentication and management of encrypted secrets (see our [documentation here](/docs/advanced/partition_encryption)).  

## OS updates

We've added RockyLinux and Debian to our list of supported releases. This means you can now run Kairos on both and take advantage of their features and stability.

We've also updated our Alpine support, so you can now run Kairos on the latest version of Alpine Linux.

## Extend Kairos

Extend the configuration of your node with custom, container-based deployment models (see [our documentation here](/docs/advanced/bundles) and a [full example showing how to deploy MetaLB](/docs/examples/bundles)). There are also available `Kubevirt` and `MetalLB` bundles from the [community-bundles](https://github.com/kairos-io/community-bundles) repository.

## Notes

You can see the full [Changelog here](https://github.com/kairos-io/kairos/releases/tag/v1.5.0).
