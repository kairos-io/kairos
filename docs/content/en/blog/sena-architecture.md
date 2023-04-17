---
title: "Kairos is now part of the Secure Edge-Native Architecture by Spectro Cloud and Intel"
date: 2023-04-18
linkTitle: "The Secure Edge-Native Architecture"
description: "Learn about how Kairos is now part of SENA, the Secure Edge-Native Architecture announced by Spectro Cloud and developed in collaboration with Intel, enabling organizations to securely deploy, provision, operate and manage at scale edge locations. Discover the benefits of SENA and what's coming up in the future roadmap of Kairos' secure edge computing solutions."
author: Ettore Di Giacinto ([Twitter](https://twitter.com/mudler_it)) ([GitHub](https://github.com/mudler))
---

The Kairos team is thrilled to announce the release of the Secure Edge-Native Architecture (SENA) whitepaper! You can download it [here](https://github.com/kairos-io/kairos/files/11250843/Secure-Edge-Native-Architecture-white-paper-20240417.3.pdf)

## What is SENA?

SENA stands for "Secure Edge-Native Architecture." It is a comprehensive solution architecture that outlines the tools and practices to address the modern requirements for deploying and managing Kubernetes-based edge applications at scale. SENA’s objective is to establish a new industry standard in the form of a well-defined framework that leverages best-in-class security and other concepts, design principles and tools, bringing together the most innovative hardware and software security capabilities.

SENA covers considerations across the full lifecycle of edge hardware and software to enable teams to efficiently deploy, provision, operate and manage edge environments at scale. 

## Kairos and SENA

Kairos is a core foundation of SENA, providing capabilities in combination with other components across the following areas:

### When deploying hardware edge devices

- Ease of deployment: Kairos enables zero-touch provisioning through our [Kubernetes Native API](https://kairos.io/docs/installation/automated/) and locally with [AuroraBoot](https://kairos.io/docs/reference/auroraboot/).
- Self-coordinated deployment: Enable self-coordinated, fully autonomous deployments with [Integrated Kairos P2P support](https://kairos.io/docs/installation/p2p/).
- Flexible deployments: Kairos can be fully customized to meet your Infrastructure needs. Extend [Kairos images easily](https://kairos.io/docs/advanced/customizing/), or [build your own using the Kairos framework](https://kairos.io/docs/reference/build-from-scratch/), even at scale [by leveraging the power of Kubernetes](https://kairos.io/docs/advanced/build/).

### When provisioning the complete edge stack

- Ensuring the provenance of the image attestation before deployments and during upgrades via the Kubernetes control plane with [kyverno](https://kyverno.io/docs/writing-policies/verify-images/). Instructions can be found [here](https://kairos.io/docs/upgrade/kubernetes/#verify-images-attestation-during-upgrades).
- Ensuring provenance of the artifacts and comply with SLSA: Kairos releases SBOM artifacts, and builds on Github Actions, allowing you to identify and track components included in the released images with [cosign](https://github.com/sigstore/cosign).


## When operating the edge application

- Immutable, read-only OS stack: Kairos is a single [container image](https://kairos.io/docs/architecture/container/), [immutable system](https://kairos.io/docs/architecture/immutable/)  which is read-only and cannot be modified during runtime.
- Ensuring the privacy of user data at rest and in use. You can [encrypt data at rest](https://kairos.io/docs/advanced/partition_encryption/#offline-mode) using the TPM chip and with the Kairos Key Management Server (KMS) 'kcrypt'.The KMS also accepts only hardware devices with a TPM chip, ensuring onboarding of trusted devices.
- Providing the ability for applications to execute in a Trusted Execution Environment (TEE) leveraging [Gramine](https://github.com/gramineproject/gramine). A TEE is an environment where hardware mechanisms are used to ensure the integrity and privacy of process execution, protecting against privileged (root) processes and physical snooping of electrical signals or devices in the system. You can already run workloads in a TEE with Kairos. For instructions check out [Confidential computing](https://kairos.io/docs/advanced/coco/) 

## What's next

Here are some of the items in our roadmap:

- Static and Dynamic measured boot: We are planning to have UKI-flavored variants to boot the full OS in a single file. This will enable measurement, signing, and verification, simplifying maintenance and management, and leading to true immutability with a reduced attack surface.
- Ensuring the provenance and integrity of the OS during boot and runtime. We plan to integrate measured boot and SecureBoot on top of UKI images, integrating with Keylime, enabling remote attestation of system integrity after boot
- Ensuring the provenance and integrity of the application stack in runtime. Integration with GSC, [MarbleRun](https://github.com/edgelesssys/marblerun) - to seamlessly run confidential applications in your Kubernetes cluster and running attestation of confidential workloads.
- Management of hardware at scale: OpenAMT - Offering ways to automatically register Kairos boxes to an OpenAMT-supported management platform.

You can already benefit from the SENA Architecture today with Kairos and you can follow our roadmap to see what's coming up in the next releases [here](https://github.com/orgs/kairos-io/projects/2).

Stay tuned! More to come! 
