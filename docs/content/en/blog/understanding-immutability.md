---
title: "Understanding Immutable Linux OS: Benefits, Architecture, and Challenges"
date: 2023-03-09
linkTitle: "Understanding Immutable Linux OS: Benefits, Architecture, and Challenges"
description: "In this post we are trying to answer some of the typical questions that help understanding Immutable OSes principles and we will dive a bit in what solutions are out there, and what are the challenges in the field"
author: Ettore Di Giacinto ([Twitter](https://twitter.com/mudler_it)) ([GitHub](https://github.com/mudler))
---

For years, the traditional Linux operating system has been a top pick for its flexibility and ability to be customized. But as great as it is, there are use cases in which stricter security rules and higher reliability standards are needed. That's where immutable Linux operating systems come in - offering a more secure and reliable option, especially in settings where security is paramount.

{{< card header="![guardian2](https://user-images.githubusercontent.com/2420543/224127635-9bbe5c83-aad9-48a4-a944-087ed105dc0d.jpeg)" subtitle="_Guardian, fierce and protective, defending a treasure hoard, dark and mysterious dungeon environment, flickering torchlight lighting, monochrome photography style, 64K UHD quality._ Author: _Stable diffusion_"
         >}}
{{< /card >}}

In this post, we'll be addressing some common questions to help you understand the principles behind immutable operating systems. We'll also be exploring the various solutions available and the challenges faced in this field. So, get ready to dive in!

## What is an Immutable Linux OS?

Explaining the concept of an immutable Linux OS to a newcomer can often turn into a detailed discussion on system internals. However, we'll simplify it here as much as possible, even for those unfamiliar with the concepts.

Formally defined, an immutable Linux OS (also known as Immutable Infrastructure or Immutable Deployment) is an operating system designed to be unchangeable and read-only. This means that once the operating system has been installed, the system files and directories cannot be modified. Any changes made to the system are temporary and lost when the system is rebooted. Think of it as a snapshot of a standard Linux system that cannot be changed. Any updates or changes are made by creating a new instance of the OS, deploying it, and switching over to the new instance. You can also find a very good writeup by Adrian Hornsby [here](https://medium.com/the-cloud-architect/immutable-infrastructure-21f6613e7a23).

If you're already a Linux user, you'll know that as `root` (Administrator), you can write anywhere in the filesystem, potentially corrupting the OS portion responsible for booting or management. In an immutable OS, however, any command that attempts to modify the system files will fail, as those files are only accessible for reading.

Immutable systems are particularly useful in environments where security is a top priority, such as cloud computing, embedded systems, kiosks, and container execution. Essentially, any environment that needs to scale can benefit from the security and reliability of an immutable OS.

{{% alert color="info" %}}
_"But what does that really mean? And what problem are Immutable systems trying to solve?"_
{{% /alert %}}

There are several advantages to using immutable Linux systems over traditional Linux systems. Firstly, there is an additional layer of **security** as it's not possible to tamper with the runtime OS. Changes, if accepted, are discarded on the next reboot. This means that if a hacker or a malicious actor gains access to the system, they cannot make permanent changes that could compromise the system's security.

Secondly, **maintenance** of immutable systems is easier because they do not require regular updates or patches at the atomic package level. Instead, the entire OS is updated, similar to how updates are handled on Android phones.

Finally, because the system is read-only, it is more **reliable** and less prone to failure. A declarative configuration model is usually tied to it, simplifying the configuration of the OS when orchestrated with other tools such as Ansible, Terraform, or similar.

{{% alert color="info" %}}
_"Right, but how do I manage upgrades?"_
{{% /alert %}}

Instead of upgrading the system in place, upgrades are typically handled by creating a new, updated image of the operating system and replacing the existing image, in an atomic operation. This process is commonly referred to as "image-based upgrade". The image can also be delivered to the end system, but this differs depending on the implementation, and there is no building on the node side.

![Upgrade](https://user-images.githubusercontent.com/2420543/224147132-50d6808e-0a1c-48d0-8f44-627bd0dfa9f2.png)

In contrast, standard Linux systems typically use package managers such as `apt` or `yum` to upgrade software packages in place. This can be a more complex process because the package manager must ensure that all dependencies are satisfied and that there are no conflicts between different software packages. Additionally, upgrades in standard Linux systems can sometimes cause issues if there are conflicts between different versions of software packages or if the upgrade process is interrupted.

## Challenges at scale

In standard Linux systems, the package manager has a lot of responsibilities and interacts directly with the system to apply changes. It can install packages, upgrade packages, merge configurations, and generate additional data required for the package. This makes installing software, upgrading, and running a system easy as a couple of interactions away with the package manager.

When it comes to upgrading an installed system, the package manager should take care of many aspects, such as: correctly ordering dependencies (which may require a solver), verifying which packages are installed or not, which new packages will be installed, and handling file transmission securely. However, as the complexity of the stack grows, conflicts between packages can arise, and the package manager may prompt the user to solve them. This is not ideal for scaling out deployments, upgrades, and cutting operational costs since it exposes the infrastructure to drift.

{{< card header="![Screenshot from 2023-03-09 18-25-17](https://user-images.githubusercontent.com/2420543/224106950-7d652652-c8e0-4ee4-980d-b057e4af903f.png)" 
          footer="">}}
 _Huh, didn't we get rid of package conflicts already? ([screenshot](https://www.reddit.com/r/openSUSE/comments/z4ld75/this_seems_to_be_common_in_opensuse_should_i_wait/))_
{{< /card >}}

Tools like Ansible, Salt, Puppet, or Chef can manage and control standard systems upgrade mechanisms without requiring any interaction with each system during high-scale upgrades. In the standard model, clients handle certain portions of upgrades and installations, such as updating configuration files, or regenerating the initramfs. However, these actions could eventually raise the infrastructure drift level, causing a configuration merging to block everything or cause damage to your infrastructure and interrupt services. To avoid such issues, preparing fallback or switching services connections after an upgrade has been rolled out is one way to approach it.

Transactional upgrades, are a step toward making standard mutable Linux systems, act more similarly to image-based upgrades in immutable Linux systems. In a transactional upgrade, the new software packages are prepared, usually into a separate partition, and applied after the first boot, similar to how an image-based upgrade works. However, unlike an immutable system, the existing system files can still be modified during the upgrade process.

On the other hand, immutable OSes simplify managing the OS stack by not exposing the node to complexities during upgrades or installation. The image is built ahead of time, using a well-tested, reproducible recipe that does not modify the system itself. The package manager is responsible for preparing a new, pristine environment that the real system will boot into afterward. For instance, immutable Linux OSes that use A/B partitioning create a new image of the operating system with the updated software packages or configuration changes. The new image is deployed to a transitive partition, which then becomes the new active partition. If the upgrade fails, the system can simply boot on the passive partition.

## Immutable OS: a look at the current landscape

Here are some popular Immutable OS solutions, although this list is not exhaustive, there are much better and updated you can find [on Github](https://github.com/castrojo/awesome-immutable). Each of the solutions was created to tackle its own set of challenges, and they differ in their implementation details depending on their target environments.

The following are some of the most popular Immutable OS solutions:

- CoreOS: A Linux-based operating system designed for containers and cloud computing, which uses an immutable file system called "Container Linux". CoreOS has now merged with Red Hat Enterprise Linux.
- Project Atomic: A CentOS-based Linux distribution that focuses on container deployment and management, using a layered approach that allows for easy rollbacks.
- Ubuntu Core: Ubuntu Core is a version of the Ubuntu operating system designed and [engineered for IoT and embedded systems](https://ubuntu.com/core/services/guide/intro-ubuntu-core). It uses snap packages exclusively to create a confined and transaction-based system. It also updates itself and its applications automatically.
- RancherOS: - A Linux-based operating system that is designed to be minimal, lightweight, and optimized for running containers. RancherOS uses Docker for all system processes, and its file system is mounted read-only, making it immutable.
- Talos: An open-source Linux distribution designed to run Kubernetes, K3s, or other container orchestration systems. It features a highly secure, API-managed infrastructure with automated and scalable operations and is suitable for cloud, containers, and general-purpose environments.
- K3OS (discontinued): A minimal Linux distribution designed specifically for running Kubernetes clusters. k3os is built around k3s, a lightweight Kubernetes distribution, and uses the immutable Container Linux file system with an A/B update model to ensure smooth and reliable updates. It is suitable for cloud and container environments.
- Flatcar Container Linux: A Linux-based operating system that is based on CoreOS and is designed for use in containerized environments. Like CoreOS, Flatcar Container Linux uses an immutable file system to provide stability and security.
- Fedora Silverblue: A Fedora-based Linux distribution that uses an immutable file system and a transactional update model to provide a stable and secure environment. Fedora Silverblue is designed for use in desktop and containerized environments. A nice overview can be found [here](ttps://www.lifeintech.com/2021/11/19/immutable-os/) or [here](https://www.redhat.com/sysadmin/immutability-silverblue)
- Photon OS: A Linux-based operating system developed by VMware, which is designed to run containerized workloads. Photon OS uses a minimal package set and an immutable file system for enhanced security and manageability.

To simplify the comparison between the different Immutable OS solutions, the following table highlights their key differences and the environments they are targeted for:

| Solution | Based on | Update Model | Target Environment |
|---|---|---|---|
| CoreOS | Gentoo | Transactional Updates | Cloud |
| Talos | Nothing | Container image update | Cloud, Containers, General purpose |
| K3OS | Alpine | A/B | Cloud, Containers |
| Project Atomic | CentOS | Layered Packages | Containers |
| Ubuntu Core | Ubuntu | Transactional Updates | IoT, Embedded Systems |
| RancherOS | Linux | Docker for System Processes | Containers |
| Flatcar Container Linux | CoreOS | Transactional Updates | Cloud |
| Red Hat Atomic Host | Red Hat | Transactional Updates | Cloud, optimized for running containers |
| Fedora Silverblue | Fedora | Transactional Updates | Desktop, Containers |
| Photon OS | Linux | Immutable File System | Cloud |

{{% alert color="info" %}}
_"So, what's Kairos? What's the challenges that Kairos tries to overcome?"_
{{% /alert %}}

## How Kairos fits in the ecosystem

Kairos is a great fit when you want to deploy a Linux system on real hardware at the Edge[1] or in a datacenter, whether it's in your cloud on-premises or in the Edge. Specifically, if you're looking for:

- Zero-touch configuration and high-scalable deployments. [See how to perform automated installs](/docs/installation/automated/) or [how to create custom appliances](/docs/advanced/build/).
- A single distribution center of upgrades across your infrastructure using container registries. [See docs](/docs/architecture/container/#benefits)
- Strong security posture, including [online data encryption at-rest via TPM](/docs/advanced/partition_encryption/), Supply chain verification and Service bill of material
- Good hardware support
- Simplified Kubernetes deployment with [self-coordinated K3s](/docs/installation/p2p/)
- [Flexibility in customization](/docs/advanced/customizing/), including fine-grained control over the OS layer (packages installed, versions), and complete support maintenance level by [building images from scratch](/docs/reference/build-from-scratch).
- Complete control over your infrastructure
- A [community-driven](/community/), [open roadmap](https://github.com/orgs/kairos-io/projects/2), office hours, and the opportunity to get involved

**Maintenance** - One thing you may have noticed when comparing Kairos to other alternatives is that it doesn't tie you to a specific OS. Instead, Kairos is flexible and portable, supporting all the popular Linux distributions, such as Ubuntu, Debian, and Fedora, among others. This unties you from typical vendor lock-in strategies forcing to choose a specific distribution only for the immutability aspect.

The design shines also for its support for **long-term maintenance**. Each framework image released by Kairos allows the conversion of any OS to the given Kairos version, which could potentially enable maintenance for as long as the base OS support model allows. [You can learn more about it here](/docs/reference/build-from-scratch).

**Container based** - Kairos treats every operating system (OS) as a set of packages and represents the OS with a standard container image that can be executed with tools such as `podman`, `docker`, and so on. [This container image](/docs/architecture/container/) includes all the necessary components for booting. Kairos components manage all node lifecycle operations, such as upgrading, installing, and resetting. These components are packaged within the [framework images](/docs/reference/image_matrix/#framework-images), which can be overlaid while creating a standard container image. Unlike traditional Linux distributions, the kairos-agent handles upgrades by pulling new container images as systems to boot, instead of relying on the OS package manager.

All installation and upgrades are delivered exclusively through container images, which are overlaid at boot time, eliminating the need for a container engine at runtime. The container image used for booting includes the kernel, initrd, and all other required pieces. This allows for customization directly within a Dockerfile. The container being booted is the image itself, and there is no actual container runtime running the image. The container is used to construct an image internally, which is then used to boot the system in an A/B fashion, without adding any overhead.

This approach offers several benefits, including the ability to verify the image with security scans and treat it similarly to a standard application that can be distributed via a container registry.

**Separation of concerns** - The separation of concerns between the OS and the management interface is clear in Kairos. The OS is responsible for providing the booting components and packages necessary for its operation, while Kairos provides the framework for managing the node's lifecycle and immutability interface. The relationship between the image and Kairos is governed by a [contract](/docs/reference/build-from-scratch), which enables package handling without vendor lock-in.

This separation of concerns simplifies the delegation of package maintenance, CVE monitoring, and security fixes to the OS layer. Upgrades to container images can be achieved by chaining Dockerfiles or manually committing changes to the image.

**Automatic deployments** - To further [automate](/docs/installation/automated/) custom deployment models, the Kairos Kubernetes Native Extensions can be used to create customized configurations either directly from Kubernetes or via the command line interface (CLI).

**Self co-ordinated**: [Configuring multiple nodes](/docs/installation/p2p/) at the Edge to form a single cluster can present challenges at various levels, from the network stack (such as assigning IPs to machines) to the configuration of the cluster topology (such as determining which machine will be the master). However, Kairos enables completely self-coordinated deployments, including for high availability (HA), eliminating the need for any configuration templating mechanism or specific role assignments for nodes.

## Conclusion

In conclusion, an immutable Linux OS provides a more secure and reliable environment than a standard Linux system. However, it may not be suitable for all use cases, such as those that require frequent updates or modifications to the system. Upgrades in immutable systems are handled differently from standard Linux systems, using an image-based approach rather than package-based upgrades. While transactional upgrades in standard mutable Linux systems offer some benefits over traditional package-based upgrades, they still do not provide the same level of security and reliability as image-based upgrades in immutable Linux systems. Overall, the decision to use an immutable Linux system should be based on the specific requirements of the use case, and the benefits and limitations should be carefully considered, something that we can't just let ChatGPT decide :wink:

Immutable Linux OSes offer a higher degree of reliability, security, and fault tolerance compared to traditional Linux systems. By using read-only file systems, separate update partitions, A/B partitioning, Immutable Linux OSes provide a safe, reliable way to update the system without downtime or the risk of breaking the system. Immutable Linux OSes are particularly well-suited for critical systems such as cloud container platforms, embedded systems, or IoT devices, where stability, security and scalability are of the utmost importance.

## Footnotes

1: (Author note) As I dislike marketing buzzwords, I prefer to describe the Edge as the last-mile of computing. It involves a dedicated hardware that needs to be controlled by the Cloud in some way, such as a small server running Kubernetes, performing measurements and communicating with the Cloud. The term "Edge" is a broad, generic term that encompasses various computing scenarios, such as near-edge and far-edge computing, each with its own specialized deployment solution.

To put it simply, Kairos can be deployed on bare-metal hardware, and it provides robust support for hardware deployment.