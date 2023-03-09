---
title: "Understanding Immutable Linux OS: Benefits, Architecture, and Challenges"
date: 2023-03-09
linkTitle: "Understanding Immutable Linux OS: Benefits, Architecture, and Challenges"
description: "In this post we are trying to answer to some of the typical questions that helps understanding Immutable OSes principles and we will dive a bit in what solutions are out there, and what are the challenges in the field"
author: Ettore Di Giacinto ([Twitter](https://twitter.com/mudler_it)) ([GitHub](https://github.com/mudler))
---

The traditional Linux operating system has been a popular choice for many years due to its flexibility and customization. However, it has its limitations when it comes to security and reliability. Immutable Linux operating systems provide a more secure and reliable option, especially in environments where security is a top priority. In this post we are trying to answer to some of the typical questions that helps understanding Immutable OSes principles and we will dive a bit in what solutions are out there, and what are the challenges in the field.

## What is an Immutable Linux OS?

When someone experienced will try to explain this to a newcomer, it ends to be a very detailed discussion on system internals, we will try here to simplify it as much as possible for anyone even unfamiliar to chew the concepts.

If we look for a formal definition, an immutable Linux OS ( also known as an Immutable Infrastructure or Immutable Deployment) is an operating system that is designed to be unchangeable and read-only, meaning that the system files and directories cannot be modified after the operating system has been installed. Any changes made to the system are temporary and are lost when the system is rebooted. It is like a snapshot of a standard Linux system that cannot be changed. Instead, any updates or changes are made by creating a new instance of the OS, deploying it, and then switching over to the new instance.  

If you already have used Linux, or you are a Linux user, typically, if you are `root` (Administrator) you can write anywhere in the filesystem. That means that potentially, you could corrupt the OS portion responsible for booting, or for management.
In layman terms, in a standard Linux distribution for instance to install a package you would use the package manager, for instance `apt`, `dnf`, or alikes. In an immutable OS any command that would attempt to modify the files in the system would fail - as those are only accessible for reading.

Immutable systems are used in environments where security is a top priority, such as cloud computing environment, embedded systems, kiosks or containers - every environment that need also to scale.

{{% alert color="info" %}}
_But what does that mean really? What's the problem that Immutable systems are trying to solve?_
{{% /alert %}}

There are several advantages to using immutable Linux systems over traditional Linux systems. Firstly - there is an additional layer of **security** - it's not possible to tamper the runtime OS, changes, if accepted, are discarded on the next reboot. This means that if a hacker or a malicious actor gains access to the system, they cannot make permanent changes that could compromise the system's security. 

Secondly, **maintenance** - immutable systems are easier to maintain because they do not require regular updates or patches at atomic package level, the whole OS gets updated as we are used already with Android phones or alikes.

Finally, because the system is read-only, it is more reliable and less prone to failure. A declarative configuration model is usually tied to it, to allow configurations of the OSes even simplified when orchestrated with other tools (such as Ansible, Terraform or alikes) or even not required.

{{% alert color="info" %}}
_Right, but now, how I do manage upgrades?_
{{% /alert %}}

Instead of upgrading the system in place, considering every single package, upgrades are typically handled by creating a new, updated image of the operating system and replacing the existing image. This process is commonly referred to as "image-based upgrade". The image can also be delivered to the end system, but this differs depending on the implementation - such as there is no building on the node side.

In contrast, standard Linux systems typically use package managers, such as `apt` or `yum`, to upgrade software packages in place. This can be a more complex process because the package manager must ensure that all dependencies are satisfied and that there are no conflicts between different software packages. Additionally, upgrades in standard Linux systems can sometimes cause issues if there are conflicts between different versions of software packages or if the upgrade process is interrupted.

## The elephant in the room at scale

In standard Linux systems, the package manager has typically more responsabilities, interacting directly with the system to apply changes to it:

- Install packages
- Upgrade packages  
- Merging configurations  
- Generating additional data required for the package (caches, initramfs in case of Kernels)  

This allows a great degree of flexibility, as installing software, upgrading, and having a system running is easy as a couple of interaction away with the package manager.

The package manager then have a big deal of portion in the stack. It is responsible of applying upgrades correctly, installing packages and so on so forth - every single action it is demanded to the node which wants to apply the upgrade.

Consider an installed system that runs an upgrade process - ideally the package management should take care of at least some aspects:
- Correctly order dependencies (a solver might be involved)
- Verify which packages are installed or not
- Verify which new packages are going to be installed
- Verify package file conflicts
- Generate additional files wanted by the packages
- Handle file transmission securely
- Verify conflicts

In a scenario where scaling out deployments, upgrades, and cutting operational costs is a top priority, delegating the heavy lifting to the last mile is sub-optimal, as it exposes to infrastructure drift.
Nowadays you hardly encounter package conflicts if you are not diving into substantial system changes. But when the complexity of your stack grows, you might need set of packages that conflicts with others  - In a typical system the package manager then would lead to the user to act on such a conflicts.

[ Image of package conflicts here! ]
Huh, didn't we get rid of those already?

For instance, `zypper` and `dnf` prompts the user when a package conflicts arises to solve, or similarly, strategy to weaken deps or specific configuration is needed in certain cases to handle successfully upgrades, or installation via package manager - This is also because, we have tools like Ansible, Salt, Puppet, or Chef to manage and control standard systems upgrade mechanisms, and such. Do we want to have any interaction with each system during high scale upgrades? Definitely not.
Another interesting aspect is, that, in the standard model, the client handle certain portions of upgrades and installations - such as for instance updating configurations files, or more deep level aspects as regenerating the initramfs. Those are all actions that, eventually, could raise up your infrastructure drift level. Would you ever want a configuration merging *during* the rollout of an upgrade block everything or cause damage to your infrastructure and interrupt services? I'm going to bet you don't. There are various ways to not deal explictly with it, as preparing fallback, or switching services connections after an upgrade has been rolled out - but, what if we can make the experience better, avoiding any chance of breakage? Something you learn at scale is that - even if something happens sporadically, when working with big numbers, that will you bite again later on.
Transactional upgrades are a step toward in standard mutable Linux systems, which makes act similar in some ways to image-based upgrades in immutable Linux systems, but there are some important differences. In a transactional upgrade, the new software packages are prepared, usually into a separate partition, and applied after the first boot, similar to how an image-based upgrade works. However, unlike an immutable system, the existing system files can still be modified during the upgrade process and an Immutable OS has no concept of packages - have usually the concept of the OS image which contains all the packages inside.

On the opposite, Immutable OS don't expose the node to those complexities during upgrades nor during installation. It is a simplification of managing the OS stack - the image is built beforeahead, by supposedly an infrastructure, or the node itself, by a well-tested, reproducible recipe which is not modifying in dynamically the system itself, but rather prepares the system to boot into the new image, with the packages you want inside.

The complexity of preparing a system is still demanded to the package manager during build phase, however, it will prepare a new, pristine, environment that the real system will boot afterwards.
For instance Immutable Linux OSes that use A/B partitioning, when an update is available, the system creates a new image of the operating system with the updated software packages or configuration changes. The new image is deployed to a transitive partition, which then becomes the new active partition. Once the update is complete and the system has been verified as functioning properly, the old partition becomes the passive for fallback. In case of a failed upgrade, the system can simply boot on the passive partition.

## Immutable OS: a look at the current landscape

Here are several popular solutions for Immutable OSes. For instance:

- CoreOS - A Linux-based operating system designed for containers and cloud computing, which uses an immutable file system called "Container Linux". CoreOS has now merged with Red Hat Enterprise Linux.
- NixOS - A Linux-based operating system that uses a purely functional package management system, allowing for easy deployment of consistent environments and atomic upgrades.
- Project Atomic - A CentOS-based Linux distribution that focuses on container deployment and management, using a layered approach that allows for easy rollbacks.
- Ubuntu Core - A minimal Ubuntu-based operating system designed for use in IoT devices, appliances, and other embedded systems. Ubuntu Core uses transactional updates to ensure that the system remains immutable and secure.
- RancherOS - A Linux-based operating system that is designed to be minimal, lightweight, and optimized for running containers. RancherOS uses Docker for all system processes, and its file system is mounted read-only, making it immutable.
- Talos
- K3OS
- Flatcar Container Linux - A Linux-based operating system that is based on CoreOS and is designed for use in containerized environments. Like CoreOS, Flatcar Container Linux uses an immutable file system to provide stability and security.
- Fedora Silverblue - A Fedora-based Linux distribution that uses an immutable file system and a transactional update model to provide a stable and secure environment. Fedora Silverblue is designed for use in desktop and containerized environments.
- Photon OS - A Linux-based operating system developed by VMware, which is designed to run containerized workloads. Photon OS uses a minimal package set and an immutable file system for enhanced security and manageability.

Each of the solutions were created with its own challenges in mind. For instance, k3OS wanted to be a minimal distro around k3s. Due to the target environment too, solutions are different in the implementation details as well. 

We will try to simplify (if you know already this topic and I'm oversimplifying, sorry!) compare them in this table:

| Solution | Based on | File System | Update Model | Target Environment |
|---|---|---|---|---|
| CoreOS | Linux | Container Linux | Transactional Updates | Cloud, Containers |
| Talos | Linux | Container Linux | Transactional Updates | Cloud, Containers |
| K3OS | Linux | Container Linux | A/B | Cloud, Containers |
| NixOS | Linux | NixOS | Functional Package Management | General Purpose |
| Project Atomic | CentOS | Atomic File System | Layered Packages | Containers |
| Ubuntu Core | Ubuntu | Transactional Updates | Transactional Updates | IoT, Embedded Systems |
| RancherOS | Linux | Read-Only File System | Docker for System Processes | Containers |
| Flatcar Container Linux | CoreOS | Container Linux | Transactional Updates | Cloud, Containers |
| Red Hat Atomic Host | Red Hat | Atomic File System | Transactional Updates | Containers |
| Clear Linux | Linux | Stateless File System | Transactional Updates | Cloud, Containers |
| Fedora Silverblue | Fedora | Atomic File System | Transactional Updates | Desktop, Containers |
| Photon OS | Linux | Minimal Package Set | Immutable File System | Containers |

## How Kairos fits in the ecosystem

So, what's Kairos? What's the challenges that Kairos tries to overcome?

I'd say Kairos fits when you are looking for deploying a Linux system on real hardware at the Edge[1], or in a datacenter in the Edge or in your cloud on prem, and you are looking for:
- Zero touch configuration, scale deployments
- Single Distribution center of upgrades across your infrastructure: container registries
- Strong security posture: Online data encryption at-rest via TPM
- Good HW support
- Simplified Kubernetes deployment with self-coordinated K3s
- Flexibility in customization: More fine-grained control on the OS layer (packages installed, versions), support maintenance level

**Maintenance**: You probably noticed already that in the alternatives there is a common pattern: every each one of the solution is particularly tied to a specific OS - for most cases this *might* not be a barrier, but - it has long term consequences, such as: who is going to maintain the OS? how the patches are delivered? how can we make sure the supply chain is untampered?
In a typical Linux system this *might* not be a problem of interest, as we already trust the vendors for upgrades, but in case of Immutable OS, where everything is shipped as a snapshot, that is indeed very important to understand. What are the long term maintenance plans for the distribution? what about support? 

Those are all genuine questions when it comes to which distribution to pick for an infrastructure - because if we were the one to choose, we want to take a good call, thinking in long-term and with maintenance in mind.

Kairos here takes a different approach from the solutions we mentioned so far - first of all it doesn't have any opinion on the base OS. Indeed you can find Kairos versions for all the popular Linux distributions, such as Ubuntu, Debian, Fedora, and others. That means that it's design is flexible and portable to support any base OS, regardless of the Linux distribution.

Another significant advantage of Kairos is its support for long-term maintenance. Each framework image released allows the conversion of any OS to the given Kairos framework version, potentially enabling maintenance for as long as the base OS support model allows. [Learn more about it here](/docs/reference/build-from-scratch).

**Container based**: Kairos treats every OS as a collection of packages, which are managed by Kairos components through container images. Those components as shipped as framework images, that can be overlayed while building a standard, container image. Unlike traditional Linux distributions, upgrades are not carried out by the package manager of the OS, but rather by the `kairos-agent`, which handles upgrades by pulling new container images as systems to boot. 

All installation and upgrades are delivered exclusively through container images, which are overlayed at boot time, eliminating the need for a container engine at runtime. Differently from what you might expect, the container image is *exactly* the image used to boot, including Kernel, Initrd and all the pieces - which means it opens up at customization directly within a Dockerfile.

The image being booted is the container, which contains all the required pieces for booting, from the kernel to the init system. There is no real container runtime running the image, as the container is used to construct an image internally that is then used to boot the system in an A/B fashion, without introducing any overhead.

This approach has several benefits, including the ability to verify the image with security scans and treat it similarly to a standard application, which can be distributed via a container registry.

**Separation of concerns**: One of the key advantages of Kairos is its clear separation of concerns between the OS and the management interface. The OS provides the booting bits and packages necessary for the OS to function, while Kairos provides the operational framework for handling the node's lifecycle and immutability interface. There is a contract between the [Image and Kairos](/docs/reference/build-from-scratch) that allows to handle packages, without vendor lock-in.
This separation allows for easier delegation of package maintenance, CVE, and security fixes to the OS layer, while enabling upgrades to container images by chaining Dockerfiles or manually committing changes to the image.

**Automatic deployments**: To further automatize custom deployment models, you can use the Kairos Kubernetes Native Extensions to create customized configurations directly from Kubernetes or via CLI.

**Self co-ordinated**: At the Edge, configuring multiple-nodes to form a single cluster might have challenges from the network stack (how are IP assigned to the machines?, which machine is going to do the master?) down to the configuration level of the cluster topology. Kairos allows completely self-coordinated deployments also for HA, removing the need of any configuration templating mechanism, or specific-role assignment for nodes.

## Conclusion

In conclusion, an immutable Linux OS provides a more secure and reliable environment than a standard Linux system. However, it may not be suitable for all use cases, such as those that require frequent updates or modifications to the system. Upgrades in immutable systems are handled differently from standard Linux systems, using an image-based approach rather than package-based upgrades. While transactional upgrades in standard mutable Linux systems offer some benefits over traditional package-based upgrades, they still do not provide the same level of security and reliability as image-based upgrades in immutable Linux systems. Overall, the decision to use an immutable Linux system should be based on the specific requirements of the use case, and the benefits and limitations should be carefully considered.

Immutable Linux OSes offer a higher degree of reliability, security, and fault tolerance compared to traditional Linux systems. By using read-only file systems, separate update partitions, A/B partitioning, Immutable Linux OSes provide a safe, reliable way to update the system without downtime or the risk of breaking the system. Immutable Linux OSes are particularly well-suited for critical systems such as cloud container platforms, embedded systems, or IoT devices, where stability, security and scalability are of the utmost importance.


## Footnotes

1: As I dislike marketing buzzwords, the Edge works like the last-mile of computing: imagine a dedicated hardware that needs to be controlled by the Cloud somehow - a small server running Kubernetes, doing measurements for instance, and talking back to the Cloud. It is a generic, very wide, term that re-defines computing usage across the scenarios (near edge, far edge, ...), each one having their own dedicated solution for deployment. 

In a simpler term, Kairos can be also deployed to bare-metals, or generally speaking it have a good support for Hardware.