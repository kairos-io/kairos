import "./Sections.css";
import explore from "../../assets/index/explore.png";
import github from "../../assets/index/github.png";
import book from "../../assets/index/book.png";

import one from "../../assets/index/1.png";
import two from "../../assets/index/2.png";
import three from "../../assets/index/3.png";
import four from "../../assets/index/4.png";
import five from "../../assets/index/5.png";
import six from "../../assets/index/6.png";
import seven from "../../assets/index/7.png";

const MAIN_SECTIONS = [
  {
    direction: "vertical",
    sections: [
      {
        title: <>The Immutable <br/> edge factory</>,
        icon: one,
        sections: [
          {
            title: "Let's get meta:",
            description:
              "We call Kairos a meta Linux Distribution. Why meta? Because it sits as a container layer turning any Linux distro into an immutable system distributed via container registries. With Kairos, the OS is the container image, which is used for new installations and upgrades.",
          },
          {
            title: "Bring your own OS:",
            description:
              "The Kairos 'factory' enables you to build custom bootable OS images for your edge devices, from your choice of OS (including openSUSE, Alpine and Ubuntu) and your choice of edge Kubernetes distribution - Kairos is totally agnostic.",
          },
          {
          title: "Repeatable, immutable:",
          description:
              "Each node boots from the same image, so no more snowflakes in your clusters, and each system is immutable - it boots in a restricted, permissionless mode, where certain paths of it are not writeable. For instance, after installation it's not possible to install additional packages in the system, and any configuration change is discarded after reboot. This dramatically reduces the attack surface and the impact of malicious actors gaining access to the device. ",
          },
        ],
      },
      {
        title: <>Welcome to the <br/> self-driving edge</>,
        icon: two,
        sections: [
          {
            title: "Zero touch provisioning:",
            description:
              "Keeping simplicity while providing complex solutions is a key factor of Kairos. Onboarding of nodes can be done via QR code, manually, remotely via SSH, interactively, or completely automated with Kubernetes, with zero touch provisioning.",
          },
          {
            title: "Secure peer to peer mesh with VPN:",
            description:
              "Kairos optionally supports P2P full-mesh out of the box. New devices wake up with a shared secret and distributed ledger of other nodes and clusters to look for — they form a unified overlay network that’s E2E encrypted to discover other devices, even spanning multiple networks, to bootstrap the cluster.",
          },
        ],
      },
      {
        title: <>Containerize your <br/> lifecycle management</>,
        icon: three,
        sections: [
          {
            title: "Treat your OS just like any app:",
            description:
              "Each Kairos OS is created as easily as writing a Dockerfile — no custom recipes or arcane languages here. You can run and customize the container images locally with docker, podman, or your container engine of choice exactly how you do for apps already.",
          },
          {
            title: "Run your pipeline to the edge: ",
            description:
              "Your built OS is a container-based single image that is distributed via container registries, so it plugs neatly into your existing CI/CD pipelines. It makes edge scale as repeatable and portable as driving containers. Customizing, mirroring of images, scanning vulnerabilities, gating upgrades, patching CVEs are some of the endless possibilities.  Updating nodes is just as easy as selecting a new version via Kubernetes. Each node will pull the update from your repo, installing on A/B partitions for zero-risk upgrades with failover.",
          },
          {
            title: "Run K8s with K8s:",
            description:
              "Use Kubernetes management principles to manage and provision your clusters. Kairos supports automatic node provisioning, via CRDs, upgrade management via Kubernetes, node repurposing and machine auto scaling capabilities, and complete configuration management via cloud-init.",
          },
        ],
      },
      {
        title: <>A community soul</>,
        icon: four,
        sections: [
          {
            title: "On the shoulders of giants:",
            description:
              "Kairos draws on the strength of the cloud-native ecosystem, not just for principles and approaches, but components. Cluster API is optionally supported as well, and can be used to manage Kubernetes clusters using native Kubernetes APIs with zero touch provisioning. ",
          },
          {
            title: "Extensively tested:",
            description:
              "We move fast, but we try not to break stuff — particularly your nodes. Every change in the Kairos codebase runs through highly engineered automated testing before release to catch bugs earlier.",
          },
          {
            title: "Designed for everyone:",
            description:
              "While Kairos has been engineered for large-scale use by devops and IT engineering teams working in cloud, bare metal, edge and embedded systems environments, we welcome makers, hobbyists and anyone in the community to participate in driving forward our vision of the immutable, decentralized, containerized edge.",
          },
          {
            title: "Backed by Spectro Cloud:",
            description:
              <>Kairos is a vibrant, active project with time and financial backing from Spectro Cloud, a Kubernetes management platform provider with a strong commitment to the open source community. It is a silver member of the CNCF and LF Edge, a Certified Kubernetes Service Provider, and a contributor to projects such as Cluster API. Find more about Spectro Cloud <a href="https://www.spectrocloud.com/" target="_blank" rel="noopener noreferrer">here</a>.</>,
          },
        ],
      },
    ],
  },
  {
    direction: "horizontal",
    sections: [
      {
        title: "Installation",
        icon: five,
        description:
          "See how to get up and running with Kairos, in less than 15 minutes!",
      },
      {
        title: "Architecture",
        icon: six,
        description:
          "Get inside Kairos, from the factory to P2P mesh capabilities.",
      },
      {
        title: "Examples",
        icon: seven,
        description:
          "Stretch your wings with best practices of common tasks after installing Kairos.",
      },
    ],
  },
  {
    title: "Join our community",
    direction: "horizontal",
    theme: "gray",
    sections: [
      {
        title: "Contribute on Github",
        icon: github,
        noHeight: true,
        iconWidth: "64",
      },
      {
        title: "Documentation",
        icon: book,
        noHeight: true,
        iconWidth: "64",
      },
    ],
  },
];

export default function Sections() {
  function renderTertiarySections(section, index) {
    return (
      <div className="tertiary-section" key={index}>
        <div className="title">{section.title}</div>
        <p>{section.description}</p>
      </div>
    );
  }
  function renderSecondarySections(section, index) {
    return (
      <div className="secondary-section" key={index}>
        <div className="title">
          <div className={`${section.noHeight ? "image-container-removeHeight" : "image-container"}`}><img src={section.icon} alt="section logo" width={section.iconWidth || "112"} /></div>
          <div>{section.title}</div>
          <p>{section.description}</p>
        </div>
        {section.sections?.length && (
          <div>{section.sections.map(renderTertiarySections)}</div>
        )}
      </div>
    );
  }

  function renderMainSections(section, index) {
    return (
      <div className="main-section" key={index}>
        {section.title && <div className="title">{section.title}</div>}
        <div className="sections" aria-current={section.direction}>
          {section.sections.map(renderSecondarySections)}
        </div>
      </div>
    );
  }
  return (
    <div className="sections">{MAIN_SECTIONS.map(renderMainSections)}</div>
  );
}
