---
title: "WebUI"
linkTitle: "WebUI"
weight: 1
date: 2022-11-13
description: >
  Use the WebUI at boot to drive the installation
---

{{% alert title="Note" color="warning" %}}

This feature will be available in Kairos version `1.5.0` and in all future releases.

{{% /alert %}}

By default when running the LiveCD, or during installation, Kairos will start a WebUI in the background, listening by default on the `8080` port:

![WebUI](https://user-images.githubusercontent.com/2420543/214573939-31f887b8-890c-4cce-a02a-0100198ea7d9.png)

The WebUI has an input form that accepts the `YAML` config file, features a syntax highlighter and a `YAML` syntax checker. You can find a [full example in our documentation](/docs/reference/configuration) or navigate to our [examples section](/docs/examples).
