---
title: "Kairos release v2.0"
date: 2023-04-13
linkTitle: "Announcing v2.0 Kairos release"
description: "Introducing Kairos 2.0: long live UKI!"
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

## Kairos 2.0.0 release

Kairos 2.0.0 has just been released, and we are thrilled to share the latest updates and improvements to the Kairos project. This release is a major release as reflect changes to internal core components.
  
### What changed?

We replaced the former dracut modules (a set of bash scripts/dracut/systemd services), which were responsible for the immutability management of Kairos, with https://github.com/kairos-io/immucore , a self-contained binary which doesn't have dependencies and can run without dracut and systemd. While changes shouldn't be impactful for most of our users, as changes impacted only in internal components, we suggest to try the upgrade in a lab environment before upgrading from earlier versions (v1.x).

The 2.0 release allows us to:
- not depend anymore on systemd while set up immutability on boot ( allowing us to unblock several stories, for instance create Alpine images with vanilla kernels )
- have hybrid images, that boots both [UKI](https://github.com/uapi-group/specifications/blob/main/specs/unified_kernel_image.md) as a single file image, and as well as pivoting (as we are doing currently)
- pave the way for things like SecureBoot, Static Measured boot and much more
- debug things more cleanly, have a better testbed, and allow to ease out maintenance of the codebase
- be a step closer to our Confidential computing roadmap, indeed now you can try out running [Confidential computing workload](https://kairos.io/docs/advanced/coco/).

Besides, we have now full SBOM list attached to images, as part of the release process, and `in-toto` attestation, allowing [you to verify attestation also of SBOM lists](https://docs.sigstore.dev/cosign/attestation/), and have full audit of images. We also have integrated `grype` and `trivy` in our pipelines, and as such now releases contains also CVE reports, and finally we upload the generated reports as sarif file to GitHub to have notifications and see with more ease the impact of CVEs to the images. See also our [documentation](https://kairos.io/docs/upgrade/kubernetes/#verify-images-attestation-during-upgrades) on how to gate upgrades and allow only verified images to be used during the process.

There were also fixes to the Debian flavor (thanks to the community for reporting issues!) and now manual upgrades with private registries are supported, too.

Finally, it is also now possible to specify custom bind mounts path to overlay on top of the persistent partition, allowing to easily specify paths that you want to be persistent in the system via the cloud config file: https://kairos.io/docs/advanced/customizing/#customizing-the-file-system-hierarchy-using-custom-mounts .

If you are curious on what's next, check out our [Roadmap](https://github.com/orgs/kairos-io/projects/2) and feel free to engage with our [community](https://kairos.io/community/)!

---

For a full list of changes, see the  [Changelog](https://github.com/kairos-io/kairos/releases/tag/v2.0.0). We hope you find these updates useful and as always, let us know if you have any questions or feedback. Thanks for using Kairos!
