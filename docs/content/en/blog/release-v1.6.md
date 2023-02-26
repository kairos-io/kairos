---
title: "Kairos release v1.6"
date: 2023-02-26
linkTitle: "Announcing v1.6 Kairos release"
description: "Introducing Kairos 1.6: Get ready to boot with AuroraBoot!"
author: Ettore Di Giacinto ([Twitter](https://twitter.com/mudler_it)) ([GitHub](https://github.com/mudler))
---
<h1 align="center">
  <br>
     <img width="184" alt="kairos-white-column 5bc2fe34" src="https://user-images.githubusercontent.com/2420543/215073247-96988fd1-7fcf-4877-a28d-7c5802db43ab.png">
    <br>
<br>
</h1>

Kairos is a cloud-native meta-Linux distribution that brings the power of public cloud to your on-premises environment. With Kairos, you can build your own cloud with complete control and no vendor lock-in. It allows you to easily spin up a Kubernetes cluster with the Linux distribution of your choice, and manage the entire cluster lifecycle with Kubernetes.

#### Why you should try Kairos:
Kairos provides a wide range of use cases, from Kubernetes applications to appliances and more. You can provision nodes with your own image or use Kairos releases for added flexibility. Kairos also simplifies day-2 operations like node upgrades. It provides the benefits of a unified, cloud-native approach to OS management.

#### What you can do with Kairos:
With Kairos, you can create an immutable infrastructure that stays consistent and free of drift with atomic upgrades. You can manage your cluster's entire lifecycle with Kubernetes, from building to upgrading. Kairos also allows you to automatically create multi-node, single clusters that span across regions for maximum flexibility and scalability.

## Kairos 1.6.0 release

Kairos 1.6.0 has just been released, and we are thrilled to share the latest updates and improvements to the Kairos project. This release includes bug fixes, small improvements to the Kairos core codebase, and the introduction of AuroraBoot, a tool that simplifies bootstrapping of Kairos nodes. In this post, we will explore how AuroraBoot works and its benefits for users deploying Kairos.
  
### What is AuroraBoot?  

[AuroraBoot](https://kairos.io/docs/reference/auroraboot/) is a tool designed to make the process of bootstrapping Kairos machines quick, simple, and efficient. It is specifically designed for the Kairos operating system and provides a comprehensive solution for downloading required artifacts and provisioning a machine, both from network or manually via flashing to USB stick.  
  
AuroraBoot simplifies the bootstrapping process by automating several steps, such as downloading required files, verifying their authenticity, and providing a streamlined interface for customizing the installation media. With AuroraBoot, users can prepare the environment for network-based bootstrapping, download the necessary release assets, and also customize the installation media for USB-based mass-installations.  
  
### The Benefits of AuroraBoot  
With AuroraBoot, users can prepare multiple nodes in a lab before shipment or deploy Kairos nodes in a network segment where workload can already be sent to (running AuroraBoot in an already-existing downstream cluster). Additionally, AuroraBoot offers a simple, intuitive, and streamlined way to deploy Kairos automatically and manually. It makes the deployment process faster, more efficient, and less error-prone. Besides, it does leverage the DHCP server already existing in the network for booting, requiring zero-configuration. 

You can see AuroraBoot in action here, with [a full e2e example](https://kairos.io/docs/examples/p2p_e2e/) on how to use it with p2p in Kairos, and in the video below:

{{< youtube id="7Vym18wz9Uw" title="Kairos and libp2p" >}}

## Improvements to the WebUI for a simplified user experience

The WebUI got several improvements, we have integrated the documentation inside the web interface, and now can be accessed also offline. The configuration schema is validated and a message is displayed if the configuration is incorrect. You can see how it works here

[Screencast from 2023-02-21 15-21-29.webm](https://user-images.githubusercontent.com/433958/220372220-2e693032-24be-4de4-8539-18cfe8c5fab8.webm)

## Other Improvements in Kairos 1.6.0

Aside from AuroraBoot, Kairos 1.6.0 includes several improvements and bugfixes, including:  
- Integration of documentation into the Web UI
- Initial support for schema validation in the WebUI and the installer
- Support for Rocky Linux in provider builds
- Renaming of kairos-agent and addition of SHA256 signatures
- Addition of custom mounts
- Fix for DHCP hostname issues
- Fix for encryption reset failures
- Fix for systemd-networkd hostname settings
- Fix for Tumbleweed ISO

You can check the full changelog at:  https://github.com/kairos-io/kairos/releases/tag/v1.6.0

## Conclusion

Kairos 1.6.0 is a significant step forward in simplifying the deployment process of Kairos nodes. With AuroraBoot, users can deploy Kairos faster, more efficiently, and with less risk of error. Additionally, the bug fixes and improvements in this release demonstrate Kairos' commitment to providing a reliable and robust operating system for users. We invite you to download and try Kairos 1.6.0 and experience the benefits of AuroraBoot for yourself.  

---

For a full list of changes, see the  [Changelog](https://github.com/kairos-io/kairos/releases/tag/v1.6.0). We hope you find these updates useful and as always, let us know if you have any questions or feedback. Thanks for using Kairos!